package ukc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

// Create provisions a Kraft Cloud instance and waits for the agent /health endpoint.
type Create struct{ M *Manager }

func (t *Create) Name() string { return "ukc_create" }
func (t *Create) Description() string {
	return "Create a Unikraft Cloud instance running the drover HTTP agent (remote exec). " +
		"Requires UKC_TOKEN (and optionally UKC_METRO). " +
		"Returns instance id, URL, and stores credentials in ~/.drover-code/ukc-instances.json. " +
		"When finished with an instance, call ukc_delete for that id; at end of a task prefer ukc_delete_all to avoid leaving instances running."
}
func (t *Create) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("name", toolutil.NewSchema("string").Desc("Unique name for the instance (DNS label)")).
		Prop("image", toolutil.NewSchema("string").Desc("OCI image (default: UKC_DEFAULT_AGENT_IMAGE or built-in default)")).
		Prop("environment", toolutil.NewSchema("string").Desc("Optional language environment to boot (e.g., rust, node, python, go). Replaces 'image' with the cached template ID if provided.")).
		Prop("memory_mb", toolutil.NewSchema("integer").Desc("Memory in MB (optional; cloud default applies if unset)")).
		Required("name").
		Build()
}
func (t *Create) NeedsPermission(_ json.RawMessage) bool { return true }

type createInput struct {
	Name        string `json:"name"`
	Image       string `json:"image"`
	Environment string `json:"environment"`
	MemoryMB    int    `json:"memory_mb"`
}

func (t *Create) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	if t.M == nil {
		return "", fmt.Errorf("ukc_create: manager not configured (set UKC_TOKEN)")
	}
	var in createInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("ukc_create: bad input: %w", err)
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return "", fmt.Errorf("ukc_create: name is required")
	}

	image := strings.TrimSpace(in.Image)
	in.Environment = strings.ToLower(strings.TrimSpace(in.Environment))

	if in.Environment != "" {
		if t.M.Templates == nil {
			return "", fmt.Errorf("ukc_create: templates cache is not initialized")
		}
		templateID, ok := t.M.Templates.Get(in.Environment)
		if !ok {
			return "", fmt.Errorf("ukc_create: template for environment %q not found. Please run ukc_build_template first to build it.", in.Environment)
		}
		// When booting from a template, the template UUID is passed in place of the image
		image = templateID
	} else if image == "" {
		image = t.M.cfg.DefaultImage
	}

	token, err := RandToken()
	if err != nil {
		return "", fmt.Errorf("ukc_create: token: %w", err)
	}

	t.M.mu.Lock()
	cfg := t.M.cfg
	t.M.mu.Unlock()

	inst, err := CreateInstance(ctx, cfg, in.Name, image, in.MemoryMB, map[string]string{
		"AGENT_TOKEN": token,
	})
	if err != nil {
		return "", err
	}
	if inst.Metro == "" {
		inst.Metro = cfg.Metro
	}
	base := InstanceHTTPSURL(inst)
	if base == "" {
		return "", fmt.Errorf("ukc_create: could not determine instance URL")
	}

	waitCtx, cancel := context.WithTimeout(ctx, cfg.MaxHealthWait)
	defer cancel()
	if err := WaitForHealth(waitCtx, cfg.HTTPClient, base, token, cfg.MaxHealthWait); err != nil {
		_ = DeleteInstance(context.Background(), cfg, inst.UUID)
		return "", fmt.Errorf("ukc_create: instance did not become healthy: %w", err)
	}

	ent := Entry{
		ID:        inst.UUID,
		Name:      inst.Name,
		URL:       base,
		Token:     token,
		CreatedAt: time.Now().UTC().Round(time.Second),
	}
	t.M.mu.Lock()
	t.M.entries[ent.ID] = ent
	err = t.M.persistLocked()
	t.M.mu.Unlock()
	if err != nil {
		return "", err
	}

	return toolutil.Truncate(fmt.Sprintf(
		"instance_id: %s\nurl: %s\nname: %s\ncreated_at: %s\n",
		ent.ID, ent.URL, ent.Name, ent.CreatedAt.Format(time.RFC3339),
	)), nil
}
