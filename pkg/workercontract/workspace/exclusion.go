package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
)

const (
	DefaultMaxFileBytes  = 10 * 1024 * 1024  // 10 MiB
	DefaultMaxTotalBytes = 500 * 1024 * 1024 // 500 MiB
)

var rootExcludes = []string{
	"drover-local",
	"drover-code",
	"claude-go",
	"ukc-agent",
	"cmd/ukc-agent/ukc-agent",
	"bin",
	"unikraft",
}

var anywhereExcludes = []string{
	".git",
	"node_modules",
	"dist",
	"target",
	"__pycache__",
	".venv",
	"venv",
	".unikraft",
	".drover-code-workers",
}

// ShouldExclude returns true if the relative path matches hardcoded global exclusions.
func ShouldExclude(relPath string) bool {
	relPath = filepath.ToSlash(relPath)

	for _, ex := range rootExcludes {
		if relPath == ex || strings.HasPrefix(relPath, ex+"/") {
			return true
		}
	}

	for _, ex := range anywhereExcludes {
		if relPath == ex || strings.HasPrefix(relPath, ex+"/") || strings.Contains(relPath, "/"+ex+"/") || strings.HasSuffix(relPath, "/"+ex) {
			return true
		}
	}
	return false
}

// Limits caps workspace payload size (ADR 0003 / workspace exclusion).
type Limits struct {
	MaxFileBytes  int64
	MaxTotalBytes int64
}

// DefaultLimits returns ADR defaults when fields are zero.
func DefaultLimits() Limits {
	return Limits{
		MaxFileBytes:  DefaultMaxFileBytes,
		MaxTotalBytes: DefaultMaxTotalBytes,
	}
}

func (l Limits) normalize() Limits {
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = DefaultMaxFileBytes
	}
	if l.MaxTotalBytes <= 0 {
		l.MaxTotalBytes = DefaultMaxTotalBytes
	}
	return l
}

// UploadSummary describes a planned workspace payload before upload.
type UploadSummary struct {
	FileCount  int
	TotalBytes int64
}

// PlanUpload walks localDir and returns file count and total bytes that
// would be included after workspace exclusion rules.
func PlanUpload(localDir string, limits Limits) (UploadSummary, error) {
	limits = limits.normalize()
	filter, err := NewFilter(localDir)
	if err != nil {
		return UploadSummary{}, err
	}

	var summary UploadSummary
	err = filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == localDir {
			return nil
		}
		relPath, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		if filter.SkipWalk(relPath, info) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		size := info.Size()
		if size > limits.MaxFileBytes {
			return fmt.Errorf("workspace exclusion: file %s exceeds max size (%d > %d bytes)", relPath, size, limits.MaxFileBytes)
		}
		summary.FileCount++
		summary.TotalBytes += size
		if summary.TotalBytes > limits.MaxTotalBytes {
			return fmt.Errorf("workspace exclusion: total payload exceeds max (%d > %d bytes)", summary.TotalBytes, limits.MaxTotalBytes)
		}
		return nil
	})
	return summary, err
}

type Filter struct {
	root     string
	matchers []*gitignore.GitIgnore
}

// NewFilter creates a workspace filter that respects .gitignore, .droverignore, and secret exclusions.
func NewFilter(root string) (*Filter, error) {
	f := &Filter{root: root}
	for _, name := range []string{".gitignore", ".droverignore"} {
		path := filepath.Join(root, name)
		m, err := gitignore.CompileIgnoreFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("compile %s: %w", name, err)
		}
		f.matchers = append(f.matchers, m)
	}
	return f, nil
}

// SkipWalk returns true if the file should be excluded from the workspace payload.
func (f *Filter) SkipWalk(relPath string, info os.FileInfo) bool {
	if ShouldExclude(relPath) {
		return true
	}
	if isSecretPath(relPath) {
		return true
	}
	for _, m := range f.matchers {
		if m != nil && m.MatchesPath(relPath) {
			return true
		}
	}
	return false
}

func isSecretPath(relPath string) bool {
	relPath = filepath.ToSlash(relPath)
	base := filepath.Base(relPath)
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key") {
		return true
	}
	if strings.HasPrefix(strings.ToLower(base), "credentials") {
		return true
	}
	return false
}
