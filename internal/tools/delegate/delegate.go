// Package delegate is the local coding subagent tool.
// The parent conversation receives only the short result Ask returns.
package delegate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

// Agent runs one coding task in a child loop.
// Ask is wired by the process. A nil Ask means this session has no subagent.
type Agent struct {
	Ask func(ctx context.Context, task string) (string, error)
}

func (t *Agent) Name() string { return "delegate" }

func (t *Agent) Description() string {
	return "Do one coding task in a separate loop and return a short result. " +
		"The child can read, edit, and run bash. Its tool calls stay out of this conversation. " +
		"Use it for a chunk of work that would flood this conversation. " +
		"It cannot ask the user or start another delegate. Local sessions only."
}

func (t *Agent) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("task", toolutil.NewSchema("string").Desc("The coding task to complete in the working tree")).
		Required("task").
		Build()
}

func (t *Agent) NeedsPermission(json.RawMessage) bool { return true }

func (t *Agent) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	if t == nil || t.Ask == nil {
		return "", fmt.Errorf("delegate: not available in this session")
	}
	var in struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("delegate: bad input: %w", err)
	}
	task := strings.TrimSpace(in.Task)
	if task == "" {
		return "", fmt.Errorf("delegate: task is empty")
	}
	return t.Ask(ctx, task)
}
