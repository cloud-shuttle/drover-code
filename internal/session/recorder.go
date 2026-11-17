package session

import (
	"strings"
	"sync"
	"time"

	"github.com/cloudshuttle/drover-code/internal/api"
)

// Recorder writes the live conversation back to the same session file.
// Reset the conversation before calling Rotate. Save drops a snapshot whose
// id changed mid-write, so a clear does not copy the old transcript into the
// new session.
type Recorder struct {
	mu      sync.Mutex
	id      string
	store   *Store
	workDir string
	snap    func() []api.Message
}

// NewRecorder binds a session id to a snapshot function. snap should return
// the conversation's current messages.
func NewRecorder(store *Store, id, workDir string, snap func() []api.Message) *Recorder {
	return &Recorder{id: id, store: store, workDir: workDir, snap: snap}
}

// ID returns the session id that later saves will update.
func (r *Recorder) ID() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.id
}

// Use switches later saves to an existing session id.
func (r *Recorder) Use(id string) {
	if r == nil || strings.TrimSpace(id) == "" {
		return
	}
	r.mu.Lock()
	r.id = id
	r.mu.Unlock()
}

// Rotate starts a new session id. The previous file is left as it was.
func (r *Recorder) Rotate() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.id = NewID()
	r.mu.Unlock()
}

// Save writes the current snapshot when it is non-empty.
func (r *Recorder) Save() {
	if r == nil || r.snap == nil || r.store == nil {
		return
	}
	for attempt := 0; attempt < 2; attempt++ {
		r.mu.Lock()
		id := r.id
		r.mu.Unlock()
		msgs := r.snap()
		r.mu.Lock()
		same := id == r.id
		r.mu.Unlock()
		if !same {
			continue
		}
		if len(msgs) == 0 {
			return
		}
		_ = r.store.Save(Transcript{
			ID:       id,
			WorkDir:  r.workDir,
			Updated:  time.Now().UTC(),
			Messages: msgs,
		})
		return
	}
}
