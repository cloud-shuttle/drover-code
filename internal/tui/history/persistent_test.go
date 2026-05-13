package history

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersistentHistory_AddAndGet(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "history-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hist, err := NewPersistentHistory(tmpDir)
	if err != nil {
		t.Fatalf("failed to create history: %v", err)
	}
	
	// Default home directory resolution might still put it in ~/.drover,
	// so we override filePath for testing to ensure isolation.
	hist.filePath = filepath.Join(tmpDir, "history.json")
	hist.entries = []string{}

	// Test basic adding
	hist.Add("hello")
	hist.Add("world")

	entries := hist.Get()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0] != "hello" || entries[1] != "world" {
		t.Errorf("expected ['hello', 'world'], got %v", entries)
	}

	// Test global deduplication (HIST_IGNORE_ALL_DUPS behavior)
	hist.Add("hello")

	entries = hist.Get()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries after deduplication, got %d", len(entries))
	}
	// 'hello' should have been moved to the end
	if entries[0] != "world" || entries[1] != "hello" {
		t.Errorf("expected ['world', 'hello'], got %v", entries)
	}

	// Test empty string is ignored
	hist.Add("")
	entries = hist.Get()
	if len(entries) != 2 {
		t.Errorf("empty string should be ignored, got length %d", len(entries))
	}
}

func TestPersistentHistory_Persistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "history-test-persist-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	filePath := filepath.Join(tmpDir, "history.json")

	// Create first instance
	hist1 := &PersistentHistory{
		entries:  []string{},
		filePath: filePath,
	}

	hist1.Add("cmd1")
	hist1.Add("cmd2")

	// Create second instance and load
	hist2 := &PersistentHistory{
		entries:  []string{},
		filePath: filePath,
	}
	if err := hist2.load(); err != nil {
		t.Fatalf("failed to load history: %v", err)
	}

	entries := hist2.Get()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries from persistence, got %d", len(entries))
	}
	if entries[0] != "cmd1" || entries[1] != "cmd2" {
		t.Errorf("expected ['cmd1', 'cmd2'], got %v", entries)
	}
}
