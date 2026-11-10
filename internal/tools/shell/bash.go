// Package shell implements the bash tool.
package shell

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

const (
	defaultTimeoutSecs = 120
	maxTimeoutSecs     = 600
)

// Notice is one update for a background command.
// Done is false when the command starts. Output is set when it finishes.
type Notice struct {
	ID      string
	Running int
	Output  string
	Done    bool
}

// Bash executes a shell command and returns combined stdout+stderr.
type Bash struct {
	WorkDir         string
	AllowBackground bool
	// Sandbox keeps the command inside WorkDir and blocks the network.
	// Local sessions on macOS and Linux turn it on. The default leaves tests unsandboxed.
	Sandbox bool

	mu      sync.Mutex
	next    int
	jobs    map[string]context.CancelFunc
	onDone  func(Notice)
	stopped bool
}

type bashInput struct {
	Command    string `json:"command"`
	TimeoutSec int    `json:"timeout_seconds"`
	WorkDir    string `json:"working_directory"`
	Background bool   `json:"background"`
}

func (t *Bash) Name() string { return "bash" }
func (t *Bash) Description() string {
	desc := "Execute a bash command and return its output. " +
		"stdout and stderr are returned separately with exit code. " +
		"Default timeout is 120 seconds (max 600). " +
		"The command runs in the project working directory unless working_directory is set. " +
		"Avoid commands that run indefinitely — prefer timeouts in the command itself (e.g. curl --max-time 10)."
	if t != nil && t.AllowBackground {
		desc += " Set background true for tests, builds, and installs: the tool returns a job id immediately and the result arrives as a later message. Leave it false for short commands. Background is available in a local session only."
	}
	if t != nil && t.Sandbox {
		desc += " This session sandboxes the command: it can write only inside the working tree and a private temp directory, and network is blocked."
	}
	return desc
}

func (t *Bash) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("command", toolutil.NewSchema("string").Desc("The bash command to execute. Supports pipelines, redirects, and multi-statement commands")).
		Prop("timeout_seconds", toolutil.NewSchema("integer").Desc("Maximum execution time in seconds (default: 120, max: 600)")).
		Prop("working_directory", toolutil.NewSchema("string").Desc("Directory to run the command in. Defaults to the project working directory")).
		Prop("background", toolutil.NewSchema("boolean").Desc("Start the command and return a job id immediately. The result arrives as a later message. Use for tests, builds, and installs. Local sessions only.")).
		Required("command").
		Build()
}

// SetOnDone receives a notice when a background command starts and when it finishes.
func (t *Bash) SetOnDone(fn func(Notice)) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.onDone = fn
	t.mu.Unlock()
}

// Running reports how many background commands are still alive.
func (t *Bash) Running() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.jobs)
}

// StopAll kills every background command. Later notices are not delivered.
func (t *Bash) StopAll() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.stopped = true
	cancels := make([]context.CancelFunc, 0, len(t.jobs))
	for _, cancel := range t.jobs {
		cancels = append(cancels, cancel)
	}
	t.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
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

	if inp.Background {
		if !t.AllowBackground {
			return "", fmt.Errorf("bash: background commands are only available in a local session; run this command in the foreground")
		}
		return t.startBackground(inp.Command, workDir, timeout)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd, cleanup, err := t.command(cmdCtx, workDir, inp.Command)
	if err != nil {
		return "", err
	}
	defer cleanup()

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

func (t *Bash) startBackground(command, workDir string, timeout time.Duration) (string, error) {
	jobCtx, cancel := context.WithTimeout(context.Background(), timeout)
	cmd, cleanup, err := t.command(jobCtx, workDir, command)
	if err != nil {
		cancel()
		return "", err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cleanup()
		cancel()
		return "", fmt.Errorf("bash: exec failed: %w", err)
	}

	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		cancel()
		_ = cmd.Wait()
		cleanup()
		return "", fmt.Errorf("bash: background commands are stopped")
	}
	if t.jobs == nil {
		t.jobs = make(map[string]context.CancelFunc)
	}
	t.next++
	id := fmt.Sprintf("b%d", t.next)
	t.jobs[id] = cancel
	running := len(t.jobs)
	t.mu.Unlock()

	t.emit(Notice{ID: id, Running: running})

	go func() {
		defer cleanup()
		t.waitBackground(id, command, cmd, jobCtx, cancel, &stdout, &stderr, time.Now(), timeout)
	}()

	return fmt.Sprintf("background job %s started\n$ %s\nThe result will arrive as a later message. Do not poll.\n", id, command), nil
}

func (t *Bash) waitBackground(id, command string, cmd *exec.Cmd, jobCtx context.Context, cancel context.CancelFunc, stdout, stderr *bytes.Buffer, started time.Time, timeout time.Duration) {
	defer cancel()
	runErr := cmd.Wait()
	elapsed := time.Since(started).Round(time.Millisecond)
	timedOut := jobCtx.Err() == context.DeadlineExceeded

	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if !timedOut {
			exitCode = -1
		}
	}
	body := formatBashOutput(command, stdout.String(), stderr.String(), exitCode, elapsed)
	if timedOut {
		body = fmt.Sprintf("timed out after %s\n%s", timeout, body)
	}
	text := fmt.Sprintf("Background command %s finished.\n%s", id, body)

	t.mu.Lock()
	running := len(t.jobs) - 1
	if running < 0 {
		running = 0
	}
	stopped := t.stopped
	t.mu.Unlock()
	if !stopped {
		// Emit while the job is still counted so a waiter that treats
		// Running()==0 as idle cannot miss this notice.
		t.emit(Notice{ID: id, Running: running, Output: text, Done: true})
	}
	t.mu.Lock()
	delete(t.jobs, id)
	t.mu.Unlock()
}

func (t *Bash) emit(n Notice) {
	t.mu.Lock()
	fn := t.onDone
	stopped := t.stopped
	t.mu.Unlock()
	if stopped || fn == nil {
		return
	}
	fn(n)
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
