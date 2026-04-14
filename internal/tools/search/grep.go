package search

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

const maxGrepMatches = 500

// Grep searches files for a regex pattern.
type Grep struct {
	WorkDir string
}

type grepInput struct {
	Pattern       string `json:"pattern"`
	Path          string `json:"path"`
	FilePattern   string `json:"file_pattern"`
	ContextLines  int    `json:"context_lines"`
	CaseSensitive bool   `json:"case_sensitive"`
	MaxMatches    int    `json:"max_matches"`
}

func (t *Grep) Name() string { return "grep" }
func (t *Grep) Description() string {
	return "Search for a regex pattern in files. Returns matching lines with file path and line number. " +
		"Uses ripgrep (rg) if available for speed, otherwise pure Go. " +
		"Automatically skips binary files and respects .gitignore. " +
		"Set context_lines to include surrounding lines for each match."
}

func (t *Grep) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("pattern", toolutil.NewSchema("string").Desc("Regular expression to search for")).
		Prop("path", toolutil.NewSchema("string").Desc("File or directory to search. Defaults to the working directory")).
		Prop("file_pattern", toolutil.NewSchema("string").Desc("Only search files matching this glob, e.g. '*.go' or '*.ts'")).
		Prop("context_lines", toolutil.NewSchema("integer").Desc("Number of lines before and after each match to include (default: 2)")).
		Prop("case_sensitive", toolutil.NewSchema("boolean").Desc("Whether the search is case-sensitive (default: false)")).
		Prop("max_matches", toolutil.NewSchema("integer").Desc("Stop after this many matches (default: 500)")).
		Required("pattern").
		Build()
}

func (t *Grep) NeedsPermission(_ json.RawMessage) bool { return false }

func (t *Grep) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
	var inp grepInput
	if err := json.Unmarshal(rawInput, &inp); err != nil {
		return "", fmt.Errorf("grep: bad input: %w", err)
	}
	if inp.Pattern == "" {
		return "", fmt.Errorf("grep: pattern cannot be empty")
	}

	contextLines := inp.ContextLines
	if contextLines == 0 {
		contextLines = 2
	}
	maxMatches := inp.MaxMatches
	if maxMatches == 0 {
		maxMatches = maxGrepMatches
	}

	searchPath := t.WorkDir
	if inp.Path != "" {
		var err error
		searchPath, err = toolutil.SafePath(t.WorkDir, inp.Path)
		if err != nil {
			return "", fmt.Errorf("grep: %w", err)
		}
	}

	if rgPath, err := exec.LookPath("rg"); err == nil {
		return t.grepWithRg(ctx, rgPath, inp.Pattern, searchPath, inp.FilePattern,
			contextLines, maxMatches, inp.CaseSensitive)
	}
	return t.grepPureGo(ctx, inp.Pattern, searchPath, inp.FilePattern,
		contextLines, maxMatches, inp.CaseSensitive)
}

func (t *Grep) grepWithRg(
	ctx context.Context,
	rgPath, pattern, searchPath, filePattern string,
	contextLines, maxMatches int,
	caseSensitive bool,
) (string, error) {
	args := []string{
		"--line-number",
		"--no-heading",
		"--color=never",
		fmt.Sprintf("--context=%d", contextLines),
		fmt.Sprintf("--max-count=%d", maxMatches),
	}
	if !caseSensitive {
		args = append(args, "--ignore-case")
	}
	if filePattern != "" {
		args = append(args, "--glob", filePattern)
	}
	args = append(args, pattern, searchPath)

	cmd := exec.CommandContext(ctx, rgPath, args...)
	cmd.Dir = t.WorkDir
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return fmt.Sprintf("no matches for %q", pattern), nil
		}
		return "", fmt.Errorf("grep: rg failed: %w", err)
	}
	return toolutil.Truncate(out.String()), nil
}

func (t *Grep) grepPureGo(
	ctx context.Context,
	pattern, searchPath, filePattern string,
	contextLines, maxMatches int,
	caseSensitive bool,
) (string, error) {
	regexStr := pattern
	if !caseSensitive {
		regexStr = "(?i)" + pattern
	}
	re, err := regexp.Compile(regexStr)
	if err != nil {
		return "", fmt.Errorf("grep: invalid pattern %q: %w", pattern, err)
	}

	var results []string
	matchCount := 0

	err = filepath.WalkDir(searchPath, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if filePattern != "" {
			ok, _ := filepath.Match(filePattern, d.Name())
			if !ok {
				return nil
			}
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}

		fileResults, n, err := searchFile(ctx, re, path, t.WorkDir, contextLines)
		if err != nil || len(fileResults) == 0 {
			return nil
		}
		results = append(results, fileResults...)
		matchCount += n
		if matchCount >= maxMatches {
			return fmt.Errorf("max matches reached")
		}
		return nil
	})
	if err != nil && err.Error() != "max matches reached" {
		return "", fmt.Errorf("grep: walk: %w", err)
	}

	if len(results) == 0 {
		return fmt.Sprintf("no matches for %q", pattern), nil
	}

	return toolutil.Truncate(strings.Join(results, "")), nil
}

func searchFile(ctx context.Context, re *regexp.Regexp, path, baseDir string, contextLines int) ([]string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, nil
	}
	defer f.Close()

	rel, err := filepath.Rel(baseDir, path)
	if err != nil {
		rel = path
	}

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	for _, l := range lines[:minInt(len(lines), 5)] {
		if strings.ContainsRune(l, 0) {
			return nil, 0, nil
		}
	}

	var results []string
	matchCount := 0

	for i, line := range lines {
		if !re.MatchString(line) {
			continue
		}
		matchCount++

		var block strings.Builder
		start := maxInt(0, i-contextLines)
		for j := start; j < i; j++ {
			fmt.Fprintf(&block, "%s-%d-%s\n", rel, j+1, lines[j])
		}
		fmt.Fprintf(&block, "%s:%d:%s\n", rel, i+1, line)
		end := minInt(len(lines)-1, i+contextLines)
		for j := i + 1; j <= end; j++ {
			fmt.Fprintf(&block, "%s-%d-%s\n", rel, j+1, lines[j])
		}
		block.WriteString("--\n")
		results = append(results, block.String())

		select {
		case <-ctx.Done():
			return results, matchCount, nil
		default:
		}
	}
	return results, matchCount, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

