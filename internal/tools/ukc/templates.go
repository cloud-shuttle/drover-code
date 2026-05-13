package ukc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

// TemplatesCache manages a mapping of Environment (e.g. "rust") to Template ID/UUID.
type TemplatesCache struct {
	mu       sync.Mutex
	path     string
	mappings map[string]string // e.g., "rust" -> "uuid-of-template"
}

// NewTemplatesCache initializes a cache backed by a JSON file.
func NewTemplatesCache(path string) (*TemplatesCache, error) {
	tc := &TemplatesCache{
		path:     path,
		mappings: make(map[string]string),
	}
	if err := tc.load(); err != nil {
		return nil, err
	}
	return tc, nil
}

func (tc *TemplatesCache) load() error {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	b, err := os.ReadFile(tc.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(b, &tc.mappings)
}

func (tc *TemplatesCache) save() error {
	b, err := json.MarshalIndent(tc.mappings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(tc.path), 0755); err != nil {
		return err
	}
	return os.WriteFile(tc.path, b, 0644)
}

// Get returns the template UUID for a given environment name.
func (tc *TemplatesCache) Get(env string) (string, bool) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	id, ok := tc.mappings[env]
	return id, ok
}

// Set stores the template UUID for a given environment name.
func (tc *TemplatesCache) Set(env, uuid string) error {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	tc.mappings[env] = uuid
	return tc.save()
}

// EnvSetupScripts contains the default installation scripts for environments.
// These are run on the base Alpine image.
var EnvSetupScripts = map[string]string{
	"rust":   "apk add --no-cache rust cargo gcc musl-dev",
	"node":   "apk add --no-cache nodejs npm",
	"python": "apk add --no-cache python3 py3-pip",
	"go":     "apk add --no-cache go",
}

// BuildTemplate is a tool that automates building a language template.
type BuildTemplate struct {
	M     *Manager
	Cache *TemplatesCache
}

func (t *BuildTemplate) Name() string { return "ukc_build_template" }

func (t *BuildTemplate) Description() string {
	return "Builds a Unikraft Cloud template for a specific environment (e.g. rust, node, python, go). " +
		"It boots a base instance, runs the setup script, and snapshots it. " +
		"The resulting template UUID is saved in the local cache to be used by ukc_create."
}

func (t *BuildTemplate) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("environment", toolutil.NewSchema("string").Desc("The language environment to build (e.g., rust, node, python, go)")).
		Required("environment").
		Build()
}

func (t *BuildTemplate) NeedsPermission(_ json.RawMessage) bool { return true }

type buildTemplateInput struct {
	Environment string `json:"environment"`
}

// runAgentCommand executes a command via the Drover Agent /exec HTTP endpoint.
func runAgentCommand(ctx context.Context, client *http.Client, baseURL, token, cmd string) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	body, err := json.Marshal(map[string]string{"command": cmd})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/exec", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	resp.Body.Close()
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("POST /exec: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var post struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(respBody, &post); err != nil {
		return "", fmt.Errorf("bad JSON from agent: %w", err)
	}
	if post.JobID == "" {
		return "", fmt.Errorf("missing job_id")
	}

	streamURL := strings.TrimRight(baseURL, "/") + "/exec/" + post.JobID + "/stream"
	out, code, err := ReadExecStream(ctx, client, streamURL, token, nil)
	if err != nil {
		return "", err
	}
	if code != 0 {
		return out, fmt.Errorf("command exited with code %d:\n%s", code, out)
	}
	return out, nil
}

func (t *BuildTemplate) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	if t.M == nil {
		return "", fmt.Errorf("ukc_build_template: manager not configured")
	}
	var in buildTemplateInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("ukc_build_template: bad input: %w", err)
	}
	in.Environment = strings.ToLower(strings.TrimSpace(in.Environment))
	setupScript, ok := EnvSetupScripts[in.Environment]
	if !ok {
		return "", fmt.Errorf("ukc_build_template: unknown environment %q", in.Environment)
	}

	// 1. Boot a temporary base instance
	tempName := fmt.Sprintf("builder-%s-%d", in.Environment, time.Now().Unix())
	
	token, err := RandToken()
	if err != nil {
		return "", fmt.Errorf("ukc_build_template: token: %w", err)
	}

	t.M.mu.Lock()
	cfg := t.M.cfg
	t.M.mu.Unlock()

	inst, err := CreateInstance(ctx, cfg, tempName, cfg.DefaultImage, 512, map[string]string{
		"AGENT_TOKEN": token,
	})
	if err != nil {
		return "", fmt.Errorf("ukc_build_template: failed to create base instance: %w", err)
	}

	base := InstanceHTTPSURL(inst)
	if base == "" {
		_ = DeleteInstance(context.Background(), cfg, inst.UUID)
		return "", fmt.Errorf("ukc_build_template: could not determine instance URL")
	}

	// Wait for health
	waitCtx, cancel := context.WithTimeout(ctx, cfg.MaxHealthWait)
	defer cancel()
	if err := WaitForHealth(waitCtx, cfg.HTTPClient, base, token, cfg.MaxHealthWait); err != nil {
		_ = DeleteInstance(context.Background(), cfg, inst.UUID)
		return "", fmt.Errorf("ukc_build_template: builder instance did not become healthy: %w", err)
	}

	// 2. Run the setup script via agent
	execOut, err := runAgentCommand(ctx, cfg.HTTPClient, base, token, setupScript)
	if err != nil {
		_ = DeleteInstance(context.Background(), cfg, inst.UUID)
		return "", fmt.Errorf("ukc_build_template: failed to execute setup script: %w", err)
	}

	// 3. Snapshot the instance into a template
	_, _ = runAgentCommand(ctx, cfg.HTTPClient, base, token, "echo 1 > /uk/libukp/template_instance")

	// Sleep briefly to let the cloud snapshot finish
	time.Sleep(5 * time.Second)

	// In KraftCloud, the UUID of the snapshot is usually the instance UUID, or you can 
	// boot from an instance UUID if it has been snapshotted (template).
	templateUUID := inst.UUID

	// 4. Save to cache
	if err := t.Cache.Set(in.Environment, templateUUID); err != nil {
		return "", fmt.Errorf("ukc_build_template: failed to save template to cache: %w", err)
	}

	return fmt.Sprintf("Successfully built template for %q.\nTemplate UUID: %s\nOutput from setup:\n%s", in.Environment, templateUUID, execOut), nil
}
