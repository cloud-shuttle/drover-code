package ukc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

// DeleteAll removes every instance listed in the registry from Kraft Cloud and clears the file.
type DeleteAll struct{ M *Manager }

func (t *DeleteAll) Name() string { return "ukc_delete_all" }
func (t *DeleteAll) Description() string {
	return "Delete all Unikraft Cloud instances recorded in ~/.drover-code/ukc-instances.json. " +
		"Use as a cleanup escape hatch after tasks or crashed sessions."
}
func (t *DeleteAll) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").Build()
}
func (t *DeleteAll) NeedsPermission(_ json.RawMessage) bool { return true }

func (t *DeleteAll) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	if t.M == nil {
		return "", fmt.Errorf("ukc_delete_all: manager not configured (set UKC_TOKEN)")
	}

	t.M.mu.Lock()
	ids := make([]string, 0, len(t.M.entries))
	for id := range t.M.entries {
		ids = append(ids, id)
	}
	cfg := t.M.cfg
	t.M.mu.Unlock()

	var out strings.Builder
	var mu sync.Mutex
	record := func(format string, args ...any) {
		mu.Lock()
		fmt.Fprintf(&out, format, args...)
		mu.Unlock()
	}

	var g errgroup.Group
	for _, id := range ids {
		id := id
		g.Go(func() error {
			err := DeleteInstance(ctx, cfg, id)
			if err != nil {
				record("error deleting %s: %v\n", id, err)
				return err
			}
			record("deleted %s\n", id)
			return nil
		})
	}
	_ = g.Wait() // errors already recorded

	t.M.mu.Lock()
	t.M.entries = make(map[string]Entry)
	err := t.M.persistLocked()
	t.M.mu.Unlock()
	if err != nil {
		return "", err
	}

	s := strings.TrimSpace(out.String())
	if s == "" {
		s = "(no instances in registry)"
	}
	return toolutil.Truncate("registry cleared.\n\n" + s), nil
}
