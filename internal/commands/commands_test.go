package commands

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCommands(t *testing.T) {
	workDir := t.TempDir()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := Init(workDir)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	io.Copy(&buf, r)

	output := buf.String()
	if !strings.Contains(output, "Successfully created") {
		t.Errorf("expected success message, got: %s", output)
	}

	// Verify files were created
	cmdDir := filepath.Join(workDir, ".drover", "commands")
	files, err := os.ReadDir(cmdDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	if len(files) != len(starterCommands) {
		t.Errorf("expected %d commands created, got %d", len(starterCommands), len(files))
	}

	// Test idempotency
	oldStdout2 := os.Stdout
	r2, w2, _ := os.Pipe()
	os.Stdout = w2

	err = Init(workDir)
	if err != nil {
		t.Fatalf("Init idempotent failed: %v", err)
	}

	w2.Close()
	os.Stdout = oldStdout2
	var buf2 bytes.Buffer
	io.Copy(&buf2, r2)
	
	output2 := buf2.String()
	if !strings.Contains(output2, "All default commands already exist.") {
		t.Errorf("expected already exists message, got: %s", output2)
	}
}

func TestListCommands(t *testing.T) {
	registry := NewRegistry()
	registry.Register(CommandDefinition{
		Name:        "testcmd",
		Description: "A test command",
		Agent:       "executor",
		RiskTier:    2,
		Subtask:     true,
	})

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	List(registry)

	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	io.Copy(&buf, r)

	output := buf.String()
	if !strings.Contains(output, "/testcmd") {
		t.Errorf("expected command name in output, got: %s", output)
	}
	if !strings.Contains(output, "A test command") {
		t.Errorf("expected command description in output, got: %s", output)
	}
	if !strings.Contains(output, "agent:executor") || !strings.Contains(output, "risk:2") || !strings.Contains(output, "subtask") {
		t.Errorf("expected extra fields in output, got: %s", output)
	}
}

func TestListCommands_Empty(t *testing.T) {
	registry := NewRegistry()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	List(registry)

	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	io.Copy(&buf, r)

	output := buf.String()
	if !strings.Contains(output, "No custom commands found") {
		t.Errorf("expected no commands message, got: %s", output)
	}
}
