// Package fs implements file-system tools: read_file, write_file, edit_file.
package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

// ReadFile reads a file and returns its content, optionally sliced to a
// line range. It is safe: it detects binary files and refuses them.
type ReadFile struct {
	WorkDir string
}

type readFileInput struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"` // 1-based, inclusive; 0 = from start
	EndLine   int    `json:"end_line"`   // 1-based, inclusive; 0 = to end
}

func (t *ReadFile) Name() string { return "read_file" }
func (t *ReadFile) Description() string {
	return "Read the contents of a file. Returns the file content as a string. " +
		"Use start_line and end_line to read a specific range (1-based, inclusive). " +
		"Returns an error for binary files."
}

func (t *ReadFile) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("path", toolutil.NewSchema("string").Desc("Path to the file, relative to the working directory or absolute")).
		Prop("start_line", toolutil.NewSchema("integer").Desc("First line to return (1-based). Omit to start from the beginning")).
		Prop("end_line", toolutil.NewSchema("integer").Desc("Last line to return (1-based). Omit to read to the end of the file")).
		Required("path").
		Build()
}

func (t *ReadFile) NeedsPermission(_ json.RawMessage) bool { return false }

func (t *ReadFile) Execute(_ context.Context, rawInput json.RawMessage) (string, error) {
	var inp readFileInput
	if err := json.Unmarshal(rawInput, &inp); err != nil {
		return "", fmt.Errorf("read_file: bad input: %w", err)
	}

	absPath, err := toolutil.SafePath(t.WorkDir, inp.Path)
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("read_file: %w", err)
	}

	// Refuse binary files — they confuse the model and waste context.
	if isBinary(data) {
		return "", fmt.Errorf("read_file: %q appears to be a binary file", inp.Path)
	}

	content := string(data)

	// Apply line range if requested.
	if inp.StartLine > 0 || inp.EndLine > 0 {
		content, err = sliceLines(content, inp.StartLine, inp.EndLine)
		if err != nil {
			return "", fmt.Errorf("read_file: %w", err)
		}
	}

	return toolutil.Truncate(content), nil
}

// isBinary returns true if the data appears to be binary.
func isBinary(data []byte) bool {
	sample := data
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	if !utf8.Valid(sample) {
		return true
	}
	for _, b := range sample {
		if b == 0 {
			return true
		}
	}
	return false
}

// sliceLines returns lines [start, end] (both 1-based, inclusive).
// 0 for start means "from line 1"; 0 for end means "to last line".
func sliceLines(content string, start, end int) (string, error) {
	lines := strings.Split(content, "\n")
	total := len(lines)

	if start == 0 {
		start = 1
	}
	if end == 0 {
		end = total
	}
	if start < 1 {
		start = 1
	}
	if end > total {
		end = total
	}
	if start > end {
		return "", fmt.Errorf("start_line (%d) > end_line (%d)", start, end)
	}

	// Annotate lines with their numbers so the model can reference them.
	var b strings.Builder
	for i := start; i <= end; i++ {
		fmt.Fprintf(&b, "%6d\t%s\n", i, lines[i-1])
	}
	return b.String(), nil
}

