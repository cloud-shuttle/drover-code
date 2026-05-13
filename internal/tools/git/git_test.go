package git

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitTools_BasicIntegration(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	dir := t.TempDir()
	ctx := context.Background()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
	}

	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-m", "init")

	st := &Status{WorkDir: dir}
	out, err := st.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "##") && !strings.Contains(out, "main") && !strings.Contains(out, "master") {
		// git status output varies by version/config; just assert it's non-empty.
		if strings.TrimSpace(out) == "" {
			t.Fatalf("expected non-empty status output")
		}
	}

	lg := &Log{WorkDir: dir}
	out, err = lg.Execute(ctx, mustJSON(t, map[string]any{"max_count": 1, "one_line": true}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "init") {
		t.Fatalf("expected commit message in log output, got:\n%s", out)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestGitTools_DiffAndCommitValidation(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}
	dir := t.TempDir()
	ctx := context.Background()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "t@e.com")
	run("config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-m", "c1")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	df := &Diff{WorkDir: dir}
	out, err := df.Execute(ctx, mustJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "v1") || !strings.Contains(out, "v2") {
		t.Fatalf("expected diff content, got:\n%s", out)
	}

	run("add", "a.txt")
	run("commit", "-m", "c2")
	out, err = df.Execute(ctx, mustJSON(t, map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if out != "no changes" {
		t.Fatalf("expected clean tree no changes, got:\n%s", out)
	}

	_, err = NewCommitTool(dir).Execute(ctx, mustJSON(t, map[string]any{"message": ""}))
	if err == nil {
		t.Fatal("expected empty commit message error")
	}

	_, err = (&Diff{WorkDir: dir}).Execute(ctx, json.RawMessage(`not json`))
	if err == nil {
		t.Fatal("expected bad json error")
	}
}

func TestGitTools_AddSecondCommitAndBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}
	dir := t.TempDir()
	ctx := context.Background()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "t@e.com")
	run("config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-m", "first")

	ad := &Add{WorkDir: dir}
	if _, err := ad.Execute(ctx, mustJSON(t, map[string]any{"paths": []string{"f.txt"}})); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ad.Execute(ctx, mustJSON(t, map[string]any{})); err != nil {
		t.Fatal(err)
	}
	cm := NewCommitTool(dir)
	if _, err := cm.Execute(ctx, mustJSON(t, map[string]any{"message": "second"})); err != nil {
		t.Fatal(err)
	}

	br := &CreateBranch{WorkDir: dir}
	if _, err := br.Execute(ctx, mustJSON(t, map[string]any{"name": "side", "checkout": true})); err != nil {
		t.Fatal(err)
	}
}

func TestGitTools_CreateBranchEmptyName(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}
	dir := t.TempDir()
	ctx := context.Background()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	_, err := (&CreateBranch{WorkDir: dir}).Execute(ctx, mustJSON(t, map[string]any{"name": ""}))
	if err == nil {
		t.Fatal("expected error for empty branch name")
	}
}
