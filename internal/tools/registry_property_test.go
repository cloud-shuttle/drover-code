package tools

import (
	"context"
	"encoding/json"
	"testing"
	"testing/quick"
)

type registryStubTool struct {
	name string
}

func (s registryStubTool) Name() string { return s.name }

func (s registryStubTool) Description() string { return "stub" }

func (s registryStubTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (s registryStubTool) NeedsPermission(json.RawMessage) bool { return false }

func (s registryStubTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "ok", nil
}

// Property: registering two distinct tools yields two definitions and both Execute paths work.
func TestProperty_RegistryRoundTrip(t *testing.T) {
	err := quick.Check(func(a, b byte) bool {
		n1 := string(rune('A' + rune(a%26)))
		n2 := string(rune('a' + rune(b%26)))
		if n1 == n2 {
			n2 = n2 + "_"
		}
		r := NewRegistry()
		r.Register(registryStubTool{n1})
		r.Register(registryStubTool{n2})
		if len(r.Definitions()) != 2 {
			return false
		}
		out1, err1 := r.Execute(context.Background(), n1, json.RawMessage(`{}`))
		if err1 != nil || out1 != "ok" {
			return false
		}
		out2, err2 := r.Execute(context.Background(), n2, json.RawMessage(`{}`))
		return err2 == nil && out2 == "ok"
	}, &quick.Config{MaxCount: 200})
	if err != nil {
		t.Fatal(err)
	}
}

// Property: unknown tool returns error from Execute, not panic.
func TestProperty_ExecuteUnknownTool(t *testing.T) {
	err := quick.Check(func(x byte) bool {
		name := string(rune('Z' + rune(x%10)))
		r := NewRegistry()
		_, e := r.Execute(context.Background(), name, json.RawMessage(`{}`))
		return e != nil
	}, &quick.Config{MaxCount: 100})
	if err != nil {
		t.Fatal(err)
	}
}
