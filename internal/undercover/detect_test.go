package undercover

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDetect_gitHubPublicRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "remote", "add", "origin", "https://github.com/acme/demo.git")

	st := Detect(dir)
	if !st.Active {
		t.Fatalf("expected active undercover for public github, got: %s", st.Reason)
	}
}

func TestDetect_gitLabRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "remote", "add", "origin", "https://gitlab.com/acme/demo.git")

	st := Detect(dir)
	if !st.Active {
		t.Fatalf("expected active for gitlab, got: %s", st.Reason)
	}
}

func TestDetect_noRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")

	st := Detect(dir)
	if st.Active {
		t.Fatalf("expected inactive without remote, reason=%q", st.Reason)
	}
}

func TestDetect_internalGitHubHost(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "remote", "add", "origin", "https://github.anthropic.com/acme/demo.git")

	st := Detect(dir)
	if st.Active {
		t.Fatal("expected inactive for internal github host")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, filepath.Base(dir), err, out)
	}
}
