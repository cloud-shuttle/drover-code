package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

// ListDirectory lists the contents of a directory.
type ListDirectory struct {
	WorkDir string
}

type listDirInput struct {
	Path string `json:"path"`
}

func (t *ListDirectory) Name() string { return "list_directory" }
func (t *ListDirectory) Description() string {
	return "List the files and directories in a given directory. " +
		"Shows name, type (file/dir), size, and last-modified time. " +
		"Does not recurse — use glob for recursive listings."
}

func (t *ListDirectory) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("path", toolutil.NewSchema("string").Desc("Directory path, relative or absolute. Use '.' for the working directory")).
		Required("path").
		Build()
}

func (t *ListDirectory) NeedsPermission(_ json.RawMessage) bool { return false }

func (t *ListDirectory) Execute(_ context.Context, rawInput json.RawMessage) (string, error) {
	var inp listDirInput
	if err := json.Unmarshal(rawInput, &inp); err != nil {
		return "", fmt.Errorf("list_directory: bad input: %w", err)
	}

	absPath, err := toolutil.SafePath(t.WorkDir, inp.Path)
	if err != nil {
		return "", fmt.Errorf("list_directory: %w", err)
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return "", fmt.Errorf("list_directory: %w", err)
	}

	if len(entries) == 0 {
		return fmt.Sprintf("%s (empty directory)", inp.Path), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n", inp.Path)

	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}

		typ := "file"
		sizeStr := formatSize(info.Size())
		if e.IsDir() {
			typ = "dir "
			sizeStr = "      "
		} else if info.Mode()&os.ModeSymlink != 0 {
			typ = "link"
		}

		fmt.Fprintf(&b, "  %s  %s  %s  %s\n",
			typ,
			sizeStr,
			info.ModTime().Format(time.RFC3339)[:16],
			e.Name(),
		)
	}
	return b.String(), nil
}

func formatSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%6dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%5.1fK", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%5.1fM", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%5.1fG", float64(n)/(1024*1024*1024))
	}
}

// FileInfo returns metadata about a single path: stat equivalent.
type FileInfo struct {
	WorkDir string
}

type fileInfoInput struct {
	Path string `json:"path"`
}

func (t *FileInfo) Name() string { return "file_info" }
func (t *FileInfo) Description() string {
	return "Return metadata for a file or directory: size, permissions, modification time, " +
		"and whether it exists. Does not read file content."
}

func (t *FileInfo) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("path", toolutil.NewSchema("string").Desc("Path to inspect")).
		Required("path").
		Build()
}

func (t *FileInfo) NeedsPermission(_ json.RawMessage) bool { return false }

func (t *FileInfo) Execute(_ context.Context, rawInput json.RawMessage) (string, error) {
	var inp fileInfoInput
	if err := json.Unmarshal(rawInput, &inp); err != nil {
		return "", fmt.Errorf("file_info: bad input: %w", err)
	}

	absPath, err := toolutil.SafePath(t.WorkDir, inp.Path)
	if err != nil {
		return "", fmt.Errorf("file_info: %w", err)
	}

	// Use Lstat so we can correctly report symlinks (Stat follows symlinks).
	info, err := os.Lstat(absPath)
	if os.IsNotExist(err) {
		return fmt.Sprintf("path %q does not exist", inp.Path), nil
	}
	if err != nil {
		return "", fmt.Errorf("file_info: %w", err)
	}

	kind := "file"
	if info.IsDir() {
		kind = "directory"
	} else if info.Mode()&os.ModeSymlink != 0 {
		kind = "symlink"
	}

	extra := ""
	if kind == "symlink" {
		if target, err := filepath.EvalSymlinks(absPath); err == nil {
			extra = fmt.Sprintf("\n  target:  %s", target)
		}
	}

	return fmt.Sprintf(
		"path:     %s\ntype:     %s\nsize:     %d bytes\nmode:     %s\nmodified: %s%s",
		inp.Path, kind,
		info.Size(),
		info.Mode().String(),
		info.ModTime().Format(time.RFC3339),
		extra,
	), nil
}

