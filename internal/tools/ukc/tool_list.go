package ukc

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

// List returns all instances in the local registry (not a live Kraft Cloud query).
type List struct{ M *Manager }

func (t *List) Name() string { return "ukc_list" }
func (t *List) Description() string {
	return "List Unikraft Cloud instances stored in the local registry (id, name, url, age). " +
		"Use to recover instance ids from earlier sessions."
}
func (t *List) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").Build()
}
func (t *List) NeedsPermission(_ json.RawMessage) bool { return false }

func (t *List) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if t.M == nil {
		return "", fmt.Errorf("ukc_list: manager not configured (set UKC_TOKEN)")
	}

	t.M.mu.Lock()
	entries := make([]Entry, 0, len(t.M.entries))
	for _, e := range t.M.entries {
		entries = append(entries, e)
	}
	t.M.mu.Unlock()

	sort.Slice(entries, func(i, j int) bool { return entries[i].CreatedAt.Before(entries[j].CreatedAt) })

	if len(entries) == 0 {
		return "(no instances in registry)", nil
	}
	var b strings.Builder
	now := time.Now()
	for _, e := range entries {
		age := now.Sub(e.CreatedAt).Round(time.Second)
		fmt.Fprintf(&b, "- id=%s name=%s url=%s age=%s\n", e.ID, e.Name, e.URL, age.String())
	}
	return toolutil.Truncate(strings.TrimRight(b.String(), "\n")), nil
}
