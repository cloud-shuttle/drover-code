// Package permissions implements the permission engine.
package permissions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cloudshuttle/drover-code/internal/tools"
)

type Mode int

const (
	ModeDefault Mode = iota
	ModePlan
	ModeBypass
	// ModeAllowlist allows only tools in allowedTools (and persisted RuleAllow),
	// rejects deniedTools first, and never calls promptFn. Unknown tools are denied.
	ModeAllowlist
)

func ParseMode(s string) Mode {
	switch strings.ToLower(s) {
	case "plan":
		return ModePlan
	case "bypasspermissions", "bypass":
		return ModeBypass
	default:
		return ModeDefault
	}
}

type RuleKind int

const (
	RuleAllow RuleKind = iota
	RuleDeny
)

type Rule struct {
	Tool string   `json:"tool"`
	Kind RuleKind `json:"kind"`
}

type Engine struct {
	mu           sync.RWMutex
	mode         Mode
	rules        []Rule
	rulesPath    string
	promptFn     tools.PermissionFunc
	deniedTools  map[string]bool
	allowedTools map[string]bool
}

func NewEngine(
	mode Mode,
	allowedTools, deniedTools []string,
	rulesPath string,
	promptFn tools.PermissionFunc,
) *Engine {
	e := &Engine{
		mode:         mode,
		rulesPath:    rulesPath,
		promptFn:     promptFn,
		allowedTools: sliceToSet(allowedTools),
		deniedTools:  sliceToSet(deniedTools),
	}
	_ = e.load()
	return e
}

func (e *Engine) Check(ctx context.Context, toolName string, input json.RawMessage) (tools.Decision, error) {
	e.mu.RLock()
	mode := e.mode
	e.mu.RUnlock()

	if mode == ModeBypass {
		return tools.Allow, nil
	}
	if mode == ModeAllowlist {
		return e.checkAllowlist(toolName), nil
	}
	if e.deniedTools[toolName] {
		return tools.Deny, nil
	}
	if e.hasRule(toolName, RuleDeny) {
		return tools.Deny, nil
	}
	if e.allowedTools[toolName] {
		return tools.Allow, nil
	}
	if e.hasRule(toolName, RuleAllow) {
		return tools.Allow, nil
	}

	decision := e.promptFn(ctx, tools.PermissionRequest{
		ToolName: toolName,
		Input:    input,
		Summary:  fmt.Sprintf("Allow %s?", toolName),
	})

	if decision == tools.AlwaysAllow {
		e.addRule(Rule{Tool: toolName, Kind: RuleAllow})
	}

	return decision, nil
}

func (e *Engine) Mode() Mode {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.mode
}

// FastDecision returns a definite allow/deny decision if one can be made without
// prompting the user. The second return value is false when a prompt would be needed.
func (e *Engine) FastDecision(toolName string) (tools.Decision, bool) {
	e.mu.RLock()
	mode := e.mode
	e.mu.RUnlock()
	if mode == ModeBypass {
		return tools.Allow, true
	}
	if mode == ModeAllowlist {
		return e.checkAllowlist(toolName), true
	}
	if e.deniedTools[toolName] {
		return tools.Deny, true
	}
	if e.hasRule(toolName, RuleDeny) {
		return tools.Deny, true
	}
	if e.allowedTools[toolName] {
		return tools.Allow, true
	}
	if e.hasRule(toolName, RuleAllow) {
		return tools.Allow, true
	}
	return tools.Deny, false
}

// PersistAllow records an allow rule for toolName (used by plan-mode batch approval).
func (e *Engine) PersistAllow(toolName string) {
	e.addRule(Rule{Tool: toolName, Kind: RuleAllow})
}

func (e *Engine) SetMode(m Mode) {
	e.mu.Lock()
	e.mode = m
	e.mu.Unlock()
}

func (e *Engine) WrapPermitFn() tools.PermissionFunc {
	return func(ctx context.Context, req tools.PermissionRequest) tools.Decision {
		d, _ := e.Check(ctx, req.ToolName, req.Input)
		return d
	}
}

func (e *Engine) hasRule(tool string, kind RuleKind) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.hasRuleLocked(tool, kind)
}

func (e *Engine) addRule(r Rule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, existing := range e.rules {
		if existing.Tool == r.Tool && existing.Kind == r.Kind {
			return
		}
	}
	e.rules = append(e.rules, r)
	_ = e.saveLocked()
}

func (e *Engine) load() error {
	data, err := os.ReadFile(e.rulesPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return json.Unmarshal(data, &e.rules)
}

func (e *Engine) saveLocked() error {
	if e.rulesPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(e.rulesPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(e.rules, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := e.rulesPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, e.rulesPath)
}

func sliceToSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

// checkAllowlist returns Allow iff toolName is allowlisted (config map or
// persisted RuleAllow) and not denied.
func (e *Engine) checkAllowlist(toolName string) tools.Decision {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.deniedTools[toolName] {
		return tools.Deny
	}
	if e.hasRuleLocked(toolName, RuleDeny) {
		return tools.Deny
	}
	if e.allowedTools[toolName] {
		return tools.Allow
	}
	if e.hasRuleLocked(toolName, RuleAllow) {
		return tools.Allow
	}
	return tools.Deny
}

func (e *Engine) hasRuleLocked(tool string, kind RuleKind) bool {
	for _, r := range e.rules {
		if r.Tool == tool && r.Kind == kind {
			return true
		}
	}
	return false
}

