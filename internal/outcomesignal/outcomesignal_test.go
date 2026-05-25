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
