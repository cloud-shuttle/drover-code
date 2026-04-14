// Package shell implements the bash tool.
package shell

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

const (
	defaultTimeoutSecs = 120
	maxTimeoutSecs     = 600
)

// Bash executes a shell command and returns combined stdout+stderr.
type Bash struct {
	WorkDir string
}

type bashInput struct {
	Command    string `json:"command"`
	TimeoutSec int    `json:"timeout_seconds"`
	WorkDir    string `json:"working_directory"`
}

func (t *Bash) Name() string { return "bash" }
func (t *Bash) Description() string {
	return "Execute a bash command and return its output. " +
		"stdout and stderr are returned separately with exit code. " +
		"Default timeout is 120 seconds (max 600). " +
		"The command runs in the project working directory unless working_directory is set. " +
		"Avoid commands that run indefinitely — prefer timeouts in the command itself (e.g. curl --max-time 10)."
}

func (t *Bash) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("command", toolutil.NewSchema("string").Desc("The bash command to execute. Supports pipelines, redirects, and multi-statement commands")).
		Prop("timeout_seconds", toolutil.NewSchema("integer").Desc("Maximum execution time in seconds (default: 120, max: 600)")).
		Prop("working_directory", toolutil.NewSchema("string").Desc("Directory to run the command in. Defaults to the project working directory")).
		Required("command").
		Build()
}

func (t *Bash) NeedsPermission(rawInput json.RawMessage) bool { return true }

func (t *Bash) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
	var inp bashInput
	if err := json.Unmarshal(rawInput, &inp); err != nil {
		return "", fmt.Errorf("bash: bad input: %w", err)
	}
	if inp.Command == "" {
		return "", fmt.Errorf("bash: command cannot be empty")
	}

	timeout := time.Duration(inp.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = defaultTimeoutSecs * time.Second
	}
	if timeout > maxTimeoutSecs*time.Second {
		timeout = maxTimeoutSecs * time.Second
	}

	workDir := t.WorkDir
	if inp.WorkDir != "" {
		var err error
		workDir, err = toolutil.SafePath(t.WorkDir, inp.WorkDir)
		if err != nil {
			return "", fmt.Errorf("bash: %w", err)
		}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", inp.Command)
	cmd.Dir = workDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(startTime).Round(time.Millisecond)

	exitCode := 0
	if runErr != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("bash: command timed out after %s: %s", timeout, inp.Command)
		}
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return "", fmt.Errorf("bash: exec failed: %w", runErr)
		}
	}

	return formatBashOutput(
		inp.Command,
		stdout.String(),
		stderr.String(),
		exitCode,
		elapsed,
	), nil
}

func formatBashOutput(command, stdout, stderr string, exitCode int, elapsed time.Duration) string {
	var b strings.Builder

	fmt.Fprintf(&b, "$ %s\n", command)
	fmt.Fprintf(&b, "exit_code: %d  elapsed: %s\n", exitCode, elapsed)

	if stdout != "" {
		b.WriteString("\n[stdout]\n")
		b.WriteString(strings.TrimRight(stdout, "\n"))
		b.WriteString("\n")
	}
	if stderr != "" {
		b.WriteString("\n[stderr]\n")
		b.WriteString(strings.TrimRight(stderr, "\n"))
		b.WriteString("\n")
	}
	if stdout == "" && stderr == "" {
		b.WriteString("\n(no output)\n")
	}

	return toolutil.Truncate(b.String())
}
