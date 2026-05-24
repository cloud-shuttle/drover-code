package ukc

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var rootExcludes = []string{
	"drover-local",
	"drover-code",
	"claude-go",
	"ukc-agent",
	"cmd/ukc-agent/ukc-agent",
	"bin",
	"unikraft",
}

var anywhereExcludes = []string{
	".git",
	"node_modules",
	"dist",
	"target",
	"__pycache__",
	".venv",
	"venv",
	".unikraft",
	".drover-code-workers",
}

func shouldExclude(relPath string) bool {
	relPath = filepath.ToSlash(relPath)

	for _, ex := range rootExcludes {
		if relPath == ex || strings.HasPrefix(relPath, ex+"/") {
			return true
		}
	}

	for _, ex := range anywhereExcludes {
		if relPath == ex || strings.HasPrefix(relPath, ex+"/") || strings.Contains(relPath, "/"+ex+"/") || strings.HasSuffix(relPath, "/"+ex) {
			return true
		}
	}
	return false
}

// UploadWorkspace streams a tar.gz of the local directory to the UKC agent's /workspace endpoint.
func UploadWorkspace(ctx context.Context, cfg Config, inst Instance, localDir string, agentToken string) error {
	return UploadWorkspaceWithLimits(ctx, cfg, inst, localDir, agentToken, DefaultWorkspaceLimits())
}

// UploadWorkspaceWithLimits applies workspace exclusion and size caps before upload.
func UploadWorkspaceWithLimits(ctx context.Context, cfg Config, inst Instance, localDir, agentToken string, limits WorkspaceLimits) error {
	return UploadWorkspaceAt(ctx, cfg.HTTPClient, InstanceHTTPSURL(inst), agentToken, localDir, limits)
}

func uploadWorkspaceStream(ctx context.Context, client *http.Client, baseURL, agentToken string, body io.Reader) error {
	url := strings.TrimRight(baseURL, "/") + "/workspace"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+agentToken)
	req.Header.Set("Content-Type", "application/gzip")

	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload workspace failed (HTTP %d): %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// UploadWorkspaceAt uploads localDir to the worker runtime at baseURL.
func UploadWorkspaceAt(ctx context.Context, client *http.Client, baseURL, agentToken, localDir string, limits WorkspaceLimits) error {
	limits = limits.normalize()
	filter, err := newWorkspaceFilter(localDir)
	if err != nil {
		return err
	}

	pr, pw := io.Pipe()
	go func() {
		var walkErr error
		defer func() {
			pw.CloseWithError(walkErr)
		}()

		gzw := gzip.NewWriter(pw)
		defer gzw.Close()
		tw := tar.NewWriter(gzw)
		defer tw.Close()

		var totalBytes int64
		walkErr = filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if path == localDir {
				return nil
			}
			relPath, err := filepath.Rel(localDir, path)
			if err != nil {
				return err
			}
			relPath = filepath.ToSlash(relPath)

			if filter.skipWalk(relPath, info) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			if info.Mode().IsRegular() {
				if info.Size() > limits.MaxFileBytes {
					return fmt.Errorf("workspace exclusion: file %s exceeds max size (%d > %d bytes)", relPath, info.Size(), limits.MaxFileBytes)
				}
				totalBytes += info.Size()
				if totalBytes > limits.MaxTotalBytes {
					return fmt.Errorf("workspace exclusion: total payload exceeds max (%d > %d bytes)", totalBytes, limits.MaxTotalBytes)
				}
			}

			var link string
			if info.Mode()&os.ModeSymlink != 0 {
				link, err = os.Readlink(path)
				if err != nil {
					return err
				}
			}

			header, err := tar.FileInfoHeader(info, link)
			if err != nil {
				return err
			}
			header.Name = relPath

			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			if info.Mode().IsRegular() {
				f, err := os.Open(path)
				if err != nil {
					return err
				}
				defer f.Close()
				if _, err := io.Copy(tw, f); err != nil {
					return err
				}
			}
			return nil
		})
	}()

	return uploadWorkspaceStream(ctx, client, baseURL, agentToken, pr)
}

// DownloadWorkspace fetches the modified workspace tar.gz and extracts it to destDir.
func DownloadWorkspace(ctx context.Context, cfg Config, inst Instance, destDir string, agentToken string) error {
	return DownloadWorkspaceAt(ctx, cfg.HTTPClient, InstanceHTTPSURL(inst), agentToken, destDir)
}

// DownloadWorkspaceAt fetches the result payload from a worker runtime.
func DownloadWorkspaceAt(ctx context.Context, client *http.Client, baseURL, agentToken, destDir string) error {
	url := strings.TrimRight(baseURL, "/") + "/workspace"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+agentToken)

	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download workspace failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	_ = os.MkdirAll(destDir, 0755)

	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("read gzip: %v", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %v", err)
		}

		target := filepath.Join(destDir, filepath.Clean(header.Name))
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) && target != filepath.Clean(destDir) {
			continue // prevent zip slip
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return err
			}
		}
	}
	return nil
}
