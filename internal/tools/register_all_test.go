package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRegisterAll_toolNamesAndCount(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry()
	RegisterAll(r, dir)

	defs := r.Definitions()
	if len(defs) != 16 {
		t.Fatalf("expected 16 built-in tools, got %d", len(defs))
	}
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
	}
	slices.Sort(names)

	want := []string{
		"bash",
		"edit_file",
		"file_info",
		"git_add",
		"git_commit",
		"git_create_branch",
		"git_diff",
		"git_log",
		"git_push",
		"git_status",
		"glob",
		"grep",
		"list_directory",
		"read_file",
		"web_fetch",
		"write_file",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("tool names mismatch\ngot:  %q\nwant: %q", names, want)
	}
}

func TestRegisterAll_emptyWorkDirUsesGetwd(t *testing.T) {
	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Errorf("restore chdir: %v", err)
		}
	})

	r := NewRegistry()
	RegisterAll(r, "")
	if r.Get("read_file") == nil {
		t.Fatal("expected read_file registered")
	}
	p := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(p, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := r.Execute(context.Background(), "read_file", mustJSONRaw(t, map[string]any{"path": "x.txt"}))
	if err != nil {
		t.Fatal(err)
	}
	if out != "ok" {
		t.Fatalf("read_file output %q", out)
	}
}

func mustJSONRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
