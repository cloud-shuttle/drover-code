package dream

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type sqliteStore struct {
	mu sync.Mutex
	db *sql.DB
}

// NewSQLiteStore opens (or creates) a SQLite-backed dream store at path.
// The parent directory is created if needed. Suitable for large session counts
// vs loading all entries from memory.json.
func NewSQLiteStore(path string) (Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("dream sqlite: mkdir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("dream sqlite: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &sqliteStore{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *sqliteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func (s *sqliteStore) migrate() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS dream_entries (
	id TEXT PRIMARY KEY,
	ts TEXT NOT NULL,
	tags_json TEXT NOT NULL,
	content TEXT NOT NULL,
	session_id TEXT NOT NULL
);`); err != nil {
		return err
	}
	_, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_dream_ts ON dream_entries(ts DESC);`)
	return err
}

func (s *sqliteStore) Save(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errors.New("dream sqlite: store is closed")
	}
	tags, err := json.Marshal(e.Tags)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO dream_entries (id, ts, tags_json, content, session_id) VALUES (?, ?, ?, ?, ?)`,
		e.ID,
		e.Timestamp.UTC().Format(time.RFC3339Nano),
		string(tags),
		e.Content,
		e.SessionID,
	)
	if err != nil {
		return fmt.Errorf("dream sqlite: insert: %w", err)
	}
	return nil
}

func (s *sqliteStore) Recent(n int) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("dream sqlite: store is closed")
	}
	rows, err := s.db.Query(
		`SELECT id, ts, tags_json, content, session_id FROM dream_entries ORDER BY ts DESC LIMIT ?`,
		n,
	)
	if err != nil {
		return nil, fmt.Errorf("dream sqlite: recent: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

func (s *sqliteStore) All() ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, errors.New("dream sqlite: store is closed")
	}
	rows, err := s.db.Query(
		`SELECT id, ts, tags_json, content, session_id FROM dream_entries ORDER BY ts ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("dream sqlite: all: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

func (s *sqliteStore) Prune(r Retention) error {
	if !r.Active() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return errors.New("dream sqlite: store is closed")
	}
	if cutoff, ok := r.minTimestamp(); ok {
		ts := cutoff.UTC().Format(time.RFC3339Nano)
		if _, err := s.db.Exec(`DELETE FROM dream_entries WHERE ts < ?`, ts); err != nil {
			return fmt.Errorf("dream sqlite: prune age: %w", err)
		}
	}
	if r.MaxEntries > 0 {
		var cnt int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM dream_entries`).Scan(&cnt); err != nil {
			return fmt.Errorf("dream sqlite: prune count: %w", err)
		}
		excess := cnt - r.MaxEntries
		if excess > 0 {
			if _, err := s.db.Exec(
				`DELETE FROM dream_entries WHERE id IN (
					SELECT id FROM dream_entries ORDER BY ts ASC LIMIT ?
				)`, excess,
			); err != nil {
				return fmt.Errorf("dream sqlite: prune excess: %w", err)
			}
		}
	}
	return nil
}

// importFromJSONIfEmpty loads memory.json into an empty DB once (e.g. after
// switching DROVER_CODE_DREAM_BACKEND=sqlite). Skipped when
// DROVER_CODE_DREAM_SKIP_JSON_IMPORT=1 or the DB already has rows.
func (s *sqliteStore) importFromJSONIfEmpty(jsonPath string) error {
	if strings.TrimSpace(os.Getenv("DROVER_CODE_DREAM_SKIP_JSON_IMPORT")) == "1" {
		return nil
	}
	var cnt int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM dream_entries`).Scan(&cnt); err != nil {
		return fmt.Errorf("dream sqlite: import count: %w", err)
	}
	if cnt > 0 {
		return nil
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("dream sqlite: read json for import: %w", err)
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil || len(entries) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("dream sqlite: import tx: %w", err)
	}
	stmt, err := tx.Prepare(
		`INSERT INTO dream_entries (id, ts, tags_json, content, session_id) VALUES (?, ?, ?, ?, ?)`,
	)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("dream sqlite: import prepare: %w", err)
	}
	defer stmt.Close()
	for _, e := range entries {
		tags, err := json.Marshal(e.Tags)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		_, err = stmt.Exec(
			e.ID,
			e.Timestamp.UTC().Format(time.RFC3339Nano),
			string(tags),
			e.Content,
			e.SessionID,
		)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("dream sqlite: import insert: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("dream sqlite: import commit: %w", err)
	}
	bak := jsonPath + ".imported"
	if err := os.Rename(jsonPath, bak); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("dream sqlite: rename json after import: %w", err)
	}
	return nil
}

func scanEntries(rows *sql.Rows) ([]Entry, error) {
	var out []Entry
	for rows.Next() {
		var id, tsStr, tagsJSON, content, sessionID string
		if err := rows.Scan(&id, &tsStr, &tagsJSON, &content, &sessionID); err != nil {
			return nil, err
		}
		ts, err := time.Parse(time.RFC3339Nano, tsStr)
		if err != nil {
			ts, _ = time.Parse(time.RFC3339, tsStr)
		}
		var tags []string
		_ = json.Unmarshal([]byte(tagsJSON), &tags)
		out = append(out, Entry{
			ID:        id,
			Timestamp: ts,
			Tags:      tags,
			Content:   content,
			SessionID: sessionID,
		})
	}
	return out, rows.Err()
}

// OpenStore picks JSON (default) or SQLite via DROVER_CODE_DREAM_BACKEND=sqlite
// (case-insensitive, surrounding space trimmed).
//
// When opening SQLite, if the database has no rows and .claude/memory.json
// exists with entries, they are imported in one transaction and the JSON file
// is renamed to memory.json.imported. Set DROVER_CODE_DREAM_SKIP_JSON_IMPORT=1
// to skip.
func OpenStore(workDir string) (Store, error) {
	backend := strings.TrimSpace(os.Getenv("DROVER_CODE_DREAM_BACKEND"))
	if strings.EqualFold(backend, "sqlite") {
		dbPath := filepath.Join(workDir, ".drover", "memory.db")
		jsonPath := filepath.Join(workDir, ".drover", "memory.json")
		s, err := NewSQLiteStore(dbPath)
		if err != nil {
			return nil, err
		}
		sq, ok := s.(*sqliteStore)
		if !ok {
			return s, nil
		}
		if err := sq.importFromJSONIfEmpty(jsonPath); err != nil {
			_ = sq.Close()
			return nil, err
		}
		return s, nil
	}
	path := filepath.Join(workDir, ".drover", "memory.json")
	return NewJSONStore(path)
}
