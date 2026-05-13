// internal/tui/history/persistent.go
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	maxHistoryEntries = 500
	historyFileName   = "history.json"
)

type PersistentHistory struct {
	entries  []string
	filePath string
}

func NewPersistentHistory(workDir string) (*PersistentHistory, error) {
	var historyDir string
	if envDir := os.Getenv("DROVER_HISTORY_DIR"); envDir != "" {
		historyDir = envDir
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			home = workDir // fallback
		}
		historyDir = filepath.Join(home, ".drover")
	}

	if err := os.MkdirAll(historyDir, 0755); err != nil {
		return nil, err
	}

	filePath := filepath.Join(historyDir, historyFileName)

	h := &PersistentHistory{
		entries:  make([]string, 0, maxHistoryEntries),
		filePath: filePath,
	}

	if err := h.load(); err != nil {
		// Non-fatal: continue with empty history
		fmt.Printf("Warning: could not load history: %v\n", err)
	}

	return h, nil
}

// Add adds a message to history (globally deduplicates entries, keeping the most recent)
func (h *PersistentHistory) Add(message string) {
	if message == "" {
		return
	}

	// Remove any existing occurrences of this message to act like HIST_IGNORE_ALL_DUPS
	filtered := make([]string, 0, len(h.entries))
	for _, entry := range h.entries {
		if entry != message {
			filtered = append(filtered, entry)
		}
	}
	
	filtered = append(filtered, message)
	h.entries = filtered

	// Keep only the last N entries
	if len(h.entries) > maxHistoryEntries {
		h.entries = h.entries[len(h.entries)-maxHistoryEntries:]
	}

	_ = h.save() // best effort
}

// Get returns history entries in chronological order (oldest first)
// Wait, the user's snippet said "reverse chronological order (newest first)" and reversed it, but in the TUI navigation Up goes to historyIndex--, which assumes oldest first (if you append to the end) or newest first if you start at index 0. 
// Our TUI Model.Update navigation used `inputHistory` natively where index len(inputHistory)-1 is the newest.
// Let's just return the raw entries slice, where the end is the newest.
func (h *PersistentHistory) Get() []string {
	return h.entries
}

// load reads history from disk
func (h *PersistentHistory) load() error {
	data, err := os.ReadFile(h.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // fresh install is fine
		}
		return err
	}

	var rawEntries []string
	if err := json.Unmarshal(data, &rawEntries); err != nil {
		return err
	}

	// Globally deduplicate loaded entries, keeping the latest occurrence
	seen := make(map[string]bool)
	var deduplicated []string
	// Traverse backwards to keep the most recent
	for i := len(rawEntries) - 1; i >= 0; i-- {
		entry := rawEntries[i]
		if !seen[entry] && entry != "" {
			seen[entry] = true
			deduplicated = append(deduplicated, entry)
		}
	}

	// Reverse it back to chronological order
	for i, j := 0, len(deduplicated)-1; i < j; i, j = i+1, j-1 {
		deduplicated[i], deduplicated[j] = deduplicated[j], deduplicated[i]
	}

	h.entries = deduplicated

	// Save the cleaned-up history back to disk if we found and removed duplicates
	if len(deduplicated) < len(rawEntries) {
		_ = h.save()
	}

	return nil
}

// save writes history to disk
func (h *PersistentHistory) save() error {
	data, err := json.MarshalIndent(h.entries, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(h.filePath, data, 0644)
}
