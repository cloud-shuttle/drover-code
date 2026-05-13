package shell

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBash_Echo(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	dir := t.TempDir()
	b := Bash{WorkDir: dir}
	raw, _ := json.Marshal(map[string]string{"command": "echo hello"})
	out, err := b.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "exit_code: 0") {
		t.Fatalf("out: %s", out)
	}
}

func TestBash_WorkingDirectoryRelative(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	b := Bash{WorkDir: root}
	raw, _ := json.Marshal(map[string]string{
		"command":           "pwd",
		"working_directory": "sub",
	})
	out, err := b.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, sub) {
		t.Fatalf("expected pwd under sub, got: %s", out)
	}
}

func TestBash_Timeout(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	b := Bash{WorkDir: t.TempDir()}
	raw, _ := json.Marshal(map[string]any{
		"command":         "sleep 10",
		"timeout_seconds": 1,
	})
	_, err := b.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout, got %v", err)
	}
}

func TestBash_EmptyCommand(t *testing.T) {
	b := Bash{WorkDir: t.TempDir()}
	_, err := b.Execute(context.Background(), []byte(`{"command":""}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBash_LargeOutputTruncation(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	b := Bash{WorkDir: t.TempDir()}
	// Generate 100,000 characters of 'a'
	raw, _ := json.Marshal(map[string]any{
		"command": `printf 'a%.0s' {1..100000}`,
	})
	out, err := b.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	// The internal toolutil.Truncate caps at 200k.
	if len(out) > 210000 {
		t.Fatalf("expected truncated output, got length %d", len(out))
	}
	if !strings.Contains(out, "exit_code: 0") {
		t.Fatal("expected exit code 0")
	}
}

func TestBash_NonZeroExit(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	b := Bash{WorkDir: t.TempDir()}
	raw, _ := json.Marshal(map[string]any{
		"command": `echo "failing"; exit 42`,
	})
	out, err := b.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "failing") {
		t.Fatalf("expected stdout 'failing', got: %s", out)
	}
	if !strings.Contains(out, "exit_code: 42") {
		t.Fatalf("expected exit_code 42, got: %s", out)
	}
}
