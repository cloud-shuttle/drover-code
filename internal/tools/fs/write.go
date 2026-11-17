package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudshuttle/drover-code/internal/tools/gopls"
	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

// WriteFile creates or completely overwrites a file.
// Destructive — always requires permission.
type WriteFile struct {
	WorkDir string
	// Gopls appends diagnostics for a .go file. Local sessions set this.
	Gopls bool
}

type writeFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *WriteFile) Name() string { return "write_file" }
func (t *WriteFile) Description() string {
	desc := "Write content to a file, creating it if it does not exist or completely replacing " +
		"it if it does. Creates parent directories as needed. " +
		"For making targeted changes to an existing file, prefer edit_file instead."
	if t.Gopls {
		desc += " On a .go file, the result includes gopls errors and warnings for that file."
	}
	return desc
}

func (t *WriteFile) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("path", toolutil.NewSchema("string").Desc("Path to write, relative to working directory or absolute")).
		Prop("content", toolutil.NewSchema("string").Desc("Full content to write to the file")).
		Required("path", "content").
		Build()
}

func (t *WriteFile) NeedsPermission(_ json.RawMessage) bool { return true }

func (t *WriteFile) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
	var inp writeFileInput
	if err := json.Unmarshal(rawInput, &inp); err != nil {
		return "", fmt.Errorf("write_file: bad input: %w", err)
	}

	absPath, err := toolutil.SafePath(t.WorkDir, inp.Path)
	if err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}

	// Create parent directories if needed.
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return "", fmt.Errorf("write_file: create dirs: %w", err)
	}

	// Preserve existing file permissions if the file already exists.
	perm := os.FileMode(0o644)
	if info, err := os.Stat(absPath); err == nil {
		perm = info.Mode().Perm()
	}

	if err := toolutil.WriteAtomic(absPath, []byte(inp.Content), perm); err != nil {
		return "", fmt.Errorf("write_file: %w", err)
	}

	msg := fmt.Sprintf("wrote %d bytes to %s", len(inp.Content), inp.Path)
	if t.Gopls {
		msg += gopls.Annotate(ctx, inp.Path, absPath)
	}
	return msg, nil
}
