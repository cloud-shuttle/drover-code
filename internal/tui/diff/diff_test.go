package diff

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseUnifiedDiff(t *testing.T) {
	diffStr := `--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,4 @@
 line1
-line2
+newline2
+newline2.5
 line3
@@ -10,2 +11,2 @@
 line10
-line11
+newline11`

	hunks, err := ParseUnifiedDiff(diffStr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d", len(hunks))
	}

	if hunks[0].OldStart != 1 || hunks[0].OldLines != 3 {
		t.Errorf("expected old start 1, length 3, got %d, %d", hunks[0].OldStart, hunks[0].OldLines)
	}

	if len(hunks[0].RawLines) != 5 {
		t.Errorf("expected 5 raw lines in first hunk, got %d", len(hunks[0].RawLines))
	}
}

func TestPatchApplier(t *testing.T) {
	workDir := t.TempDir()
	filePath := "test.txt"
	fullPath := filepath.Join(workDir, filePath)

	original := "line1\nline2\nline3\n"
	if err := os.WriteFile(fullPath, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}

	diffStr := `--- a/test.txt
+++ b/test.txt
@@ -1,3 +1,4 @@
 line1
-line2
+newline2
+newline2.5
 line3`

	hunks, err := ParseUnifiedDiff(diffStr)
	if err != nil {
		t.Fatal(err)
	}

	// Accept the hunk
	hunks[0].Accepted = true

	applier := NewPatchApplier(workDir)
	count, err := applier.ApplyAcceptedHunks(filePath, hunks)
	if err != nil {
		t.Fatal(err)
	}

	if count != 1 {
		t.Fatalf("expected 1 hunk applied, got %d", count)
	}

	patched, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatal(err)
	}

	expected := "line1\nnewline2\nnewline2.5\nline3\n"
	if string(patched) != expected {
		t.Errorf("expected %q, got %q", expected, string(patched))
	}
}

func TestPatchApplier_AtomicWrite(t *testing.T) {
	workDir := t.TempDir()
	filePath := "readonly.txt"
	fullPath := filepath.Join(workDir, filePath)

	original := "line1\nline2\n"
	if err := os.WriteFile(fullPath, []byte(original), 0444); err != nil {
		t.Fatal(err)
	}

	// Make the directory read-only so the tmp file cannot be created/renamed.
	if err := os.Chmod(workDir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(workDir, 0755) // cleanup

	diffStr := `--- a/readonly.txt
+++ b/readonly.txt
@@ -1,2 +1,2 @@
 line1
-line2
+newline2`

	hunks, _ := ParseUnifiedDiff(diffStr)
	hunks[0].Accepted = true

	applier := NewPatchApplier(workDir)
	_, err := applier.ApplyAcceptedHunks(filePath, hunks)
	if err == nil {
		t.Fatal("expected error when writing to read-only directory")
	}

	// Verify the original file was NOT modified (no partial writes)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != original {
		t.Fatalf("file was modified! expected %q, got %q", original, string(content))
	}
}
