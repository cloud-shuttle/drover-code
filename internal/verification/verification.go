// Package verification implements verifier-per-PR discipline for Drover Code.
// The verifier cannot edit source/oracles or push/merge. A separate fixer
// receives only approved-path workspace writes and returns a patch.
package verification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultMaxRepairAttempts is the initial local policy ceiling.
	DefaultMaxRepairAttempts = 2
	// DefaultRepairBudget is the total wall-clock budget for repair loops.
	DefaultRepairBudget = 20 * time.Minute
)

var (
	ErrBlocked          = errors.New("verification blocked")
	ErrVerifierReadonly = errors.New("verifier cannot mutate source or remotes")
	ErrRepairExhausted  = errors.New("repair budget exhausted")
)

// Mode selects verifier vs fixer capabilities.
type Mode string

const (
	ModeVerifier Mode = "verifier"
	ModeFixer    Mode = "fixer"
)

// Profile is the restricted capability set for a verification run.
type Profile struct {
	Mode               Mode
	HeadSHA            string
	MaxRepairAttempts  int
	RepairBudget       time.Duration
	ApprovedWritePaths []string // fixer only
	AllowPushMerge     bool     // always false for verifier; fixer also false by default
	SpendCeilingTokens int64
}

// DefaultVerifierProfile returns a read-only verifier profile for headSHA.
func DefaultVerifierProfile(headSHA string) Profile {
	return Profile{
		Mode:              ModeVerifier,
		HeadSHA:           headSHA,
		MaxRepairAttempts: DefaultMaxRepairAttempts,
		RepairBudget:      DefaultRepairBudget,
		AllowPushMerge:    false,
	}
}

// Evidence is the machine-checkable verdict artifact.
type Evidence struct {
	HeadSHA       string            `json:"head_sha"`
	BuildDigest   string            `json:"build_digest,omitempty"`
	FixtureSeed   string            `json:"fixture_seed,omitempty"`
	Steps         []StepResult      `json:"steps"`
	Verdict       Verdict           `json:"verdict"`
	StartedAt     time.Time         `json:"started_at"`
	FinishedAt    time.Time         `json:"finished_at"`
	Meta          map[string]string `json:"meta,omitempty"`
	RepairAttempt int               `json:"repair_attempt,omitempty"`
}

// Verdict is the outcome of a verification run.
type Verdict string

const (
	VerdictPass    Verdict = "pass"
	VerdictBlocked Verdict = "blocked"
	VerdictFail    Verdict = "fail"
)

// StepResult records one verification step.
type StepResult struct {
	Name     string `json:"name"`
	Expected string `json:"expected,omitempty"`
	Observed string `json:"observed,omitempty"`
	ExitCode int    `json:"exit_code"`
	OK       bool   `json:"ok"`
	Skipped  bool   `json:"skipped,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// Runner executes verification steps against an isolated worktree.
type Runner struct {
	WorkDir string
	Profile Profile
	// Journey is the smoke user journey (CLI/API). Empty uses default build+test.
	Journey []JourneyStep
	// RunCommand overrides command execution (tests).
	RunCommand func(ctx context.Context, name string, args ...string) (stdout string, exitCode int, err error)
}

// JourneyStep is one affected CLI/API/browser user journey check.
type JourneyStep struct {
	Name     string
	Command  string
	Args     []string
	Expect   string // substring that must appear in stdout, or empty
	Required bool
}

// DefaultSmokeJourney returns the initial local smoke journey (build + unit subset).
func DefaultSmokeJourney() []JourneyStep {
	return []JourneyStep{
		{Name: "freshness", Command: "git", Args: []string{"rev-parse", "HEAD"}, Required: true},
		{Name: "build", Command: "go", Args: []string{"build", "-o", os.DevNull, "./cmd/drover-code"}, Required: true},
		{Name: "harness-unit", Command: "go", Args: []string{"test", "./internal/harness/", "./internal/agent/", "-run", "TestAdmitSpawn|TestSmallWork", "-count=1"}, Required: false},
	}
}

// AssertCapabilities refuses verifier write/push attempts.
func (p Profile) AssertCapabilities(action string) error {
	switch action {
	case "write_source", "edit_oracle", "git_push", "git_merge", "auto_integrate":
		if p.Mode == ModeVerifier || !p.AllowPushMerge {
			return fmt.Errorf("%w: action=%s mode=%s", ErrVerifierReadonly, action, p.Mode)
		}
	case "write_approved_path":
		if p.Mode == ModeVerifier {
			return fmt.Errorf("%w: action=%s", ErrVerifierReadonly, action)
		}
	}
	return nil
}

// Run executes the journey and returns evidence. Missing evidence / skipped
// required paths are blocked, not green.
func (r *Runner) Run(ctx context.Context) (Evidence, error) {
	ev := Evidence{
		HeadSHA:   r.Profile.HeadSHA,
		StartedAt: time.Now().UTC(),
		Meta:      map[string]string{},
	}
	if r.Profile.Mode == ModeVerifier {
		if err := r.Profile.AssertCapabilities("write_source"); err != nil && false {
			// capability self-check retained for API surface
			_ = err
		}
	}
	steps := r.Journey
	if len(steps) == 0 {
		steps = []JourneyStep{
			{Name: "freshness", Command: "git", Args: []string{"rev-parse", "HEAD"}, Required: true},
			{Name: "build", Command: "go", Args: []string{"build", "-o", filepath.Join(os.TempDir(), "drover-code-verify"), "./cmd/drover-code"}, Required: true},
			{Name: "lineage-unit", Command: "go", Args: []string{"test", "./internal/agent/", "-run", "TestAdmitSpawn", "-count=1"}, Required: true},
			{Name: "harness-unit", Command: "go", Args: []string{"test", "./internal/harness/", "-count=1"}, Required: true},
		}
	}

	run := r.RunCommand
	if run == nil {
		run = defaultRun
	}

	blocked := false
	failed := false
	for _, step := range steps {
		select {
		case <-ctx.Done():
			ev.FinishedAt = time.Now().UTC()
			ev.Verdict = VerdictBlocked
			return ev, ctx.Err()
		default:
		}
		stdout, code, err := run(ctx, step.Command, step.Args...)
		sr := StepResult{
			Name:     step.Name,
			Expected: step.Expect,
			Observed: truncate(stdout, 512),
			ExitCode: code,
		}
		if err != nil && code == 0 {
			sr.OK = false
			sr.Detail = err.Error()
			if step.Required {
				blocked = true
			} else {
				sr.Skipped = true
			}
		} else if code != 0 {
			sr.OK = false
			sr.Detail = fmt.Sprintf("exit %d: %v", code, err)
			if step.Required {
				failed = true
			} else {
				sr.Skipped = true
			}
		} else if step.Expect != "" && !strings.Contains(stdout, step.Expect) {
			sr.OK = false
			sr.Detail = "expected substring missing"
			if step.Required {
				failed = true
			}
		} else {
			sr.OK = true
		}
		if step.Name == "freshness" && sr.OK {
			got := strings.TrimSpace(stdout)
			if r.Profile.HeadSHA != "" && got != r.Profile.HeadSHA {
				sr.OK = false
				sr.Detail = fmt.Sprintf("stale head: got %s want %s", got, r.Profile.HeadSHA)
				blocked = true
			}
			ev.HeadSHA = got
		}
		if step.Name == "build" && sr.OK {
			ev.BuildDigest = shortDigest(stdout + r.Profile.HeadSHA)
		}
		ev.Steps = append(ev.Steps, sr)
	}

	ev.FinishedAt = time.Now().UTC()
	switch {
	case blocked:
		ev.Verdict = VerdictBlocked
		return ev, ErrBlocked
	case failed:
		ev.Verdict = VerdictFail
		return ev, fmt.Errorf("%w: journey failure", ErrBlocked)
	default:
		// Any required step missing entirely → blocked
		for _, step := range steps {
			if !step.Required {
				continue
			}
			found := false
			for _, sr := range ev.Steps {
				if sr.Name == step.Name {
					found = true
					if !sr.OK {
						ev.Verdict = VerdictBlocked
						return ev, ErrBlocked
					}
				}
			}
			if !found {
				ev.Verdict = VerdictBlocked
				return ev, ErrBlocked
			}
		}
		ev.Verdict = VerdictPass
		return ev, nil
	}
}

// RepairLoop runs verifier → fixer → reverify up to MaxRepairAttempts within RepairBudget.
// Fixer is invoked via fixFn which must only write approved paths and return a patch.
// Never auto-merges.
func RepairLoop(ctx context.Context, r *Runner, fixFn func(ctx context.Context, attempt int, ev Evidence) error) (Evidence, error) {
	max := r.Profile.MaxRepairAttempts
	if max <= 0 {
		max = DefaultMaxRepairAttempts
	}
	budget := r.Profile.RepairBudget
	if budget <= 0 {
		budget = DefaultRepairBudget
	}
	deadline := time.Now().Add(budget)
	var last Evidence
	for attempt := 0; attempt <= max; attempt++ {
		if time.Now().After(deadline) {
			return last, ErrRepairExhausted
		}
		cctx, cancel := context.WithDeadline(ctx, deadline)
		ev, err := r.Run(cctx)
		cancel()
		ev.RepairAttempt = attempt
		last = ev
		if err == nil && ev.Verdict == VerdictPass {
			return ev, nil
		}
		if attempt == max {
			return last, ErrRepairExhausted
		}
		if fixFn == nil {
			return last, ErrRepairExhausted
		}
		if r.Profile.Mode == ModeVerifier {
			// Coordinator schedules fixer as sibling — caller switches profile.
			return last, fmt.Errorf("switch to fixer mode before repair")
		}
		if err := fixFn(ctx, attempt+1, ev); err != nil {
			return last, err
		}
	}
	return last, ErrRepairExhausted
}

// WriteEvidence persists evidence JSON under docs/tasks/<id>/evidence/<sha>.json.
func WriteEvidence(workDir, taskID string, ev Evidence) (string, error) {
	dir := filepath.Join(workDir, "docs", "tasks", taskID, "evidence")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := ev.HeadSHA
	if name == "" {
		name = fmt.Sprintf("run-%d", time.Now().Unix())
	}
	path := filepath.Join(dir, name+".json")
	b, err := json.MarshalIndent(ev, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, b, 0o644)
}

func defaultRun(ctx context.Context, name string, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	return string(out), code, err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func shortDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}
