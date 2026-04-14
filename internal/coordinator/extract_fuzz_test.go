package coordinator

import "testing"

func FuzzExtractJSON(f *testing.F) {
	f.Add("prefix [\"a\"] suffix")
	f.Add("```json\n[\"x\"]\n```")

	f.Fuzz(func(t *testing.T, s string) {
		_ = extractJSON(s)
	})
}
