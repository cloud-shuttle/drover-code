package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzParseMarkdownCommand(f *testing.F) {
	// Add seed corpus
	f.Add("---\nname: test\n---\nbody content")
	f.Add("---\nname: fuzzy\nrisk_tier: 5\n---\nbody $1 @file")
	f.Add("invalid frontmatter")
	f.Add("---\n---")

	f.Fuzz(func(t *testing.T, data string) {
		dir := t.TempDir()
		path := filepath.Join(dir, "fuzz.md")
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Skip("failed to write temp file")
		}

		// The goal of fuzzing here is to ensure it doesn't panic on malformed input.
		// We ignore the error and returned command definition.
		ParseMarkdownCommand(path)
	})
}
