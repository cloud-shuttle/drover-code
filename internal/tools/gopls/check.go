// Package gopls runs gopls check on one Go file and formats the result.
package gopls

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	checkTimeout = 30 * time.Second
	maxFindings  = 20
	maxMessage   = 200
)

// diagLine matches gopls check output: path:line:col[-end]: message.
var diagLine = regexp.MustCompile(`^(.*):(\d+):\d+(?:-\d+(?::\d+)?)?: (.*)$`)

// One check at a time. The agent can edit several files in parallel, and two
// gopls processes loading the same module compete for CPU.
var checkMu sync.Mutex

type finding struct {
	line    int
	message string
}

// Annotate returns the diagnostics block to append to a tool result.
// A non-Go file returns an empty string. A gopls or module problem is reported
// in the text; the caller still keeps the edit.
// ponytail: text from gopls check for one file; add another language when a second server is wired the same way.
func Annotate(ctx context.Context, displayPath, absPath string) string {
	if !strings.HasSuffix(strings.ToLower(absPath), ".go") {
		return ""
	}
	text, err := check(ctx, displayPath, absPath)
	if err != nil {
		text = "gopls: " + err.Error()
	}
	return "\n\n" + text
}

func check(ctx context.Context, displayPath, absPath string) (string, error) {
	if _, err := exec.LookPath("gopls"); err != nil {
		return "", errors.New("not on PATH")
	}
	dir := filepath.Dir(absPath)
	mod, err := moduleFile(ctx, dir)
	if err != nil {
		return "", err
	}
	if mod == "" || mod == os.DevNull {
		return "", errors.New("file is not in a Go module")
	}

	cctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	checkMu.Lock()
	defer checkMu.Unlock()

	cmd := exec.CommandContext(cctx, "gopls", "check", "-severity=warning", absPath)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if cctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("timed out after %s", checkTimeout)
	}

	found := parseDiagnostics(stdout.String(), absPath)
	if runErr != nil && len(found) == 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
		return "", fmt.Errorf("check failed: %s", firstLine(msg))
	}
	return formatDiagnostics(displayPath, found), nil
}

func moduleFile(ctx context.Context, dir string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "go", "env", "GOMOD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if cctx.Err() == context.DeadlineExceeded {
			return "", errors.New("timed out finding the Go module")
		}
		return "", fmt.Errorf("cannot find the Go module: %s", firstLine(err.Error()))
	}
	return strings.TrimSpace(string(out)), nil
}

func parseDiagnostics(out, absPath string) []finding {
	var found []finding
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		m := diagLine.FindStringSubmatch(line)
		if m == nil || !sameFile(m[1], absPath) {
			continue
		}
		var lineNo int
		fmt.Sscanf(m[2], "%d", &lineNo)
		msg := clip(strings.TrimSpace(m[3]), maxMessage)
		key := fmt.Sprintf("%d\n%s", lineNo, msg)
		if seen[key] {
			continue
		}
		seen[key] = true
		found = append(found, finding{line: lineNo, message: msg})
	}
	return found
}

func formatDiagnostics(display string, found []finding) string {
	if len(found) == 0 {
		return "gopls: no diagnostics"
	}
	extra := 0
	if len(found) > maxFindings {
		extra = len(found) - maxFindings
		found = found[:maxFindings]
	}
	var b strings.Builder
	b.WriteString("gopls:")
	for _, f := range found {
		fmt.Fprintf(&b, "\n%s:%d: %s", display, f.line, f.message)
	}
	if extra > 0 {
		fmt.Fprintf(&b, "\n… and %d more", extra)
	}
	return b.String()
}

func sameFile(printed, absPath string) bool {
	if filepath.Clean(printed) == filepath.Clean(absPath) {
		return true
	}
	a, err1 := filepath.EvalSymlinks(printed)
	b, err2 := filepath.EvalSymlinks(absPath)
	return err1 == nil && err2 == nil && filepath.Clean(a) == filepath.Clean(b)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return clip(s, maxMessage)
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for k := range s {
		if i == n-1 {
			return s[:k] + "…"
		}
		i++
	}
	return s
}
