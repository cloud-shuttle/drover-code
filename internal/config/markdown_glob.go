package config

import (
	"path/filepath"
	"strings"
)

// markdownPathMatchesIgnore reports whether relPath (slash-separated, relative to
// the project work dir) matches any entry in globs. Patterns use forward slashes;
// * and ? follow filepath.Match; ** matches zero or more path segments.
func markdownPathMatchesIgnore(relPath string, globs []string) bool {
	relPath = filepath.ToSlash(strings.TrimPrefix(relPath, "./"))
	for _, g := range globs {
		g = strings.TrimSpace(filepath.ToSlash(g))
		if g == "" {
			continue
		}
		if pathMatchesPattern(relPath, g) {
			return true
		}
	}
	return false
}

func pathMatchesPattern(path, pattern string) bool {
	path = filepath.ToSlash(path)
	pattern = filepath.ToSlash(pattern)
	if !strings.Contains(pattern, "**") {
		ok, err := filepath.Match(pattern, path)
		return err == nil && ok
	}
	pSegs := patternSegments(pattern)
	pathSegs := pathSegments(path)
	return matchSegs(pathSegs, pSegs)
}

func patternSegments(pattern string) []string {
	pattern = strings.Trim(pattern, "/")
	if pattern == "" {
		return nil
	}
	var out []string
	for _, seg := range strings.Split(pattern, "/") {
		if seg == "" {
			continue
		}
		out = append(out, seg)
	}
	return out
}

func pathSegments(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func matchSegs(path, pat []string) bool {
	if len(pat) == 0 {
		return len(path) == 0
	}
	if pat[0] == "**" {
		if len(pat) == 1 {
			return true
		}
		for i := 0; i <= len(path); i++ {
			if matchSegs(path[i:], pat[1:]) {
				return true
			}
		}
		return false
	}
	if len(path) == 0 {
		return false
	}
	ok, err := filepath.Match(pat[0], path[0])
	if err != nil || !ok {
		return false
	}
	return matchSegs(path[1:], pat[1:])
}
