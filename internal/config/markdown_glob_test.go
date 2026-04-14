package config

import "testing"

func TestMarkdownPathMatchesIgnore(t *testing.T) {
	cases := []struct {
		rel   string
		globs []string
		want  bool
	}{
		{"pkg/api/CLAUDE.md", []string{"pkg/**"}, true},
		{"pkg/api/CLAUDE.md", []string{"other/**"}, false},
		{"vendor/foo/CLAUDE.md", []string{"vendor/**"}, true},
		{"pkg/CLAUDE.md", []string{"**/CLAUDE.md"}, true},
		{"deep/pkg/CLAUDE.md", []string{"**/pkg/**"}, true},
		{"CLAUDE.md", []string{"**/nested/**"}, false},
		{"a/b/c", []string{"a/**/c"}, true},
		{"a/x/y/c", []string{"a/**/c"}, true},
		{"b/x/y/c", []string{"a/**/c"}, false},
	}
	for _, tc := range cases {
		if got := markdownPathMatchesIgnore(tc.rel, tc.globs); got != tc.want {
			t.Fatalf("rel=%q globs=%v: got %v want %v", tc.rel, tc.globs, got, tc.want)
		}
	}
}
