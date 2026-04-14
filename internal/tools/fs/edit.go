package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

// EditFile makes a targeted string-replacement in a file.
type EditFile struct {
	WorkDir string
}

type editFileInput struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

func (t *EditFile) Name() string { return "edit_file" }
func (t *EditFile) Description() string {
	return "Make a targeted replacement in a file: replace old_string with new_string. " +
		"old_string must match exactly one location in the file (use enough context to be unique). " +
		"The tool uses fuzzy whitespace normalisation when searching, so minor indentation " +
		"differences between your old_string and the file are handled automatically. " +
		"Returns a unified diff of the change."
}

func (t *EditFile) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("path", toolutil.NewSchema("string").Desc("File to edit, relative to working directory or absolute")).
		Prop("old_string", toolutil.NewSchema("string").Desc("The exact text to find and replace. Must be unique in the file. Include surrounding lines for context if needed")).
		Prop("new_string", toolutil.NewSchema("string").Desc("The replacement text. Use an empty string to delete old_string")).
		Required("path", "old_string", "new_string").
		Build()
}

func (t *EditFile) NeedsPermission(_ json.RawMessage) bool { return true }

func (t *EditFile) Execute(_ context.Context, rawInput json.RawMessage) (string, error) {
	var inp editFileInput
	if err := json.Unmarshal(rawInput, &inp); err != nil {
		return "", fmt.Errorf("edit_file: bad input: %w", err)
	}
	if inp.OldString == "" {
		return "", fmt.Errorf("edit_file: old_string cannot be empty — use write_file to create new files")
	}

	absPath, err := toolutil.SafePath(t.WorkDir, inp.Path)
	if err != nil {
		return "", fmt.Errorf("edit_file: %w", err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("edit_file: %w", err)
	}
	original := string(data)

	// --- exact match first (fast path) ---
	count := strings.Count(original, inp.OldString)
	if count == 1 {
		updated := strings.Replace(original, inp.OldString, inp.NewString, 1)
		return t.applyAndDiff(absPath, inp.Path, original, updated)
	}
	if count > 1 {
		return "", fmt.Errorf("edit_file: old_string matches %d locations in %s — make it more specific by including surrounding lines", count, inp.Path)
	}

	// --- fuzzy match: normalise whitespace on both sides ---
	normFile := normaliseWS(original)
	normOld := normaliseWS(inp.OldString)

	fuzzyCount := strings.Count(normFile, normOld)
	if fuzzyCount == 0 {
		return "", fmt.Errorf("edit_file: old_string not found in %s\n\nSearched for:\n%s", inp.Path, inp.OldString)
	}
	if fuzzyCount > 1 {
		return "", fmt.Errorf("edit_file: old_string matches %d locations after whitespace normalisation — make it more specific", fuzzyCount)
	}

	updated, ok := fuzzyReplace(original, inp.OldString, inp.NewString)
	if !ok {
		return "", fmt.Errorf("edit_file: fuzzy replacement failed unexpectedly in %s", inp.Path)
	}

	return t.applyAndDiff(absPath, inp.Path, original, updated)
}

func (t *EditFile) applyAndDiff(absPath, displayPath, original, updated string) (string, error) {
	perm := os.FileMode(0o644)
	if info, err := os.Stat(absPath); err == nil {
		perm = info.Mode().Perm()
	}
	if err := toolutil.WriteAtomic(absPath, []byte(updated), perm); err != nil {
		return "", fmt.Errorf("edit_file: write: %w", err)
	}
	return unifiedDiff(displayPath, original, updated), nil
}

func normaliseWS(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		fields := strings.FieldsFunc(l, unicode.IsSpace)
		lines[i] = strings.Join(fields, " ")
	}
	return strings.Join(lines, "\n")
}

func fuzzyReplace(original, oldStr, newStr string) (string, bool) {
	oldLines := strings.Split(oldStr, "\n")
	fileLines := strings.Split(original, "\n")

	normOldLines := make([]string, len(oldLines))
	for i, l := range oldLines {
		normOldLines[i] = normaliseWS(l)
	}
	normFileLines := make([]string, len(fileLines))
	for i, l := range fileLines {
		normFileLines[i] = normaliseWS(l)
	}

	for startIdx := 0; startIdx <= len(fileLines)-len(oldLines); startIdx++ {
		match := true
		for j, normOld := range normOldLines {
			if normFileLines[startIdx+j] != normOld {
				match = false
				break
			}
		}
		if !match {
			continue
		}

		endIdx := startIdx + len(oldLines)
		prefix := strings.Join(fileLines[:startIdx], "\n")
		suffix := strings.Join(fileLines[endIdx:], "\n")

		var result string
		switch {
		case prefix == "" && suffix == "":
			result = newStr
		case prefix == "":
			result = newStr + "\n" + suffix
		case suffix == "":
			result = prefix + "\n" + newStr
		default:
			result = prefix + "\n" + newStr + "\n" + suffix
		}
		return result, true
	}
	return "", false
}

func unifiedDiff(path, original, updated string) string {
	if original == updated {
		return "no changes"
	}

	origLines := strings.Split(original, "\n")
	newLines := strings.Split(updated, "\n")

	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ %s\n", path, path)

	firstDiff := 0
	for firstDiff < len(origLines) && firstDiff < len(newLines) {
		if origLines[firstDiff] != newLines[firstDiff] {
			break
		}
		firstDiff++
	}

	lastOrig := len(origLines) - 1
	lastNew := len(newLines) - 1
	for lastOrig > firstDiff && lastNew > firstDiff {
		if origLines[lastOrig] != newLines[lastNew] {
			break
		}
		lastOrig--
		lastNew--
	}

	contextBefore := max(0, firstDiff-3)
	contextAfterOrig := min(len(origLines)-1, lastOrig+3)
	contextAfterNew := min(len(newLines)-1, lastNew+3)

	origRange := contextAfterOrig - contextBefore + 1
	newRange := contextAfterNew - contextBefore + 1

	fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n",
		contextBefore+1, origRange,
		contextBefore+1, newRange,
	)

	for i := contextBefore; i < firstDiff; i++ {
		fmt.Fprintf(&b, " %s\n", origLines[i])
	}
	for i := firstDiff; i <= lastOrig && i < len(origLines); i++ {
		fmt.Fprintf(&b, "-%s\n", origLines[i])
	}
	for i := firstDiff; i <= lastNew && i < len(newLines); i++ {
		fmt.Fprintf(&b, "+%s\n", newLines[i])
	}
	end := min(contextAfterOrig, contextAfterNew)
	for i := lastOrig + 1; i <= end && i < len(origLines); i++ {
		fmt.Fprintf(&b, " %s\n", origLines[i])
	}

	return b.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

