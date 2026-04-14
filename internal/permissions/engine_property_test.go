package permissions

import (
	"context"
	"encoding/json"
	"testing"
	"testing/quick"

	"github.com/cloudshuttle/drover-code/internal/tools"
)

// Property: with ModeDefault, anything in deniedTools is denied before allow-list
// or prompt, and the prompt is never consulted for that tool.
func TestProperty_DenyListBlocksAllowListAndPrompt(t *testing.T) {
	promptCalls := 0
	prompt := func(context.Context, tools.PermissionRequest) tools.Decision {
		promptCalls++
		return tools.Allow
	}

	cfg := &quick.Config{MaxCount: 200}

	err := quick.Check(func(toolByte byte) bool {
		name := string(rune('a' + int(toolByte)%26))
		promptCalls = 0

		e := NewEngine(
			ModeDefault,
			[]string{name},
			[]string{name},
			"",
			prompt,
		)
		d, err := e.Check(context.Background(), name, json.RawMessage(`{}`))
		if err != nil {
			return false
		}
		if d != tools.Deny {
			return false
		}
		return promptCalls == 0
	}, cfg)

	if err != nil {
		t.Fatal(err)
	}
}

// Property: bypass mode always allows and never calls the prompt.
func TestProperty_BypassIgnoresDenyList(t *testing.T) {
	promptCalls := 0
	prompt := func(context.Context, tools.PermissionRequest) tools.Decision {
		promptCalls++
		return tools.Deny
	}

	cfg := &quick.Config{MaxCount: 100}

	err := quick.Check(func(toolByte byte) bool {
		name := string(rune('a' + int(toolByte)%26))
		promptCalls = 0

		e := NewEngine(
			ModeBypass,
			[]string{name},
			[]string{name},
			"",
			prompt,
		)
		d, err := e.Check(context.Background(), name, json.RawMessage(`{}`))
		if err != nil {
			return false
		}
		return d == tools.Allow && promptCalls == 0
	}, cfg)

	if err != nil {
		t.Fatal(err)
	}
}
