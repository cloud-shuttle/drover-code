package toolutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncate_underLimitUnchanged(t *testing.T) {
	s := strings.Repeat("a", 100)
	if got := Truncate(s); got != s {
		t.Fatal("expected unchanged")
	}
}

func TestTruncate_overLimitAddsNote(t *testing.T) {
	s := strings.Repeat("x", MaxOutputBytes+50)
	got := Truncate(s)
	if len(got) <= MaxOutputBytes {
		t.Fatalf("expected clipped output, len=%d", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("expected truncation note: %q…", got[:80])
	}
}

func TestTruncate_validUTF8AfterClip(t *testing.T) {
	// Multi-byte runes so a naive byte cut could split a code point; result must stay valid UTF-8.
	s := strings.Repeat("é", MaxOutputBytes/2+2000)
	got := Truncate(s)
	if !utf8.ValidString(got) {
		t.Fatal("Truncate produced invalid UTF-8")
	}
	if !strings.Contains(got, "truncated") {
		t.Fatal("expected truncation note for oversized string")
	}
}

func TestSafePath_relativeStaysUnderWorkDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := SafePath(dir, filepath.Join("sub", "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, sub) && got != filepath.Join(sub, "f.txt") {
		t.Fatalf("path %q not under %q", got, sub)
	}
}

func TestSafePath_escapeRejected(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "..", "..", "etc", "passwd")
	_, err := SafePath(dir, outside)
	if err == nil {
		t.Fatal("expected escape error")
	}
}

func TestSafePath_emptyWorkDirAllowsAbs(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	if err := os.WriteFile(p, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := SafePath("", p)
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Fatal("expected absolute path")
	}
}

func TestWriteAtomic_roundTrip(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")
	data := []byte("hello\n")
	if err := WriteAtomic(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != string(data) {
		t.Fatalf("got %q", b)
	}
}

func TestSchema_buildJSON(t *testing.T) {
	schema := NewSchema("object").
		Prop("n", NewSchema("integer")).
		Required("n").
		Desc("doc")
	raw := schema.Build()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["type"] != "object" {
		t.Fatalf("type %v", m["type"])
	}
	req, _ := m["required"].([]any)
	if len(req) != 1 || req[0] != "n" {
		t.Fatalf("required: %v", m["required"])
	}
}
