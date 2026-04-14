// Package dream implements background memory consolidation.
package dream

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cloudshuttle/drover-code/internal/api"
)

const (
	maxDreamConsolidationRunes = 200_000
	maxDreamInjectionBytes     = 24_000
	maxDreamEntryRunes         = 6_000
)

type Entry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Tags      []string  `json:"tags"`
	Content   string    `json:"content"`
	SessionID string    `json:"session_id"`
}

type Store interface {
	Save(e Entry) error
	Recent(n int) ([]Entry, error)
	All() ([]Entry, error)
	Prune(r Retention) error
}

type jsonStore struct {
	mu      sync.Mutex
	path    string
	entries []Entry
}

func NewJSONStore(path string) (Store, error) {
	s := &jsonStore{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *jsonStore) load() error {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("dream: load: %w", err)
	}
	return json.Unmarshal(data, &s.entries)
}

func (s *jsonStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *jsonStore) Save(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
	return s.save()
}

func (s *jsonStore) Recent(n int) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sorted := make([]Entry, len(s.entries))
	copy(sorted, s.entries)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.After(sorted[j].Timestamp)
	})
	if n > len(sorted) {
		n = len(sorted)
	}
	return sorted[:n], nil
}

func (s *jsonStore) All() ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Entry, len(s.entries))
	copy(out, s.entries)
	return out, nil
}

func (s *jsonStore) Prune(r Retention) error {
	if !r.Active() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var kept []Entry
	if cutoff, ok := r.minTimestamp(); ok {
		for _, e := range s.entries {
			if !e.Timestamp.Before(cutoff) {
				kept = append(kept, e)
			}
		}
	} else {
		kept = append(kept, s.entries...)
	}
	sort.Slice(kept, func(i, j int) bool {
		return kept[i].Timestamp.Before(kept[j].Timestamp)
	})
	if r.MaxEntries > 0 && len(kept) > r.MaxEntries {
		kept = kept[len(kept)-r.MaxEntries:]
	}
	s.entries = kept
	return s.save()
}

type Session struct {
	ID       string
	Messages []api.Message
}

type Worker struct {
	store     Store
	client    summariser
	retention Retention
	triggerCh chan Session
	wg        sync.WaitGroup
}

type summariser interface {
	StreamMessage(ctx context.Context, req api.StreamRequest) (*api.Stream, error)
}

func NewWorker(store Store, client summariser, retention Retention) *Worker {
	return &Worker{
		store:     store,
		client:    client,
		retention: retention,
		triggerCh: make(chan Session, 8),
	}
}

func (w *Worker) Trigger(s Session) {
	select {
	case w.triggerCh <- s:
	default:
	}
}

func (w *Worker) Start(ctx context.Context) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for {
			select {
			case s := <-w.triggerCh:
				w.consolidate(ctx, s)
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (w *Worker) Wait() { w.wg.Wait() }

func (w *Worker) consolidate(ctx context.Context, s Session) {
	if len(s.Messages) == 0 {
		return
	}

	var conv strings.Builder
	for _, m := range s.Messages {
		role := "User"
		if m.Role == api.RoleAssistant {
			role = "Assistant"
		}
		for _, b := range m.Content {
			if tb, ok := b.(api.TextBlock); ok {
				fmt.Fprintf(&conv, "%s: %s\n\n", role, tb.Text)
			}
		}
	}

	convStr := conv.String()
	if n := utf8.RuneCountInString(convStr); n > maxDreamConsolidationRunes {
		r := []rune(convStr)
		convStr = "[... truncated for dream consolidation ...]\n\n" + string(r[len(r)-maxDreamConsolidationRunes:])
	}

	summaryPrompt := fmt.Sprintf(
		`Summarise the following conversation into 3-5 bullet points capturing:
- What was worked on (files, features, bugs)
- Key decisions made
- Any important context for future sessions

Be concise. Output only the bullet points, nothing else.

---
%s`, convStr)

	ctx2, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	stream, err := w.client.StreamMessage(ctx2, api.StreamRequest{
		Messages:  []api.Message{api.UserMessage(summaryPrompt)},
		MaxTokens: 512,
	})
	if err != nil {
		return
	}
	defer stream.Close()

	var summary strings.Builder
	for stream.Next() {
		if e, ok := stream.Event().(api.ContentBlockDeltaEvent); ok {
			if td, ok := e.Delta.(api.TextDelta); ok {
				summary.WriteString(td.Text)
			}
		}
	}
	if stream.Err() != nil || summary.Len() == 0 {
		return
	}

	entry := Entry{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Content:   strings.TrimSpace(summary.String()),
		SessionID: s.ID,
		Tags:      extractTags(summary.String()),
	}
	if err := w.store.Save(entry); err != nil {
		return
	}
	if w.retention.Active() {
		_ = w.store.Prune(w.retention)
	}
}

func BuildInjection(store Store, maxEntries int) string {
	if store == nil {
		return ""
	}
	entries, err := store.Recent(maxEntries)
	if err != nil || len(entries) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Memory from previous sessions\n\n")
	for _, e := range entries {
		content := e.Content
		if utf8.RuneCountInString(content) > maxDreamEntryRunes {
			r := []rune(content)
			content = string(r[:maxDreamEntryRunes-1]) + "…"
		}
		block := fmt.Sprintf("**%s**\n%s\n\n",
			e.Timestamp.Format("2006-01-02"),
			content,
		)
		if b.Len()+len(block) > maxDreamInjectionBytes {
			break
		}
		b.WriteString(block)
	}
	return b.String()
}

func extractTags(summary string) []string {
	var tags []string
	seen := map[string]bool{}

	for _, word := range strings.Fields(summary) {
		word = strings.Trim(word, ".,;:\"'`()")
		if strings.Contains(word, ".go") || strings.Contains(word, ".ts") ||
			strings.Contains(word, ".py") || strings.Contains(word, "/") {
			if !seen[word] {
				tags = append(tags, word)
				seen[word] = true
			}
		}
	}
	return tags
}

