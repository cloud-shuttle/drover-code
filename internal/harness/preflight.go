// Package harness implements alignment gates and trusted checkpoint state
// for Drover Code agent runs. Runtime gates remain mandatory even when
// skill recipes are skipped.
package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// WorkSize classifies the cost-of-wrongness of a change.
type WorkSize string

const (
	WorkSmall  WorkSize = "small"  // reversible, low wrongness-cost
	WorkSingle WorkSize = "single" // single-session
	WorkMulti  WorkSize = "multi"  // multi-session
)

// ErrSpecRequired is returned when mutation is blocked pending an accepted spec.
var ErrSpecRequired = errors.New("accepted spec required before mutation")

// ErrSpecMismatch is returned when the bound hash no longer matches the on-disk SPEC.
var ErrSpecMismatch = errors.New("accepted spec hash mismatch; re-plan required")

// ErrScopeExpanded is returned when implementation scope grows past the approved bound.
var ErrScopeExpanded = errors.New("scope expansion requires re-approval")

// Spec is the approved planning artifact (docs/tasks/<id>/SPEC.md content metadata).
type Spec struct {
	TaskID       string   `json:"task_id"`
	Goal         string   `json:"goal"`
	NonGoals     []string `json:"non_goals,omitempty"`
	Constraints  []string `json:"constraints,omitempty"`
	Boundaries   []string `json:"boundaries,omitempty"` // touched paths / packages
	Acceptance   []string `json:"acceptance,omitempty"`
	WorkSize     WorkSize `json:"work_size"`
	SkipReason   string   `json:"skip_reason,omitempty"` // required when WorkSmall
	ContentHash  string   `json:"content_hash"`
	RepoRevision string   `json:"repo_revision,omitempty"`
	Approver     string   `json:"approver,omitempty"`
	ApprovedAt   time.Time `json:"approved_at,omitempty"`
}

// Checkpoint is trusted runtime state bound to an accepted spec.
type Checkpoint struct {
	AcceptedSpecHash string    `json:"accepted_spec_hash"`
	Approver         string    `json:"approver"`
	RepoRevision     string    `json:"repo_revision"`
	Scope            []string  `json:"scope"`
	TaskID           string    `json:"task_id"`
	WorkSize         WorkSize  `json:"work_size"`
	SkipReason       string    `json:"skip_reason,omitempty"`
	AcceptedAt       time.Time `json:"accepted_at"`
}

// Gate enforces the cost-of-wrongness alignment barrier before mutating tools.
type Gate struct {
	mu         sync.RWMutex
	workDir    string
	checkpoint *Checkpoint
	// RequireForHeadless forces a non-trivial gate when true (coordinator/cloud).
	RequireForHeadless bool
}

// NewGate roots trusted state under workDir.
func NewGate(workDir string) *Gate {
	return &Gate{workDir: workDir}
}

// MutatingTools lists tools that change the workspace or remote state.
var MutatingTools = map[string]bool{
	"write_file":    true,
	"edit_file":     true,
	"bash":          true,
	"git_add":       true,
	"git_commit":    true,
	"git_push":      true,
	"create_branch": true,
	"create_pr":     true,
}

// IsMutating reports whether toolName may change workspace or remotes.
func IsMutating(toolName string) bool {
	return MutatingTools[toolName]
}

// HashContent returns a stable hex SHA-256 of SPEC body bytes.
func HashContent(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// Accept records an approved spec into trusted checkpoint state.
// A model-written "approved" label is insufficient; callers must supply Approver
// from trusted ingress (operator / Cloud).
func (g *Gate) Accept(spec Spec) error {
	if strings.TrimSpace(spec.Approver) == "" {
		return fmt.Errorf("approver required for trusted acceptance")
	}
	if spec.WorkSize == WorkSmall {
		if strings.TrimSpace(spec.SkipReason) == "" {
			return fmt.Errorf("small work requires skip_reason")
		}
	} else {
		if strings.TrimSpace(spec.Goal) == "" {
			return fmt.Errorf("goal required for %s work", spec.WorkSize)
		}
		if len(spec.Acceptance) == 0 {
			return fmt.Errorf("acceptance checks required for %s work", spec.WorkSize)
		}
		if strings.TrimSpace(spec.ContentHash) == "" {
			return fmt.Errorf("content_hash required")
		}
	}
	if spec.TaskID == "" {
		return fmt.Errorf("task_id required")
	}
	cp := &Checkpoint{
		AcceptedSpecHash: spec.ContentHash,
		Approver:         spec.Approver,
		RepoRevision:     spec.RepoRevision,
		Scope:            append([]string(nil), spec.Boundaries...),
		TaskID:           spec.TaskID,
		WorkSize:         spec.WorkSize,
		SkipReason:       spec.SkipReason,
		AcceptedAt:       time.Now().UTC(),
	}
	if err := g.persist(cp); err != nil {
		return err
	}
	g.mu.Lock()
	g.checkpoint = cp
	g.mu.Unlock()
	return nil
}

func (g *Gate) persist(cp *Checkpoint) error {
	dir := filepath.Join(g.workDir, "docs", "tasks", cp.TaskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "CHECKPOINT.json"), b, 0o644)
}

// LoadCheckpoint reloads trusted state from disk (does not trust model labels).
func (g *Gate) LoadCheckpoint(taskID string) (*Checkpoint, error) {
	path := filepath.Join(g.workDir, "docs", "tasks", taskID, "CHECKPOINT.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cp Checkpoint
	if err := json.Unmarshal(b, &cp); err != nil {
		return nil, err
	}
	g.mu.Lock()
	g.checkpoint = &cp
	g.mu.Unlock()
	return &cp, nil
}

// Checkpoint returns the in-memory trusted checkpoint, if any.
func (g *Gate) Checkpoint() *Checkpoint {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.checkpoint == nil {
		return nil
	}
	cp := *g.checkpoint
	return &cp
}

// AllowMutation checks whether a mutating tool may run.
// Read-only research tools are unaffected (caller should skip non-mutating).
func (g *Gate) AllowMutation(toolName string, touchedPaths []string) error {
	if !IsMutating(toolName) {
		return nil
	}
	g.mu.RLock()
	cp := g.checkpoint
	g.mu.RUnlock()
	if cp == nil {
		if g.RequireForHeadless {
			return fmt.Errorf("%w: no accepted checkpoint", ErrSpecRequired)
		}
		// Interactive sessions may proceed for small work only after explicit Accept.
		return fmt.Errorf("%w: accept a SPEC (or small-work skip) before %s", ErrSpecRequired, toolName)
	}
	if cp.WorkSize != WorkSmall {
		specPath := filepath.Join(g.workDir, "docs", "tasks", cp.TaskID, "SPEC.md")
		if body, err := os.ReadFile(specPath); err == nil {
			if HashContent(body) != cp.AcceptedSpecHash {
				return ErrSpecMismatch
			}
		}
	}
	if len(cp.Scope) > 0 && len(touchedPaths) > 0 {
		for _, p := range touchedPaths {
			if !pathInScope(p, cp.Scope) {
				return fmt.Errorf("%w: %s outside approved scope", ErrScopeExpanded, p)
			}
		}
	}
	return nil
}

func pathInScope(path string, scope []string) bool {
	clean := filepath.Clean(path)
	for _, s := range scope {
		s = filepath.Clean(s)
		if clean == s || strings.HasPrefix(clean, s+string(os.PathSeparator)) {
			return true
		}
		// package-style prefixes
		if strings.HasPrefix(clean, s) {
			return true
		}
	}
	return false
}

// WriteSPEC writes docs/tasks/<id>/SPEC.md and returns its content hash.
func WriteSPEC(workDir, taskID, body string) (string, error) {
	dir := filepath.Join(workDir, "docs", "tasks", taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "SPEC.md")
	b := []byte(body)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return "", err
	}
	return HashContent(b), nil
}
