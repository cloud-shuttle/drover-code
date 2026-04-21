package ukc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

// Delete removes an instance from Kraft Cloud and the local registry.
type Delete struct{ M *Manager }

func (t *Delete) Name() string { return "ukc_delete" }
func (t *Delete) Description() string {
	return "Delete a Unikraft Cloud instance by id (from ukc_create or ukc_list) and remove it from the local registry."
}
func (t *Delete) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("instance_id", toolutil.NewSchema("string").Desc("Instance UUID")).
		Required("instance_id").
		Build()
}
func (t *Delete) NeedsPermission(_ json.RawMessage) bool { return true }

type deleteInput struct {
	InstanceID string `json:"instance_id"`
}

func (t *Delete) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	if t.M == nil {
		return "", fmt.Errorf("ukc_delete: manager not configured (set UKC_TOKEN)")
	}
	var in deleteInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("ukc_delete: bad input: %w", err)
	}
	in.InstanceID = strings.TrimSpace(in.InstanceID)
	if in.InstanceID == "" {
		return "", fmt.Errorf("ukc_delete: instance_id is required")
	}

	t.M.mu.Lock()
	_, ok := t.M.entries[in.InstanceID]
	cfg := t.M.cfg
	t.M.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("ukc_delete: unknown instance_id %q", in.InstanceID)
	}

	if err := DeleteInstance(ctx, cfg, in.InstanceID); err != nil {
		return "", err
	}

	t.M.mu.Lock()
	delete(t.M.entries, in.InstanceID)
	err := t.M.persistLocked()
	t.M.mu.Unlock()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("deleted instance %s", in.InstanceID), nil
}
