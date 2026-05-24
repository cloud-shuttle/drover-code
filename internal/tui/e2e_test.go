package tui

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestE2E_StartupAndExit(t *testing.T) {
	t.Skip("skipping e2e test in headless CI due to PTY ANSI blocking")

	// 1. Build the binary
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "drover-code")

	cmdBuild := exec.Command("go", "build", "-o", binPath, "../../cmd/drover-code")
	if out, err := cmdBuild.CombinedOutput(); err != nil {
		t.Fatalf("failed to build binary: %v\n%s", err, out)
	}

	// 2. Setup the test command
	cmd := exec.Command(binPath)
	cmd.Dir = tmpDir
	// Required to bypass "API key not found" errors
	cmd.Env = append(os.Environ(), "ANTHROPIC_API_KEY=sk-ant-api03-test-1234")

	// 3. Start inside a PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("failed to start pty: %v", err)
	}
	defer func() { _ = ptmx.Close() }()

	pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80})

	// 4. Read output asynchronously
	outputCh := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, ptmx)
		outputCh <- buf.String()
	}()

	// 5. Wait for the TUI to initialize (e.g. bubbletea to draw the first frame)
	time.Sleep(5 * time.Second)

	// 6. Send the quit command
	// Bubbletea might need some time to process inputs, we send "/quit" + Enter.
	_, err = ptmx.Write([]byte("/quit\r"))
	if err != nil {
		t.Fatalf("failed to write to pty: %v", err)
	}

	// 7. Wait for the process to exit cleanly
	waitErr := make(chan error, 1)
	go func() {
		waitErr <- cmd.Wait()
	}()

	select {
	case err := <-waitErr:
		if err != nil {
			t.Errorf("expected clean exit, got error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Errorf("timed out waiting for process to exit after /quit")
		_ = cmd.Process.Kill()
	}

	// 8. Capture and assert final output (optional, mostly for debugging)
	_ = ptmx.Close() // Force io.Copy to return if not already
	select {
	case out := <-outputCh:
		if len(out) == 0 {
			t.Log("Warning: output was empty, but process exited cleanly")
		} else {
			// /quit might appear if echoed by the TUI
			t.Logf("TUI output captured (%d bytes)", len(out))
			if !strings.Contains(out, "/quit") {
				t.Logf("Did not see '/quit' echo in output, output snippet: %q", out[:min(100, len(out))])
			}
		}
	case <-time.After(1 * time.Second):
		t.Log("timed out waiting for output channel to close")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestE2E_TypingInteraction(t *testing.T) {
	t.Skip("skipping e2e test in headless CI due to PTY ANSI blocking")

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "drover-code")

	cmdBuild := exec.Command("go", "build", "-o", binPath, "../../cmd/drover-code")
	if out, err := cmdBuild.CombinedOutput(); err != nil {
		t.Fatalf("failed to build binary: %v\n%s", err, out)
	}

	cmd := exec.Command(binPath)
	cmd.Dir = tmpDir
	cmd.Env = append(os.Environ(), "ANTHROPIC_API_KEY=sk-ant-api03-test-1234")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("failed to start pty: %v", err)
	}
	defer func() { _ = ptmx.Close() }()

	pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 80})

	outputCh := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, ptmx)
		outputCh <- buf.String()
	}()

	time.Sleep(3 * time.Second)

	// Simulate typing "hello pty"
	_, err = ptmx.Write([]byte("hello pty"))
	if err != nil {
		t.Fatalf("failed to write to pty: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Then send quit to exit
	_, err = ptmx.Write([]byte("\x03")) // Ctrl+C to force quit
	if err != nil {
		t.Fatalf("failed to write to pty: %v", err)
	}

	waitErr := make(chan error, 1)
	go func() {
		waitErr <- cmd.Wait()
	}()

	select {
	case <-waitErr:
	case <-time.After(5 * time.Second):
		t.Errorf("timed out waiting for process to exit")
		_ = cmd.Process.Kill()
	}

	_ = ptmx.Close()
	select {
	case out := <-outputCh:
		// Strip ANSI escape codes to make it easier to match the typed text.
		ansiRe := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
		cleanOut := ansiRe.ReplaceAllString(out, "")
		
		if !strings.Contains(cleanOut, "hello pty") {
			t.Errorf("Expected clean output to contain typed text 'hello pty', got snippet: %q", cleanOut[:min(200, len(cleanOut))])
		}
	case <-time.After(1 * time.Second):
		t.Log("timed out waiting for output channel to close")
	}
}
