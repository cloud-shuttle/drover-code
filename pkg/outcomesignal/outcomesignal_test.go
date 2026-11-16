package outcomesignal

import (
	"encoding/json"
	"testing"
)

func TestFromRunError_success(t *testing.T) {
	s := FromRunError(nil)
	if s.CompileSuccess == nil || !*s.CompileSuccess || s.TestsPassed == nil || !*s.TestsPassed {
		t.Fatalf("expected success signals: %+v", s)
	}
}

func TestFromRunError_attributesMatchLearnerContract(t *testing.T) {
	raw, err := FromRunError(nil).AttributesJSON()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]bool
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{AttrCompileSuccess, AttrTestsPassed} {
		if !m[key] {
			t.Fatalf("expected %s true in BYOC attributes: %v", key, m)
		}
	}
}

func TestFromHostedJob(t *testing.T) {
	t.Run("merged success", func(t *testing.T) {
		s := FromHostedJob("succeeded", "merged")
		if s.CompileSuccess == nil || !*s.CompileSuccess || s.GitMergeMerged == nil || !*s.GitMergeMerged {
			t.Fatalf("expected all true: %+v", s)
		}
	})
	t.Run("no_changes", func(t *testing.T) {
		s := FromHostedJob("succeeded", "no_changes")
		if s.GitMergeMerged == nil || *s.GitMergeMerged {
			t.Fatalf("expected merge false: %+v", s)
		}
	})
	t.Run("failed job", func(t *testing.T) {
		s := FromHostedJob("failed", "")
		if s.CompileSuccess == nil || *s.CompileSuccess {
			t.Fatalf("expected compile false: %+v", s)
		}
	})
	t.Run("merge_conflict", func(t *testing.T) {
		s := FromHostedJob("merge_conflict", "merge_conflict")
		if s.CompileSuccess == nil || !*s.CompileSuccess {
			t.Fatal("worker ran")
		}
		if s.GitMergeMerged == nil || *s.GitMergeMerged {
			t.Fatal("merge not completed")
		}
	})
}

func TestAttributesJSON(t *testing.T) {
	f := false
	raw, err := (Signals{CompileSuccess: &f}).AttributesJSON()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]bool
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if v, ok := m[AttrCompileSuccess]; !ok || v {
		t.Fatalf("expected compile_success false in %v", m)
	}
}
