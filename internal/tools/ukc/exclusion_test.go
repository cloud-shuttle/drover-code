package ukc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlanWorkspaceUpload_respectsGitignoreAndSecrets(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, ".env"), "SECRET=1\n")
	writeFile(t, filepath.Join(root, "node_modules", "pkg", "index.js"), "x")
	writeFile(t, filepath.Join(root, ".gitignore"), "node_modules/\n")

	summary, err := PlanWorkspaceUpload(root, DefaultWorkspaceLimits())
	if err != nil {
		t.Fatal(err)
	}
	if summary.FileCount != 2 {
		t.Fatalf("file count = %d, want 2 (.gitignore + main.go; .env and node_modules excluded)", summary.FileCount)
	}
}

func TestPlanWorkspaceUpload_rejectsOversizedFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "big.bin"), string(make([]byte, 64)))

	_, err := PlanWorkspaceUpload(root, WorkspaceLimits{MaxFileBytes: 32, MaxTotalBytes: 1024})
	if err == nil {
		t.Fatal("expected size error")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
