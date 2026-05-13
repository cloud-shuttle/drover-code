package diff

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PatchApplier safely applies only accepted hunks to a file
type PatchApplier struct {
	workDir string
}

func NewPatchApplier(workDir string) *PatchApplier {
	return &PatchApplier{workDir: workDir}
}

// ApplyAcceptedHunks applies only the accepted hunks from a diff to the target file
func (p *PatchApplier) ApplyAcceptedHunks(filePath string, hunks []Hunk) (int, error) {
	fullPath := filepath.Join(p.workDir, filePath)

	// Read original file
	original, err := os.ReadFile(fullPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	lines := strings.Split(string(original), "\n")
	appliedCount := 0

	// Process hunks in reverse order to avoid line number shifts
	for i := len(hunks) - 1; i >= 0; i-- {
		hunk := hunks[i]
		if !hunk.Accepted || hunk.Rejected {
			continue
		}

		// Apply this hunk
		if err := p.applyHunk(&lines, hunk); err != nil {
			return appliedCount, fmt.Errorf("failed to apply hunk at line %d: %w", hunk.OldStart, err)
		}
		appliedCount++
	}

	if appliedCount == 0 {
		return 0, nil
	}

	newContent := strings.Join(lines, "\n")

	// Write back to file
	perm := os.FileMode(0o644)
	if info, err := os.Stat(fullPath); err == nil {
		perm = info.Mode().Perm()
	}

	tmpPath := fullPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(newContent), perm); err != nil {
		return appliedCount, fmt.Errorf("failed to write tmp file: %w", err)
	}
	if err := os.Rename(tmpPath, fullPath); err != nil {
		return appliedCount, fmt.Errorf("failed to swap file: %w", err)
	}

	return appliedCount, nil
}

// applyHunk applies a single hunk to the line slice (modifies in place)
func (p *PatchApplier) applyHunk(lines *[]string, hunk Hunk) error {
	// Calculate insertion point (adjust for 1-based diff indexing)
	insertAt := hunk.OldStart - 1
	if insertAt < 0 {
		insertAt = 0
	}
	if insertAt > len(*lines) {
		insertAt = len(*lines)
	}

	// Remove old lines specified in the hunk (including context lines that are being "replaced")
	removeCount := hunk.OldLines
	if removeCount > 0 && insertAt+removeCount <= len(*lines) {
		*lines = append((*lines)[:insertAt], (*lines)[insertAt+removeCount:]...)
	} else if removeCount > 0 {
		// remove till end
		*lines = (*lines)[:insertAt]
	}

	// Insert new lines (including context lines) from RawLines
	// Filter out the '-' lines from RawLines, keep ' ' (context) and '+' lines.
	var newLines []string
	for _, line := range hunk.RawLines {
		if strings.HasPrefix(line, "+") {
			newLines = append(newLines, strings.TrimPrefix(line, "+"))
		} else if !strings.HasPrefix(line, "-") {
			// context line
			if len(line) > 0 && line[0] == ' ' {
				newLines = append(newLines, line[1:])
			} else {
				newLines = append(newLines, line)
			}
		}
	}

	*lines = append((*lines)[:insertAt], append(newLines, (*lines)[insertAt:]...)...)

	return nil
}
