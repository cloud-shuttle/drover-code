package dream

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudshuttle/drover-code/internal/api"
)

func TestJSONStore_SaveRecentAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mem.json")

	s, err := NewJSONStore(path)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := s.Save(Entry{ID: "1", Timestamp: old, Content: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(Entry{ID: "2", Timestamp: time.Now(), Content: "second"}); err != nil {
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

	// Reload from disk
	s2, err := NewJSONStore(path)
	if err != nil {
		t.Fatal(err)
	}
	all2, _ := s2.All()
	if len(all2) != 2 {
		t.Fatalf("reload len=%d", len(all2))
	}
}

func TestBuildInjection_respectsSizeCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mem.json")
	s, err := NewJSONStore(path)
	if err != nil {
		t.Fatal(err)
	}
	huge := strings.Repeat("w", maxDreamEntryRunes+500)
	for i := 0; i < 8; i++ {
		if err := s.Save(Entry{
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			Content:   huge,
		}); err != nil {
			t.Fatal(err)
		}
	}
	inj := BuildInjection(s, 8)
	if inj == "" {
		t.Fatal("empty injection")
	}
	if len(inj) > maxDreamInjectionBytes+500 {
		t.Fatalf("injection too large: %d", len(inj))
	}
}

func TestBuildInjection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mem.json")
	s, err := NewJSONStore(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Save(Entry{Timestamp: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), Content: "remember this"})
	inj := BuildInjection(s, 5)
	if inj == "" || !strings.Contains(inj, "remember this") {
		t.Fatalf("injection: %q", inj)
	}
	if BuildInjection(nil, 3) != "" {
		t.Fatal("nil store")
	}
}

func TestExtractTags(t *testing.T) {
	tags := extractTags("touched cmd/foo/main.go and README.md for /api/v1")
	if len(tags) < 2 {
		t.Fatalf("tags: %v", tags)
	}
}

func TestWorker_Consolidate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: content_block_start\n")
		io.WriteString(w, `data: {"index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
		io.WriteString(w, "event: content_block_delta\n")
		io.WriteString(w, `data: {"index":0,"delta":{"type":"text_delta","text":"- bullet memory"}}`+"\n\n")
		io.WriteString(w, "event: content_block_stop\n")
		io.WriteString(w, `data: {"index":0}`+"\n\n")
		io.WriteString(w, "event: message_delta\n")
		io.WriteString(w, `data: {"delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":1,"output_tokens":1}}`+"\n\n")
		io.WriteString(w, "event: message_stop\n")
		io.WriteString(w, "data: {}\n\n")
	}))
	defer srv.Close()

	client := api.NewClient("k", "m")
	client.SetBaseURL(srv.URL)

	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	store, err := NewJSONStore(path)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorker(store, client, Retention{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)

	w.Trigger(Session{
		ID: "sess-1",
		Messages: []api.Message{
			api.UserMessage("hello"),
			api.AssistantMessage([]api.ContentBlock{api.TextBlock{Text: "hi there"}}),
		},
	})

	deadline := time.After(3 * time.Second)
	for {
		all, _ := store.All()
		if len(all) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for dream entry")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	w.Wait()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "bullet memory") {
		t.Fatalf("file: %s", data)
	}
}

func TestWorker_Consolidate_Errors(t *testing.T) {
	// Test empty messages (should return early)
	w := NewWorker(nil, nil, Retention{})
	w.consolidate(context.Background(), Session{ID: "empty"})

	// Test API error
	errSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", 500)
	}))
	defer errSrv.Close()

	client := api.NewClient("k", "m")
	client.SetBaseURL(errSrv.URL)

	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	store, _ := NewJSONStore(path)
	w = NewWorker(store, client, Retention{})

	// This should fail gracefully and not panic
	w.consolidate(context.Background(), Session{
		ID: "err-session",
		Messages: []api.Message{
			api.UserMessage("hello"),
		},
	})
	
	all, _ := store.All()
	if len(all) != 0 {
		t.Fatalf("expected no entries due to API error, got %d", len(all))
	}

	// Test empty response string from API
	emptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "event: message_stop\ndata: {}\n\n")
	}))
	defer emptySrv.Close()

	client2 := api.NewClient("k", "m")
	client2.SetBaseURL(emptySrv.URL)
	w2 := NewWorker(store, client2, Retention{})

	w2.consolidate(context.Background(), Session{
		ID: "empty-res-session",
		Messages: []api.Message{
			api.UserMessage("hello"),
		},
	})

	all, _ = store.All()
	if len(all) != 0 {
		t.Fatalf("expected no entries due to empty response, got %d", len(all))
	}
}
