//go:build darwin || linux

package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func sandboxHelper() string {
	if runtime.GOOS == "darwin" {
		return "sandbox-exec"
	}
	return "bwrap"
}

func requireSandbox(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	if _, err := exec.LookPath(sandboxHelper()); err != nil {
		t.Skip(sandboxHelper() + " not on PATH")
	}
}

func TestBash_SandboxWriteInsideAndOutside(t *testing.T) {
	requireSandbox(t)
	root := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "pwned.txt")
	b := Bash{WorkDir: root, Sandbox: true}

	raw, _ := json.Marshal(map[string]string{"command": "echo hello > inside.txt"})
	out, err := b.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "exit_code: 0") {
		t.Fatalf("inside write: %s", out)
	}
	body, err := os.ReadFile(filepath.Join(root, "inside.txt"))
	if err != nil || strings.TrimSpace(string(body)) != "hello" {
		t.Fatalf("inside file: %q %v", body, err)
	}

	raw, _ = json.Marshal(map[string]string{
		"command": fmt.Sprintf("echo pwned > '%s'", outsideFile),
	})
	out, err = b.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(outsideFile); !os.IsNotExist(statErr) {
		t.Fatalf("outside write landed: %v\n%s", statErr, out)
	}
	if !strings.Contains(out, "Operation not permitted") && !strings.Contains(out, "Read-only file system") && !strings.Contains(out, "Permission denied") {
		t.Fatalf("outside write output: %s", out)
	}
}

func TestBash_SandboxSymlinkEscape(t *testing.T) {
	requireSandbox(t)
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	b := Bash{WorkDir: root, Sandbox: true}

	raw, _ := json.Marshal(map[string]string{"command": "echo pwned > link/via-link.txt"})
	out, err := b.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "via-link.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("symlink write landed: %v\n%s", statErr, out)
	}

	raw, _ = json.Marshal(map[string]string{
		"command":           "echo pwned > escaped.txt",
		"working_directory": "link",
	})
	_, err = b.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, "escaped.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("escaped write landed: %v", statErr)
	}
}

func TestBash_SandboxNetwork(t *testing.T) {
	requireSandbox(t)
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
	b := Bash{WorkDir: t.TempDir(), Sandbox: true}
	raw, _ := json.Marshal(map[string]string{
		"command": `python3 -c 'import socket; s=socket.socket(); s.settimeout(2); s.connect(("1.1.1.1", 80))'`,
	})
	out, err := b.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "exit_code: 0") {
		t.Fatalf("network succeeded:\n%s", out)
	}
	if !strings.Contains(out, "Operation not permitted") && !strings.Contains(out, "Permission denied") {
		t.Fatalf("network output: %s", out)
	}
}

func TestBash_SandboxReadsAndGoCache(t *testing.T) {
	requireSandbox(t)
	b := Bash{WorkDir: t.TempDir(), Sandbox: true}
	raw, _ := json.Marshal(map[string]string{"command": "head -c 1 /etc/passwd && echo"})
	out, err := b.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "exit_code: 0") {
		t.Fatalf("read: %s", out)
	}

	if _, err := exec.LookPath("go"); err != nil {
		return
	}
	raw, _ = json.Marshal(map[string]string{
		"command": `touch "$(go env GOCACHE)/probe" && echo cache-ok`,
	})
	out, err = b.Execute(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "cache-ok") || !strings.Contains(out, "exit_code: 0") {
		t.Fatalf("gocache: %s", out)
	}
}

func TestBash_SandboxTimeoutKillsChild(t *testing.T) {
	requireSandbox(t)
	root := t.TempDir()
	pidFile := filepath.Join(root, "child.pid")
	b := Bash{WorkDir: root, Sandbox: true}
	raw, _ := json.Marshal(map[string]any{
		"command":         fmt.Sprintf("sleep 30 & echo $! > '%s'; wait", pidFile),
		"timeout_seconds": 2,
	})
	start := time.Now()
	_, err := b.Execute(context.Background(), raw)
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if elapsed > 8*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
	pidText, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("pid file: %v", readErr)
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(pidText)))
	if convErr != nil {
		t.Fatalf("pid %q: %v", pidText, convErr)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("child %d still alive after timeout", pid)
}

func TestBash_SandboxBackgroundWriteOutside(t *testing.T) {
	requireSandbox(t)
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "pwned.txt")
	b := Bash{WorkDir: root, Sandbox: true, AllowBackground: true}
	notes := make(chan Notice, 4)
	b.SetOnDone(func(n Notice) { notes <- n })
	defer b.StopAll()

	raw, _ := json.Marshal(map[string]any{
		"command":    fmt.Sprintf("echo pwned > '%s'", outside),
		"background": true,
	})
	if _, err := b.Execute(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(8 * time.Second)
	for {
		select {
		case n := <-notes:
			if !n.Done {
				continue
			}
			if _, statErr := os.Stat(outside); !os.IsNotExist(statErr) {
				t.Fatalf("background write landed: %v\n%s", statErr, n.Output)
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for background command")
		}
	}
}

func TestBash_SandboxRequiresWorkDir(t *testing.T) {
	requireSandbox(t)
	marker := filepath.Join(t.TempDir(), "ran")
	b := Bash{Sandbox: true}
	raw, _ := json.Marshal(map[string]string{
		"command": fmt.Sprintf("echo ran > '%s'", marker),
	})
	_, err := b.Execute(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "working directory") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("command ran without a working directory")
	}
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}
