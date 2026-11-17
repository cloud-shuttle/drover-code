// Package todo is the session checklist the model updates while it works.
package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

type item struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// List is the in-memory checklist for one process.
type List struct {
	mu    sync.Mutex
	items []item
}

func (t *List) Name() string { return "todo" }
func (t *List) Description() string {
	return "Replace the session checklist. Pass the full list each time. Completed items stay, with done set true."
}
func (t *List) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("items", toolutil.NewSchema("array").Desc("Checklist entries in order").
			Items(toolutil.NewSchema("object").
				Prop("text", toolutil.NewSchema("string").Desc("What to do")).
				Prop("done", toolutil.NewSchema("boolean").Desc("True when finished")).
				Required("text"))).
		Required("items").
		Build()
}
func (t *List) NeedsPermission(json.RawMessage) bool { return false }

func (t *List) Execute(_ context.Context, raw json.RawMessage) (string, error) {
	var in struct {
		Items []item `json:"items"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("todo: bad input: %w", err)
	}
	if len(in.Items) > 20 {
		return "", fmt.Errorf("todo: at most 20 items")
	}
	clean := make([]item, 0, len(in.Items))
	for _, it := range in.Items {
		it.Text = strings.TrimSpace(it.Text)
		if it.Text == "" {
			continue
		}
		clean = append(clean, it)
	}
	t.mu.Lock()
	t.items = clean
	t.mu.Unlock()
	return Render(clean), nil
}

// Render formats a checklist for the TUI and the model.
func Render(items []item) string {
	if len(items) == 0 {
		return "Todo list cleared."
	}
	var b strings.Builder
	b.WriteString("Todo:")
	for _, it := range items {
		mark := " "
		if it.Done {
			mark = "x"
		}
		fmt.Fprintf(&b, "\n- [%s] %s", mark, it.Text)
	}
	return b.String()
}
