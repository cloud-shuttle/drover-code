package ukc

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRegistry_roundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ukc-instances.json")

	entries := map[string]Entry{
		"u1": {
			ID:        "u1",
			Name:      "a",
			URL:       "https://a.fra0.unikraft.app",
			Token:     "tok",
			CreatedAt: time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC),
		},
	}
	if err := saveRegistry(path, entries); err != nil {
		t.Fatal(err)
	}
	got, err := loadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if got["u1"].Name != "a" || got["u1"].Token != "tok" {
		t.Fatalf("got %+v", got["u1"])
	}
}
