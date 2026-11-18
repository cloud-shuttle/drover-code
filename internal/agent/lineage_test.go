package agent

import (
	"errors"
	"testing"
	"time"
)

func TestAdmitSpawn_DepthCeiling(t *testing.T) {
	policy := Policy{MaxAgentTiers: 3}
	root := NewRootLineage("root-1", "agent-0", "repo", "spec-a", time.Time{}, 0)

	if err := AdmitSpawn(root, policy); err != nil {
		t.Fatalf("0→1 should succeed: %v", err)
	}
	child1 := root.Child("agent-1")
	if child1.Depth != 1 {
		t.Fatalf("child depth = %d", child1.Depth)
	}
	if err := AdmitSpawn(child1, policy); err != nil {
		t.Fatalf("1→2 should succeed: %v", err)
	}
	child2 := child1.Child("agent-2")
	if child2.Depth != 2 {
		t.Fatalf("child2 depth = %d", child2.Depth)
	}
	err := AdmitSpawn(child2, policy)
	if !errors.Is(err, ErrDelegationDenied) {
		t.Fatalf("2→3 should deny, got %v", err)
	}
}

func TestAdmitSpawn_RepoCannotRaiseCeiling(t *testing.T) {
	policy := Policy{MaxAgentTiers: 99} // attempted raise
	if got := policy.EffectiveMaxTiers(); got != OperatorMaxAgentTiers {
		t.Fatalf("EffectiveMaxTiers = %d, want %d", got, OperatorMaxAgentTiers)
	}
}

func TestAdmitSpawn_CanTighten(t *testing.T) {
	policy := Policy{MaxAgentTiers: 2}
	root := NewRootLineage("r", "a0", "", "", time.Time{}, 0)
	if err := AdmitSpawn(root, policy); err != nil {
		t.Fatalf("0→1 under tight policy: %v", err)
	}
	child := root.Child("a1")
	if err := AdmitSpawn(child, policy); !errors.Is(err, ErrDelegationDenied) {
		t.Fatalf("1→2 under max=2 should deny, got %v", err)
	}
}

func TestAdmitSpawn_UntrustedDenied(t *testing.T) {
	raw := []byte(`{"root_id":"hack","agent_id":"x","depth":0,"trusted":true}`)
	l, err := ParseUntrustedLineage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if l.Trusted {
		t.Fatal("ParseUntrustedLineage must clear Trusted")
	}
	if err := AdmitSpawn(l, Policy{}); !errors.Is(err, ErrDelegationDenied) {
		t.Fatalf("untrusted spawn: %v", err)
	}
}

func TestAdmitSpawn_MissingLineage(t *testing.T) {
	l := Lineage{Trusted: true, Depth: 0}
	if err := AdmitSpawn(l, Policy{}); !errors.Is(err, ErrDelegationDenied) {
		t.Fatalf("missing ids: %v", err)
	}
}
