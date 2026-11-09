package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelectAcceptedFollowsSupersession(t *testing.T) {
	dir := t.TempDir()
	adrDir := filepath.Join(dir, "docs", "adr")
	if err := os.MkdirAll(adrDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(adrDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("0001-old.md", "# Old\n\n**Status**: Accepted\n\nSuperseded by: 0002\n")
	write("0002-new.md", "# New\n\n**Status**: Accepted\n\nSupersedes: 0001\n")
	write("0003-proposed.md", "# Later\n\n**Status**: Proposed\n")

	l := NewADRLoader(dir)
	selected, ids, err := l.SelectAccepted()
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 1 || selected[0].ID != "0002" {
		t.Fatalf("selected=%+v ids=%v", selected, ids)
	}
	if len(ids) != 1 || ids[0] != "0002" {
		t.Fatalf("ids=%v", ids)
	}
}
