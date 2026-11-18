package dream

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// genEntry generates a random valid Entry.
func genEntry() *rapid.Generator[Entry] {
	return rapid.Custom(func(t *rapid.T) Entry {
		// Use a large range of time, but not overflowing SQLite limits
		ts := time.Unix(rapid.Int64Range(0, 253402300799).Draw(t, "ts"), 0).UTC()
		return Entry{
			ID:        rapid.String().Draw(t, "id"),
			Timestamp: ts,
			Tags:      rapid.SliceOf(rapid.String()).Draw(t, "tags"),
			Content:   rapid.String().Draw(t, "content"),
			SessionID: rapid.String().Draw(t, "session_id"),
		}
	})
}

func TestProperty_SQLitePruneLimits(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		maxEntries := rapid.IntRange(0, 100).Draw(rt, "maxEntries")
		maxAgeDays := rapid.IntRange(0, 365).Draw(rt, "maxAgeDays")

		retention := Retention{
			MaxEntries: maxEntries,
			MaxAgeDays: maxAgeDays,
		}

		entries := rapid.SliceOfN(genEntry(), 0, 200).Draw(rt, "entries")

		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "memory.db")

		store, err := NewSQLiteStore(dbPath)
		if err != nil {
			rt.Fatalf("Failed to create store: %v", err)
		}

		// Insert all entries
		for i, e := range entries {
			e.ID = fmt.Sprintf("id-%d", i)
			if err := store.Save(e); err != nil {
				rt.Fatalf("Failed to save entry: %v", err)
			}
		}

		// Prune
		if err := store.Prune(retention); err != nil {
			rt.Fatalf("Failed to prune: %v", err)
		}

		// Load all entries back to check invariants
		after, err := store.All()
		if err != nil {
			rt.Fatalf("Failed to get all entries: %v", err)
		}

		// Invariant 1: MaxEntries constraint
		if retention.MaxEntries > 0 && len(after) > retention.MaxEntries {
			rt.Fatalf("Store has %d entries, but MaxEntries is %d", len(after), retention.MaxEntries)
		}

		// Invariant 2: MaxAgeDays constraint
		cutoff, hasAgeLimit := retention.minTimestamp()
		if hasAgeLimit {
			for _, e := range after {
				if e.Timestamp.Before(cutoff) {
					rt.Fatalf("Entry timestamp %v is older than cutoff %v", e.Timestamp, cutoff)
				}
			}
		}
		
		store.(*sqliteStore).Close()
	})
}

func TestProperty_SQLitePersistenceIntegrity(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		entry := genEntry().Draw(rt, "entry")

		tmpDir := t.TempDir()
		dbPath := filepath.Join(tmpDir, "memory.db")

		store, err := NewSQLiteStore(dbPath)
		if err != nil {
			rt.Fatalf("Failed to create store: %v", err)
		}

		if err := store.Save(entry); err != nil {
			rt.Fatalf("Failed to save entry: %v", err)
		}

		after, err := store.All()
		if err != nil {
			rt.Fatalf("Failed to get all entries: %v", err)
		}

		if len(after) != 1 {
			rt.Fatalf("Expected exactly 1 entry, got %d", len(after))
		}

		loaded := after[0]

		if loaded.ID != entry.ID {
			rt.Fatalf("ID mismatch: got %q, want %q", loaded.ID, entry.ID)
		}
		if loaded.Content != entry.Content {
			rt.Fatalf("Content mismatch")
		}
		if loaded.SessionID != entry.SessionID {
			rt.Fatalf("SessionID mismatch")
		}
		
		if loaded.Timestamp.UnixNano() != entry.Timestamp.UnixNano() {
			rt.Fatalf("Timestamp mismatch: got %v, want %v", loaded.Timestamp, entry.Timestamp)
		}
		
		if len(loaded.Tags) != len(entry.Tags) {
			rt.Fatalf("Tags length mismatch: got %d, want %d", len(loaded.Tags), len(entry.Tags))
		}
		for i, tag := range loaded.Tags {
			if tag != entry.Tags[i] {
				rt.Fatalf("Tag %d mismatch: got %q, want %q", i, tag, entry.Tags[i])
			}
		}

		store.(*sqliteStore).Close()
	})
}
