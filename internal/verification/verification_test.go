package verification

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestVerifierCannotPush(t *testing.T) {
	p := DefaultVerifierProfile("abc")
	if err := p.AssertCapabilities("git_push"); !errors.Is(err, ErrVerifierReadonly) {
		t.Fatalf("got %v", err)
	}
	if err := p.AssertCapabilities("auto_integrate"); !errors.Is(err, ErrVerifierReadonly) {
		t.Fatalf("got %v", err)
	}
}

func TestStaleHeadBlocks(t *testing.T) {
	r := &Runner{
		Profile: DefaultVerifierProfile("expected-sha"),
		Journey: []JourneyStep{
			{Name: "freshness", Command: "git", Args: nil, Required: true},
		},
		RunCommand: func(ctx context.Context, name string, args ...string) (string, int, error) {
			return "other-sha\n", 0, nil
		},
	}
	ev, err := r.Run(context.Background())
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("want blocked, got %v", err)
	}
	if ev.Verdict != VerdictBlocked {
		t.Fatalf("verdict %s", ev.Verdict)
	}
}

func TestMissingRequiredStepBlocks(t *testing.T) {
	r := &Runner{
		Profile: DefaultVerifierProfile(""),
		Journey: []JourneyStep{
			{Name: "build", Command: "true", Required: true},
		},
		RunCommand: func(ctx context.Context, name string, args ...string) (string, int, error) {
			return "", 1, errors.New("fail")
		},
	}
	ev, err := r.Run(context.Background())
	if err == nil || ev.Verdict == VerdictPass {
		t.Fatalf("must not pass: %+v %v", ev, err)
	}
}

func TestPassJourney(t *testing.T) {
	r := &Runner{
		Profile: DefaultVerifierProfile("abc"),
		Journey: []JourneyStep{
			{Name: "freshness", Command: "git", Required: true},
			{Name: "build", Command: "go", Required: true},
		},
		RunCommand: func(ctx context.Context, name string, args ...string) (string, int, error) {
			if name == "git" {
				return "abc\n", 0, nil
			}
			return "ok", 0, nil
		},
	}
	ev, err := r.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ev.Verdict != VerdictPass {
		t.Fatalf("verdict %s steps=%+v", ev.Verdict, ev.Steps)
	}
	if ev.BuildDigest == "" {
		t.Fatal("expected build digest")
	}
}

func TestRepairExhausted(t *testing.T) {
	r := &Runner{
		Profile: Profile{Mode: ModeFixer, MaxRepairAttempts: 1, HeadSHA: "h"},
		Journey: []JourneyStep{{Name: "build", Command: "x", Required: true}},
		RunCommand: func(ctx context.Context, name string, args ...string) (string, int, error) {
			return "fail", 1, errors.New("x")
		},
	}
	_, err := RepairLoop(context.Background(), r, func(ctx context.Context, attempt int, ev Evidence) error {
		return nil
	})
	if !errors.Is(err, ErrRepairExhausted) {
		t.Fatalf("got %v", err)
	}
}

func TestEvidenceJSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ev := Evidence{HeadSHA: "deadbeef", Verdict: VerdictPass}
	path, err := WriteEvidence(dir, "task-1", ev)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "deadbeef.json") {
		t.Fatalf("path %s", path)
	}
}
