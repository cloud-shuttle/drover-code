// Package session stores a drover-code transcript on disk so a later process
// can continue it. Files live under ~/.drover/sessions/<workdir-hash>/<id>.json.
package session

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudshuttle/drover-code/internal/api"
)

// Transcript is one saved conversation. The system prompt is rebuilt on launch
// and is not stored here.
type Transcript struct {
	ID       string
	WorkDir  string
	Updated  time.Time
	Messages []api.Message
}

// Summary is a row in the session list.
type Summary struct {
	ID      string
	Updated time.Time
	Preview string
}

// Store reads and writes transcripts under Root.
type Store struct {
	Root string
}

// DefaultRoot is ~/.drover/sessions. It is empty when the home directory
// cannot be resolved.
func DefaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".drover", "sessions")
}

// Open returns a store rooted at root.
func Open(root string) *Store {
	return &Store{Root: root}
}

// NewID returns a new session id.
func NewID() string {
	return fmt.Sprintf("session-%x", time.Now().UnixNano())
}

// Save atomically replaces the transcript file for tr.ID in tr.WorkDir.
func (s *Store) Save(tr Transcript) error {
	if s == nil || s.Root == "" {
		return fmt.Errorf("session directory is not set")
	}
	id, err := cleanID(tr.ID)
	if err != nil {
		return err
	}
	abs, err := absPath(tr.WorkDir)
	if err != nil {
		return err
	}
	dir := s.bucketDir(abs)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	updated := tr.Updated.UTC()
	if tr.Updated.IsZero() {
		updated = time.Now().UTC()
	}
	payload, err := encodeTranscript(id, abs, updated, tr.Messages)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(dir, id+".json")); err != nil {
		return err
	}
	cleanup = false
	return nil
}

// Load reads one session. idOrPrefix may be a full id or a unique prefix.
func (s *Store) Load(workDir, idOrPrefix string) (Transcript, error) {
	idOrPrefix = strings.TrimSuffix(strings.TrimSpace(idOrPrefix), ".json")
	if !validToken(idOrPrefix) {
		return Transcript{}, fmt.Errorf("no session %q in this directory", idOrPrefix)
	}
	dir, err := s.dirFor(workDir)
	if err != nil {
		return Transcript{}, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return Transcript{}, fmt.Errorf("no session %q in this directory", idOrPrefix)
		}
		return Transcript{}, err
	}
	var prefixes []string
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".json")
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || !validToken(name) {
			continue
		}
		if name == idOrPrefix {
			return readTranscript(filepath.Join(dir, e.Name()))
		}
		if strings.HasPrefix(name, idOrPrefix) {
			prefixes = append(prefixes, e.Name())
		}
	}
	switch len(prefixes) {
	case 0:
		return Transcript{}, fmt.Errorf("no session %q in this directory", idOrPrefix)
	case 1:
		return readTranscript(filepath.Join(dir, prefixes[0]))
	default:
		return Transcript{}, fmt.Errorf("session id %q matches more than one session; pass a longer id", idOrPrefix)
	}
}

// LoadLatest returns the newest non-empty transcript for workDir.
func (s *Store) LoadLatest(workDir string) (Transcript, error) {
	rows, err := s.readAll(workDir)
	if err != nil {
		return Transcript{}, err
	}
	for _, tr := range rows {
		if len(tr.Messages) > 0 {
			return tr, nil
		}
	}
	return Transcript{}, fmt.Errorf("no saved session in this directory")
}

// List returns up to limit recent non-empty sessions, newest first.
// A missing directory is an empty list.
func (s *Store) List(workDir string, limit int) ([]Summary, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.readAll(workDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Summary
	for _, tr := range rows {
		if len(tr.Messages) == 0 {
			continue
		}
		out = append(out, Summary{
			ID:      tr.ID,
			Updated: tr.Updated,
			Preview: preview(tr.Messages),
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *Store) dirFor(workDir string) (string, error) {
	if s == nil || s.Root == "" {
		return "", fmt.Errorf("session directory is not set")
	}
	abs, err := absPath(workDir)
	if err != nil {
		return "", err
	}
	return s.bucketDir(abs), nil
}

func (s *Store) bucketDir(abs string) string {
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(s.Root, hex.EncodeToString(sum[:8]))
}

func (s *Store) readAll(workDir string) ([]Transcript, error) {
	dir, err := s.dirFor(workDir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var rows []Transcript
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".json")
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || !validToken(name) {
			continue
		}
		tr, err := readTranscript(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if tr.ID == "" {
			tr.ID = name
		}
		rows = append(rows, tr)
	}
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].Updated.Equal(rows[j].Updated) {
			return rows[i].Updated.After(rows[j].Updated)
		}
		return rows[i].ID < rows[j].ID
	})
	return rows, nil
}

func absPath(workDir string) (string, error) {
	if strings.TrimSpace(workDir) == "" {
		return "", fmt.Errorf("workdir is empty")
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func cleanID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if !validToken(id) {
		return "", fmt.Errorf("invalid session id %q", id)
	}
	return id, nil
}

func validToken(s string) bool {
	if s == "" || len(s) > 80 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

func preview(msgs []api.Message) string {
	for _, msg := range msgs {
		if msg.Role != api.RoleUser {
			continue
		}
		for _, block := range msg.Content {
			text, ok := block.(api.TextBlock)
			if !ok {
				continue
			}
			s := strings.Join(strings.Fields(text.Text), " ")
			if s == "" {
				continue
			}
			return clipRunes(s, 72)
		}
	}
	return ""
}

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

// CloseDangling appends an error tool result when a saved session ends on an
// assistant tool call. The next model request can then continue.
func CloseDangling(msgs []api.Message) []api.Message {
	if len(msgs) == 0 {
		return msgs
	}
	last := msgs[len(msgs)-1]
	if last.Role != api.RoleAssistant {
		return msgs
	}
	var results []api.ToolResultBlock
	for _, block := range last.Content {
		tu, ok := block.(api.ToolUseBlock)
		if !ok || tu.ID == "" {
			continue
		}
		results = append(results, api.ToolResultBlock{
			ToolUseID: tu.ID,
			Content:   "Error: session ended before this tool finished",
			IsError:   true,
		})
	}
	if len(results) == 0 {
		return msgs
	}
	out := make([]api.Message, len(msgs), len(msgs)+1)
	copy(out, msgs)
	return append(out, api.ToolResultMessage(results))
}
