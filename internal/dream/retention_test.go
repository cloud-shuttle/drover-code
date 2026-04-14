package dream

import (
	"path/filepath"
	"testing"
	"time"
)

func TestJSONStore_PruneMaxEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.json")
	s, err := NewJSONStore(path)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if err := s.Save(Entry{
			ID:        string(rune('a' + i)),
			Timestamp: base.Add(time.Duration(i) * time.Hour),
			Content:   "c",
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
	if all[0].ID != "d" || all[1].ID != "e" {
		t.Fatalf("kept oldest-first order: %#v", all)
	}
}

func TestJSONStore_PruneAge(t *testing.T) {
	dir := t.TempDir()
	s, err := NewJSONStore(filepath.Join(dir, "m.json"))
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
