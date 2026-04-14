package toolutil

import (
	"path/filepath"
	"strings"
	"testing"
)

func FuzzSafePath(f *testing.F) {
	f.Add("/tmp/project", "src/main.go")
	f.Add("/tmp/project", "..")

	f.Fuzz(func(t *testing.T, workDir, userPath string) {
		if workDir == "" {
			return
		}
		got, err := SafePath(workDir, userPath)
		if err != nil {
			return
		}
		absW, err := filepath.Abs(workDir)
		if err != nil {
			t.Fatalf("Abs workDir: %v", err)
		}
		sep := string(filepath.Separator)
		if got != absW && !strings.HasPrefix(got, absW+sep) {
			t.Fatalf("path escapes workdir: got=%q want under %q (input path=%q)", got, absW, userPath)
		}
	})
}
