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

// Default exclusions for uploading workspaces
var defaultExcludes = []string{
	".git",
	"node_modules",
	"dist",
	"target",
	"__pycache__",
	".venv",
	"venv",
	".unikraft",
	"unikraft",
	"bin",
	"drover-local",
	"drover-code",
	"claude-go",
	"ukc-agent",
	"cmd/ukc-agent/ukc-agent",
}

func shouldExclude(relPath string) bool {
	// Normalize path for matching
	relPath = filepath.ToSlash(relPath)
	for _, ex := range defaultExcludes {
		if relPath == ex || strings.HasPrefix(relPath, ex+"/") || strings.Contains(relPath, "/"+ex+"/") || strings.HasSuffix(relPath, "/"+ex) {
			return true
		}
	}
	return false
}

// UploadWorkspace streams a tar.gz of the local directory to the UKC agent's /workspace endpoint.
func UploadWorkspace(ctx context.Context, cfg Config, inst Instance, localDir string, agentToken string) error {
	pr, pw := io.Pipe()

	go func() {
		var err error
		defer func() {
			pw.CloseWithError(err)
		}()

		gzw := gzip.NewWriter(pw)
		defer gzw.Close()
		tw := tar.NewWriter(gzw)
		defer tw.Close()

		err = filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
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

			if shouldExclude(relPath) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
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

	url := InstanceHTTPSURL(inst) + "/workspace"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, pr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+agentToken)
	req.Header.Set("Content-Type", "application/gzip")

	client := cfg.HTTPClient
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
		return fmt.Errorf("upload workspace failed (HTTP %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// DownloadWorkspace fetches the modified workspace tar.gz and extracts it to destDir.
func DownloadWorkspace(ctx context.Context, cfg Config, inst Instance, destDir string, agentToken string) error {
	url := InstanceHTTPSURL(inst) + "/workspace"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+agentToken)

	client := cfg.HTTPClient
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
