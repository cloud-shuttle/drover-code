package tools

import (
	"context"
	"encoding/json"
	"testing"
)

// FuzzRegistryExecute ensures Execute never panics for a registered tool or a missing name.
func FuzzRegistryExecute(f *testing.F) {
	f.Add("tool", []byte(`{}`))
	f.Add("", []byte(`{"x":1}`))

	f.Fuzz(func(t *testing.T, name string, input []byte) {
		if len(name) > 256 {
			return
		}
		const maxIn = 16 << 10
		if len(input) > maxIn {
			input = input[:maxIn]
		}
		r := NewRegistry()
		r.Register(registryStubTool{name: name})
		_, _ = r.Execute(context.Background(), name, json.RawMessage(input))
		_, _ = r.Execute(context.Background(), name+"\x00missing", json.RawMessage(`{}`))
	})
}
