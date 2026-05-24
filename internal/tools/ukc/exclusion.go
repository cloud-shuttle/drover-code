package ukc

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

// WorkspaceLimits caps workspace payload size (ADR 0003 / workspace exclusion).
type WorkspaceLimits struct {
	MaxFileBytes  int64
	MaxTotalBytes int64
}

// DefaultWorkspaceLimits returns ADR defaults when fields are zero.
func DefaultWorkspaceLimits() WorkspaceLimits {
	return WorkspaceLimits{
		MaxFileBytes:  DefaultMaxFileBytes,
		MaxTotalBytes: DefaultMaxTotalBytes,
	}
}

func (l WorkspaceLimits) normalize() WorkspaceLimits {
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

// PlanWorkspaceUpload walks localDir and returns file count and total bytes that
// would be included after workspace exclusion rules.
func PlanWorkspaceUpload(localDir string, limits WorkspaceLimits) (UploadSummary, error) {
	limits = limits.normalize()
	filter, err := newWorkspaceFilter(localDir)
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

		if filter.skipWalk(relPath, info) {
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

type workspaceFilter struct {
	root     string
	matchers []*gitignore.GitIgnore
}

func newWorkspaceFilter(root string) (*workspaceFilter, error) {
	f := &workspaceFilter{root: root}
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

func (f *workspaceFilter) skipWalk(relPath string, info os.FileInfo) bool {
	if shouldExclude(relPath) {
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
