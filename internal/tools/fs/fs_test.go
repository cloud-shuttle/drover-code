package fs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFile_BinaryRefused(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bin.dat")
	if err := os.WriteFile(p, []byte{0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}

	tool := &ReadFile{WorkDir: dir}
	_, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{"path": "bin.dat"}))
	if err == nil {
		t.Fatalf("expected error for binary file")
	}
}

func TestReadFile_LineSlicingNumbered(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := &ReadFile{WorkDir: dir}
	out, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"path": "a.txt", "start_line": 2, "end_line": 3,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\t two") && !strings.Contains(out, "\ttwo") {
		t.Fatalf("expected numbered output to include line 2, got:\n%s", out)
	}
	if !strings.Contains(out, "\tthree") {
		t.Fatalf("expected numbered output to include line 3, got:\n%s", out)
	}
}

func TestEditFile_ExactMatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := &EditFile{WorkDir: dir}
	_, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"path":       "a.txt",
		"old_string": "world",
		"new_string": "there",
	}))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "hello\nthere\n" {
		t.Fatalf("unexpected file content: %q", string(b))
	}
}

func TestEditFile_FuzzyWhitespaceMatch(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.go")
	orig := "func main() {\n\tfmt.Println(\"hi\")\n}\n"
	if err := os.WriteFile(p, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := &EditFile{WorkDir: dir}
	// old_string uses spaces instead of a tab.
	_, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"path":       "a.go",
		"old_string": "func main() {\n    fmt.Println(\"hi\")\n}\n",
		"new_string": "func main() {\n\tfmt.Println(\"bye\")\n}\n",
	}))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "bye") {
		t.Fatalf("expected replacement, got:\n%s", string(b))
	}
}

func TestEditFile_MultipleMatchesRefused(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("x\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := &EditFile{WorkDir: dir}
	_, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"path":       "a.txt",
		"old_string": "x",
		"new_string": "y",
	}))
	if err == nil {
		t.Fatalf("expected error for multiple matches")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "x\nx\n" {
		t.Fatalf("file should be unchanged, got %q", string(b))
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

func TestReadFile_missingFile(t *testing.T) {
	dir := t.TempDir()
	tool := &ReadFile{WorkDir: dir}
	_, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{"path": "nope.txt"}))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadFile_badJSON(t *testing.T) {
	dir := t.TempDir()
	tool := &ReadFile{WorkDir: dir}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadFile_invalidLineRange(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tool := &ReadFile{WorkDir: dir}
	_, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"path": "a.txt", "start_line": 3, "end_line": 1,
	}))
	if err == nil {
		t.Fatal("expected start > end error")
	}
}

func TestReadFile_pathEscapesWorkDir(t *testing.T) {
	dir := t.TempDir()
	tool := &ReadFile{WorkDir: dir}
	_, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{
		"path": filepath.Join("..", "..", "etc", "passwd"),
	}))
	if err == nil {
		t.Fatal("expected safe path error")
	}
}

func TestReadFile_LargeFileTruncation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "large.txt")
	// Write a 300KB file
	largeData := strings.Repeat("A long line of text that takes up space.\n", 7500)
	if err := os.WriteFile(p, []byte(largeData), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := &ReadFile{WorkDir: dir}
	out, err := tool.Execute(context.Background(), mustJSON(t, map[string]any{"path": "large.txt"}))
	if err != nil {
		t.Fatal(err)
	}

	// Output should be heavily truncated by toolutil.Truncate (max 200k bytes).
	if len(out) > 210000 {
		t.Fatalf("expected truncated output, got length %d", len(out))
	}
	if !strings.Contains(out, "A long line") {
		t.Fatalf("expected output to contain original text")
	}
}
