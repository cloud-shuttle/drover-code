package coordinator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGitHeadSHA(t *testing.T) {
	dir := t.TempDir()

	// Empty dir should return error
	_, err := gitHeadSHA(context.Background(), dir)
	if err == nil {
		t.Fatalf("expected error for non-git dir")
	}

	// Initialize git repo
	cmd := exec.Command("git", "-C", dir, "init")
	if err := cmd.Run(); err != nil {
		t.Skipf("git not available or init failed: %v", err)
	}

	// Create commit. The runner has no global git identity, so the author
	// has to be set on the command or commit exits 128 and HEAD is missing.
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644)
	if out, err := exec.Command("git", "-C", dir, "add", "test.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	commit := exec.Command("git", "-C", dir,
		"-c", "user.email=drover-test@example.com",
		"-c", "user.name=drover-test",
		"commit", "-m", "init")
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, out)
	}

	sha, err := gitHeadSHA(context.Background(), dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("expected 40-char SHA, got %q", sha)
	}
}

func TestBuildCustomToolchain_NoDockerfile(t *testing.T) {
	dir := t.TempDir()
	c := &Coordinator{
		workDir: dir,
	}

	img, err := c.buildCustomToolchain(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if img != "" {
		t.Errorf("expected empty string, got %q", img)
	}
}

func TestRunWorkerRemote_NoToken(t *testing.T) {
	c := &Coordinator{
		workDir: t.TempDir(),
	}

	// Ensure no token
	os.Unsetenv("UKC_TOKEN")

	res, err := c.runWorkerRemote(context.Background(), Subtask{Index: 0, Description: "test"}, "")
	if err == nil {
		t.Fatalf("expected error without UKC_TOKEN")
	}
	if !res.IsError || res.Output == "" {
		t.Errorf("expected error result, got %+v", res)
	}
}
