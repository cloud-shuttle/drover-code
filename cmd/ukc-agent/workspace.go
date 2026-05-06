package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const workspaceDir = "/workspace"

func handleUploadWorkspace(w http.ResponseWriter, r *http.Request) {
	// Clean the workspace directory first
	_ = os.RemoveAll(workspaceDir)
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("failed to create workspace dir: %v", err), http.StatusInternalServerError)
		return
	}

	gzr, err := gzip.NewReader(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to read gzip: %v", err), http.StatusBadRequest)
		return
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to read tar: %v", err), http.StatusBadRequest)
			return
		}

		// Prevent zip slip
		target := filepath.Join(workspaceDir, filepath.Clean(header.Name))
		if !strings.HasPrefix(target, filepath.Clean(workspaceDir)+string(os.PathSeparator)) && target != filepath.Clean(workspaceDir) {
			continue // skip invalid paths
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				http.Error(w, fmt.Sprintf("failed to create dir: %v", err), http.StatusInternalServerError)
				return
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				http.Error(w, fmt.Sprintf("failed to create parent dir: %v", err), http.StatusInternalServerError)
				return
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to create file: %v", err), http.StatusInternalServerError)
				return
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				http.Error(w, fmt.Sprintf("failed to write file: %v", err), http.StatusInternalServerError)
				return
			}
			f.Close()
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				http.Error(w, fmt.Sprintf("failed to create parent dir: %v", err), http.StatusInternalServerError)
				return
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				http.Error(w, fmt.Sprintf("failed to create symlink: %v", err), http.StatusInternalServerError)
				return
			}
		}
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"success"}`))
}

func handleDownloadWorkspace(w http.ResponseWriter, r *http.Request) {
	if _, err := os.Stat(workspaceDir); os.IsNotExist(err) {
		http.Error(w, "workspace not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="workspace.tar.gz"`)

	gzw := gzip.NewWriter(w)
	defer gzw.Close()
	tw := tar.NewWriter(gzw)
	defer tw.Close()

	err := filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip the root dir itself
		if path == workspaceDir {
			return nil
		}

		relPath, err := filepath.Rel(workspaceDir, path)
		if err != nil {
			return err
		}
		// Always use forward slashes in tar headers
		relPath = filepath.ToSlash(relPath)

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

	if err != nil {
		// Response already started, we can't send HTTP error codes cleanly here.
		// Just terminate the stream by returning.
		return
	}
}
