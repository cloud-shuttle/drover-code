// Package explore is the local read-only search tool.
// The parent conversation receives only the file map Ask returns.
package explore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

// Agent searches the working tree and returns a short file map.
// Ask is wired by the process. A nil Ask means this session has no explore pass.
type Agent struct {
	Ask func(ctx context.Context, task string) (string, error)
}

func (t *Agent) Name() string { return "explore" }

func (t *Agent) Description() string {
	return "Search the working tree and return a short map of the files that matter. " +
		"Use this for a broad question about where code lives. " +
		"The search stays out of this conversation; only the map comes back. " +
		"Do not use it to edit files or run commands. Local sessions only."
}

func (t *Agent) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("task", toolutil.NewSchema("string").Desc("What to find in the working tree")).
		Required("task").
		Build()
}

func (t *Agent) NeedsPermission(json.RawMessage) bool { return false }

func (t *Agent) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	if t == nil || t.Ask == nil {
		return "", fmt.Errorf("explore: not available in this session")
	}
	var in struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("explore: bad input: %w", err)
	}
	task := strings.TrimSpace(in.Task)
	if task == "" {
		return "", fmt.Errorf("explore: task is empty")
	}
	return t.Ask(ctx, task)
}
