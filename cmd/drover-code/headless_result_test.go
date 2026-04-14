package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWriteHeadlessResultAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "result.json")
	r := headlessResultV1{
		OK:           true,
		TurnsUsed:    3,
		TokensInput:  10,
		TokensOutput: 20,
		Branch:       "main",
		CommitSHA:    "abc",
	}
	if err := writeHeadlessResultAtomic(path, r); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out headlessResultV1
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.SchemaVersion != headlessResultSchemaVersion || !out.OK || out.TokensUsed != 30 {
		t.Fatalf("%+v", out)
	}
}

func TestEnvOptionalBool(t *testing.T) {
	t.Setenv("K", "")
	if v, ok := envOptionalBool("K"); ok || v {
		t.Fatal("unset")
	}
	t.Setenv("K", "true")
	if v, ok := envOptionalBool("K"); !ok || !v {
		t.Fatal("true")
	}
	t.Setenv("K", "0")
	if v, ok := envOptionalBool("K"); !ok || v {
		t.Fatal("false")
	}
	t.Setenv("K", "maybe")
	if v, ok := envOptionalBool("K"); ok {
		t.Fatalf("invalid should be unset, got %v %v", v, ok)
	}
}

func TestGitWorkspaceHead_gitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "t@t.com")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "f.txt")
	run("commit", "-m", "c")
	br, sha := gitWorkspaceHead(dir)
	if br == "" || sha == "" {
		t.Fatalf("branch=%q sha=%q", br, sha)
	}
}

func TestBuildHeadlessResult_env(t *testing.T) {
	t.Setenv("DROVER_CODE_PR_URL", "https://example.com/pr/1")
	t.Setenv("DROVER_CODE_POST_HOOK_OWNER", "controller")
	t.Setenv("DROVER_CODE_TESTS_PASSED", "true")

	r := buildHeadlessResult(t.TempDir(), true, "", 1, 2, 3)
	if r.PRURL != "https://example.com/pr/1" || r.PostHookOwner != "controller" {
		t.Fatalf("%+v", r)
	}
	if r.TestsPassed == nil || !*r.TestsPassed {
		t.Fatal("tests_passed")
	}
}
