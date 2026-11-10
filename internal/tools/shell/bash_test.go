package shell

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBash_Echo(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	dir := t.TempDir()
	b := Bash{WorkDir: dir}
	raw, _ := json.Marshal(map[string]string{"command": "echo hello"})
	out, err := b.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "exit_code: 0") {
		t.Fatalf("out: %s", out)
	}
}

func TestBash_WorkingDirectoryRelative(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	b := Bash{WorkDir: root}
	raw, _ := json.Marshal(map[string]string{
		"command":           "pwd",
		"working_directory": "sub",
	})
	out, err := b.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, sub) {
		t.Fatalf("expected pwd under sub, got: %s", out)
	}
}

func TestBash_Timeout(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	b := Bash{WorkDir: t.TempDir()}
	raw, _ := json.Marshal(map[string]any{
		"command":         "sleep 10",
		"timeout_seconds": 1,
	})
	_, err := b.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout, got %v", err)
	}
}

func TestBash_BackgroundReturnsBeforeExit(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	b := Bash{WorkDir: t.TempDir(), AllowBackground: true}
	notes := make(chan Notice, 4)
	b.SetOnDone(func(n Notice) { notes <- n })
	defer b.StopAll()

	raw, _ := json.Marshal(map[string]any{
		"command":    "sleep 2 && echo ok",
		"background": true,
	})
	start := time.Now()
	out, err := b.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("background execute blocked for %s", time.Since(start))
	}
	if !strings.Contains(out, "background job b1 started") || !strings.Contains(out, "Do not poll") {
		t.Fatalf("out: %s", out)
	}

	var finished Notice
	deadline := time.After(5 * time.Second)
	for !finished.Done {
		select {
		case n := <-notes:
			finished = n
		case <-deadline:
			t.Fatal("timed out waiting for background command")
		}
	}
	if finished.ID != "b1" || !strings.Contains(finished.Output, "exit_code: 0") || !strings.Contains(finished.Output, "ok") {
		t.Fatalf("notice: %+v", finished)
	}
	if b.Running() != 0 {
		t.Fatalf("running = %d", b.Running())
	}
}

func TestBash_BackgroundTimeout(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	b := Bash{WorkDir: t.TempDir(), AllowBackground: true}
	notes := make(chan Notice, 4)
	b.SetOnDone(func(n Notice) { notes <- n })
	defer b.StopAll()

	raw, _ := json.Marshal(map[string]any{
		"command":         "sleep 30",
		"timeout_seconds": 1,
		"background":      true,
	})
	if _, err := b.Execute(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case n := <-notes:
			if !n.Done {
				continue
			}
			if !strings.Contains(n.Output, "timed out") {
				t.Fatalf("notice: %+v", n)
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for background timeout")
		}
	}
}

func TestBash_BackgroundRejectedWhenDisabled(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	b := Bash{WorkDir: dir}
	raw, _ := json.Marshal(map[string]any{
		"command":    "touch " + marker,
		"background": true,
	})
	_, err := b.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "local session") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("command ran despite background being disabled")
	}
}

func TestBash_ExecuteCancelDoesNotKillBackground(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	b := Bash{WorkDir: t.TempDir(), AllowBackground: true}
	defer b.StopAll()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, _ := json.Marshal(map[string]any{
		"command":    "sleep 30",
		"background": true,
	})
	if _, err := b.Execute(ctx, raw); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if b.Running() != 1 {
		t.Fatalf("running = %d, context cancel should leave the job alive", b.Running())
	}
	b.StopAll()
	deadline := time.Now().Add(2 * time.Second)
	for b.Running() != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if b.Running() != 0 {
		t.Fatalf("StopAll left %d jobs", b.Running())
	}
}

func TestBash_BackgroundNoticeBeforeJobDrops(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	b := Bash{WorkDir: t.TempDir(), AllowBackground: true}
	defer b.StopAll()
	sawRunning := make(chan int, 1)
	b.SetOnDone(func(n Notice) {
		if !n.Done {
			return
		}
		sawRunning <- b.Running()
	})
	raw, _ := json.Marshal(map[string]any{
		"command":    "true",
		"background": true,
	})
	if _, err := b.Execute(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-sawRunning:
		if n == 0 {
			t.Fatal("job was removed before the completion notice")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the completion notice")
	}
}

func TestBash_EmptyCommand(t *testing.T) {
	b := Bash{WorkDir: t.TempDir()}
	_, err := b.Execute(context.Background(), []byte(`{"command":""}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBash_LargeOutputTruncation(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	b := Bash{WorkDir: t.TempDir()}
	// Generate 100,000 characters of 'a'
	raw, _ := json.Marshal(map[string]any{
		"command": `printf 'a%.0s' {1..100000}`,
	})
	out, err := b.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	// The internal toolutil.Truncate caps at 200k.
	if len(out) > 210000 {
		t.Fatalf("expected truncated output, got length %d", len(out))
	}
	if !strings.Contains(out, "exit_code: 0") {
		t.Fatal("expected exit code 0")
	}
}

func TestBash_NonZeroExit(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	b := Bash{WorkDir: t.TempDir()}
	raw, _ := json.Marshal(map[string]any{
		"command": `echo "failing"; exit 42`,
	})
	out, err := b.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "failing") {
		t.Fatalf("expected stdout 'failing', got: %s", out)
	}
	if !strings.Contains(out, "exit_code: 42") {
		t.Fatalf("expected exit_code 42, got: %s", out)
	}
}
