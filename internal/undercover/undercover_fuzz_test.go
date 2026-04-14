package undercover

import (
	"testing"
)

// FuzzIsInternalDomain ensures classification never panics on arbitrary strings.
func FuzzIsInternalDomain(f *testing.F) {
	f.Add("https://github.com/foo/bar.git")
	f.Add("https://github.anthropic.com/foo/bar.git")

	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 4096 {
			s = s[:4096]
		}
		_ = isInternalDomain(s)
	})
}
