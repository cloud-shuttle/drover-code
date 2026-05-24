package dream

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStore_SaveRecentAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mem.db")

	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour).UTC()
	if err := s.Save(Entry{ID: "1", Timestamp: old, Content: "first", SessionID: "s1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{ID: "2", Timestamp: time.Now().UTC(), Content: "second", Tags: []string{"a"}}); err != nil {
		t.Fatal(err)
	}

	recent, err := s.Recent(1)
	if err != nil || len(recent) != 1 || recent[0].Content != "second" {
		t.Fatalf("Recent: %v err=%v", recent, err)
	}
	all, err := s.All()
	if err != nil || len(all) != 2 {
		t.Fatalf("All: %v", all)
	}
	if all[0].Content != "first" || all[1].Content != "second" {
		t.Fatalf("order: %#v", all)
	}

	s2, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	all2, _ := s2.All()
	if len(all2) != 2 {
		t.Fatalf("reload len=%d", len(all2))
	}
}

func TestOpenStore_sqliteEnv(t *testing.T) {
	t.Setenv("DROVER_CODE_DREAM_BACKEND", " SQLite ")
	dir := t.TempDir()
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{ID: "x", Timestamp: time.Now().UTC(), Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, ".drover", "memory.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected sqlite file: %v", err)
	}
}

func TestSQLiteStore_PruneMaxEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.db")
	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if err := s.Save(Entry{
			ID:        fmt.Sprintf("id%d", i),
			Timestamp: base.Add(time.Duration(i) * time.Hour),
			Content:   "x",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Prune(Retention{MaxEntries: 2}); err != nil {
		t.Fatal(err)
	}
	all, err := s.All()
	if err != nil || len(all) != 2 {
		t.Fatalf("All: %v n=%d", err, len(all))
	}
	if all[0].ID != "id3" || all[1].ID != "id4" {
		t.Fatalf("expected newest two: %#v", all)
	}
}

func TestSQLiteStore_PruneAge(t *testing.T) {
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{ID: "old", Timestamp: time.Now().Add(-96 * time.Hour), Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{ID: "new", Timestamp: time.Now(), Content: "y"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Prune(Retention{MaxAgeDays: 2}); err != nil {
		t.Fatal(err)
	}
	all, _ := s.All()
	if len(all) != 1 || all[0].ID != "new" {
		t.Fatalf("got %#v", all)
	}
}

func TestOpenStore_migratesJSONWhenSQLiteEmpty(t *testing.T) {
	t.Setenv("DROVER_CODE_DREAM_BACKEND", "sqlite")
	t.Setenv("DROVER_CODE_DREAM_SKIP_JSON_IMPORT", "")
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".drover")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(claudeDir, "memory.json")
	payload := `[{"id":"legacy-1","timestamp":"2020-01-15T12:00:00Z","tags":[],"content":"from json","session_id":"s"}]`
	if err := os.WriteFile(jsonPath, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.All()
	if err != nil || len(all) != 1 {
		t.Fatalf("All: %v %#v", err, all)
	}
	if all[0].Content != "from json" || all[0].ID != "legacy-1" {
		t.Fatalf("entry: %#v", all[0])
	}
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatalf("json should be renamed after import: stat err=%v", err)
	}
	bak := jsonPath + ".imported"
	if _, err := os.Stat(bak); err != nil {
		t.Fatalf("expected backup json: %v", err)
	}
}

func TestOpenStore_skipsImportWhenEnvSet(t *testing.T) {
	t.Setenv("DROVER_CODE_DREAM_BACKEND", "sqlite")
	t.Setenv("DROVER_CODE_DREAM_SKIP_JSON_IMPORT", "1")
	dir := t.TempDir()
	claudeDir := filepath.Join(dir, ".drover")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(claudeDir, "memory.json")
	if err := os.WriteFile(jsonPath, []byte(`[{"id":"x","timestamp":"2020-01-15T12:00:00Z","content":"c"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	all, _ := s.All()
	if len(all) != 0 {
		t.Fatalf("expected no import, got %d rows", len(all))
	}
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("json should remain: %v", err)
	}
	_ = s
}

func TestSQLiteStore_CloseAndErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "close.db")
	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	
	// Close the store
	sq := s.(*sqliteStore)
	if err := sq.Close(); err != nil {
		t.Fatal(err)
	}
	
	// Double close should be fine
	if err := sq.Close(); err != nil {
		t.Fatal(err)
	}
	
	// Operations on closed DB should fail
	if err := sq.Save(Entry{ID: "x"}); err == nil {
		t.Fatal("expected save error on closed db")
	}
	if _, err := sq.Recent(1); err == nil {
		t.Fatal("expected recent error on closed db")
	}
	if _, err := sq.All(); err == nil {
		t.Fatal("expected all error on closed db")
	}
	if err := sq.Prune(Retention{MaxAgeDays: 1}); err == nil {
		t.Fatal("expected prune error on closed db")
	}
	
	// Test invalid path for NewSQLiteStore
	_, err = NewSQLiteStore("/dev/null/invalid/mem.db")
	if err == nil {
		t.Fatal("expected mkdir error for invalid path")
	}
}

func TestScanEntries_InvalidTime(t *testing.T) {
	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "invalid_time.db"))
	if err != nil {
		t.Fatal(err)
	}
	sq := s.(*sqliteStore)
	// manually insert bad time
	_, err = sq.db.Exec(`INSERT INTO dream_entries (id, ts, tags_json, content, session_id) VALUES ('1', 'not-a-time', '[]', 'c', 's')`)
	if err != nil {
		t.Fatal(err)
	}
	
	all, err := sq.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(all))
	}
	// Time should be zero value if parsing failed completely
	if !all[0].Timestamp.IsZero() {
		t.Fatalf("expected zero time, got %v", all[0].Timestamp)
	}
}
