package main

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

	"github.com/cloudshuttle/drover-code/internal/config"
)

func runCloudMode(ctx context.Context, workDir string, settings config.Settings) {
	fmt.Println("☁️ Drover Cloud Mode")
	fmt.Printf("Packing workspace %s and sending to Drover Cloud API...\n", workDir)

	// In the real version, this will be api.drover.cloud and will require a token
	apiURL := "http://localhost:8080/api/v1/jobs"

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

		filepath.Walk(workDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if path == workDir {
				return nil
			}
			relPath, err := filepath.Rel(workDir, path)
			if err != nil {
				return err
			}
			relPath = filepath.ToSlash(relPath)

			// Simple exclude for tests
			if strings.HasPrefix(relPath, ".git") || strings.Contains(relPath, "node_modules") {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			var link string
			if info.Mode()&os.ModeSymlink != 0 {
				link, _ = os.Readlink(path)
			}

			header, err := tar.FileInfoHeader(info, link)
			if err != nil {
				return err
			}
			header.Name = relPath
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			if !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
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

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, pr)
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/gzip")
	req.Header.Set("X-Drover-Prompt", os.Getenv("DROVER_PROMPT")) // Pass prompt via header for now

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("❌ Failed to connect to Drover Cloud API: %v\n", err)
		fmt.Println("Make sure the drover-cloud server is running on localhost:8080")
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Response from Drover Cloud: %s\n", string(body))
}
