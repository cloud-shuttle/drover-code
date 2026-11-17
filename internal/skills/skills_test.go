package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverAndRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".drover", "skills", "review", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: review\ndescription: Check the diff\n---\n\nLook at the diff.\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	list := discoverIn(dir, filepath.Join(dir, "missing-home"))
	if len(list) != 1 || list[0].Name != "review" || list[0].Description != "Check the diff" {
		t.Fatalf("discover %#v", list)
	}
	got, err := readFrom(list, "review")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Look at the diff.") {
		t.Fatalf("body %q", got)
	}
}
