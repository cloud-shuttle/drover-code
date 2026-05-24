package workerclient_test

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

	"github.com/cloudshuttle/drover-code/internal/tools/ukc"
	"github.com/cloudshuttle/drover-code/internal/workerclient"
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

func TestRunContract(t *testing.T) {
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

	client := workerclient.New(srv.URL, token, srv.Client())
	result, err := workerclient.RunContract(context.Background(), client, workerclient.ContractSpec{
		WorkDir:       workDir,
		DownloadDir:   downloadDir,
		Command:       "echo hi",
		Limits:        ukc.DefaultWorkspaceLimits(),
		MaxHealthWait: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("RunContract: %v", err)
	}
	if !uploaded {
		t.Fatal("expected workspace upload")
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d", result.ExitCode)
	}
	if !strings.Contains(result.Output, "hello") {
		t.Fatalf("output = %q", result.Output)
	}
}
