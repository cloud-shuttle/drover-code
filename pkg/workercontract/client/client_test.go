package client_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudshuttle/drover-code/pkg/workercontract/client"
	"github.com/cloudshuttle/drover-code/pkg/workercontract/workspace"
)

func emptyTarGz(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestClientLifecycle(t *testing.T) {
	t.Parallel()

	const token = "test-token"
	var uploaded bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/workspace":
			_, _ = io.Copy(io.Discard, r.Body)
			uploaded = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/exec":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"job_id":"job-1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/exec/job-1/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"line\":\"hello\",\"done\":false}\n\n")
			fmt.Fprintf(w, "data: {\"done\":true,\"exit_code\":0}\n\n")
		case r.Method == http.MethodGet && r.URL.Path == "/workspace":
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(emptyTarGz(t))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	downloadDir := filepath.Join(t.TempDir(), "out")

	c := client.New(srv.URL, token, srv.Client())

	ctx := context.Background()
	if err := c.WaitReady(ctx, 5*time.Second); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	if err := c.UploadWorkspace(ctx, workDir, workspace.DefaultLimits()); err != nil {
		t.Fatalf("UploadWorkspace: %v", err)
	}

	if !uploaded {
		t.Fatal("expected workspace upload")
	}

	out, code, err := c.Exec(ctx, "echo hi", nil)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("output = %q", out)
	}

	if err := c.DownloadWorkspace(ctx, downloadDir); err != nil {
		t.Fatalf("DownloadWorkspace: %v", err)
	}
}
