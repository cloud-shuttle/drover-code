package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// OperatorMaxAgentTiers is the hard ceiling for trusted delegation depth.
// Repository config may tighten this value but must never raise it.
const OperatorMaxAgentTiers = 3

// ErrDelegationDenied is returned when spawn admission rejects a child.
var ErrDelegationDenied = errors.New("delegation denied")

// Lineage is the trusted spawn identity for an agent loop.
// Only trusted ingress may mint a root (Depth 0). Children inherit fields;
// prompts, env vars and child-supplied JSON cannot reset lineage or mint a root.
type Lineage struct {
	RootID       string    `json:"root_id"`
	ParentID     string    `json:"parent_id,omitempty"`
	AgentID      string    `json:"agent_id"`
	Depth        int       `json:"depth"`
	ApprovedSpec string    `json:"approved_spec_hash,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	Deadline     time.Time `json:"deadline,omitempty"`
	BudgetTokens int64     `json:"budget_tokens,omitempty"`
	Trusted      bool      `json:"trusted"`
}

// Policy controls spawn admission. MaxAgentTiers is clamped to [1, OperatorMaxAgentTiers].
type Policy struct {
	MaxAgentTiers int `json:"maxAgentTiers"`
}

// EffectiveMaxTiers returns the operator-clamped tier ceiling.
func (p Policy) EffectiveMaxTiers() int {
	n := p.MaxAgentTiers
	if n <= 0 || n > OperatorMaxAgentTiers {
		return OperatorMaxAgentTiers
	}
	return n
}

// NewRootLineage mints a trusted root (depth 0). Call only from trusted ingress.
func NewRootLineage(rootID, agentID, scope, approvedSpec string, deadline time.Time, budget int64) Lineage {
	if rootID == "" {
		rootID = agentID
	}
	return Lineage{
		RootID:       rootID,
		AgentID:      agentID,
		Depth:        0,
		ApprovedSpec: approvedSpec,
		Scope:        scope,
		Deadline:     deadline,
		BudgetTokens: budget,
		Trusted:      true,
	}
}

// Child derives a child lineage from a trusted parent. Depth is parent+1.
func (p Lineage) Child(childID string) Lineage {
	return Lineage{
		RootID:       p.RootID,
		ParentID:     p.AgentID,
		AgentID:      childID,
		Depth:        p.Depth + 1,
		ApprovedSpec: p.ApprovedSpec,
		Scope:        p.Scope,
		Deadline:     p.Deadline,
		BudgetTokens: p.BudgetTokens,
		Trusted:      p.Trusted,
	}
}

// DelegationDeniedEvent is the structured deny payload (no prompts).
type DelegationDeniedEvent struct {
	RootID          string `json:"root_id"`
	ParentID        string `json:"parent_id"`
	RequestedDepth  int    `json:"requested_depth"`
	MaxTiers        int    `json:"max_tiers"`
	Reason          string `json:"reason"`
}

func (e DelegationDeniedEvent) Error() string {
	b, _ := json.Marshal(e)
	return fmt.Sprintf("delegation_denied%s", string(b))
}

// AdmitSpawn validates that parent may launch a child at parent.Depth+1.
// Forged/missing lineage and untrusted parents are denied.
func AdmitSpawn(parent Lineage, policy Policy) error {
	max := policy.EffectiveMaxTiers()
	if !parent.Trusted {
		return fmt.Errorf("%w: %s", ErrDelegationDenied, DelegationDeniedEvent{
			RootID:         parent.RootID,
			ParentID:       parent.AgentID,
			RequestedDepth: parent.Depth + 1,
			MaxTiers:       max,
			Reason:         "untrusted_or_forged_lineage",
		}.Error())
	}
	if parent.RootID == "" || parent.AgentID == "" {
		return fmt.Errorf("%w: %s", ErrDelegationDenied, DelegationDeniedEvent{
			RootID:         parent.RootID,
			ParentID:       parent.AgentID,
			RequestedDepth: parent.Depth + 1,
			MaxTiers:       max,
			Reason:         "missing_lineage",
		}.Error())
	}
	if parent.Depth < 0 {
		return fmt.Errorf("%w: %s", ErrDelegationDenied, DelegationDeniedEvent{
			RootID:         parent.RootID,
			ParentID:       parent.AgentID,
			RequestedDepth: parent.Depth + 1,
			MaxTiers:       max,
			Reason:         "invalid_depth",
		}.Error())
	}
	requested := parent.Depth + 1
	if requested >= max {
		return fmt.Errorf("%w: %s", ErrDelegationDenied, DelegationDeniedEvent{
			RootID:         parent.RootID,
			ParentID:       parent.AgentID,
			RequestedDepth: requested,
			MaxTiers:       max,
			Reason:         "max_agent_tiers",
		}.Error())
	}
	if !parent.Deadline.IsZero() && time.Now().After(parent.Deadline) {
		return fmt.Errorf("%w: %s", ErrDelegationDenied, DelegationDeniedEvent{
			RootID:         parent.RootID,
			ParentID:       parent.AgentID,
			RequestedDepth: requested,
			MaxTiers:       max,
			Reason:         "deadline_exceeded",
		}.Error())
	}
	return nil
}

// ParseUntrustedLineage refuses to treat child/env JSON as a trusted root.
// Use only to detect forgery attempts; never promote the result to Trusted.
func ParseUntrustedLineage(raw []byte) (Lineage, error) {
	var l Lineage
	if err := json.Unmarshal(raw, &l); err != nil {
		return Lineage{}, err
	}
	l.Trusted = false
	return l, nil
}
