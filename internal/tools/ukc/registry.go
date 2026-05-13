// Package ukc implements Unikraft Cloud instance tools for drover-code.
package ukc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

// Entry is one instance in the on-disk registry.
type Entry struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

// FilePath returns ~/.drover-code/ukc-instances.json
func FilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".drover-code", "ukc-instances.json"), nil
}

func loadRegistry(path string) (map[string]Entry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return make(map[string]Entry), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read registry: %w", err)
	}
	var raw map[string]Entry
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	if raw == nil {
		raw = make(map[string]Entry)
	}
	return raw, nil
}

func saveRegistry(path string, entries map[string]Entry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir registry dir: %w", err)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}
	data = append(data, '\n')
	if err := toolutil.WriteAtomic(path, data, 0o600); err != nil {
		return fmt.Errorf("write registry: %w", err)
	}
	return nil
}
