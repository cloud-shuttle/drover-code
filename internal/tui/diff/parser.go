package diff

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseUnifiedDiff parses a raw unified diff string into a slice of Hunks.
func ParseUnifiedDiff(diffStr string) ([]Hunk, error) {
	var hunks []Hunk
	lines := strings.Split(diffStr, "\n")

	var currentHunk *Hunk

	for _, line := range lines {
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}

		if strings.HasPrefix(line, "@@ ") {
			if currentHunk != nil {
				hunks = append(hunks, *currentHunk)
			}
			
			// Parse @@ -start,length +start,length @@
			// e.g. @@ -1,4 +1,5 @@
			parts := strings.Split(line, " ")
			if len(parts) >= 3 {
				oldPart := parts[1] // -1,4
				oldPart = strings.TrimPrefix(oldPart, "-")
				
				oldStart := 0
				oldLines := 1 // defaults to 1 if ,length is omitted
				
				if strings.Contains(oldPart, ",") {
					sp := strings.Split(oldPart, ",")
					oldStart, _ = strconv.Atoi(sp[0])
					oldLines, _ = strconv.Atoi(sp[1])
				} else {
					oldStart, _ = strconv.Atoi(oldPart)
				}

				currentHunk = &Hunk{
					Header:   line,
					OldStart: oldStart,
					OldLines: oldLines,
				}
			} else {
				currentHunk = &Hunk{Header: line}
			}
			continue
		}

		if currentHunk != nil {
			currentHunk.RawLines = append(currentHunk.RawLines, line)
			if strings.HasPrefix(line, "-") {
				currentHunk.OldContent = append(currentHunk.OldContent, line)
			} else if strings.HasPrefix(line, "+") {
				currentHunk.NewContent = append(currentHunk.NewContent, line)
			} else {
				currentHunk.Context = append(currentHunk.Context, line)
			}
		}
	}

	if currentHunk != nil {
		hunks = append(hunks, *currentHunk)
	}

	if len(hunks) == 0 {
		return nil, fmt.Errorf("no hunks found")
	}

	return hunks, nil
}
