// Package hostedworker runs the shared worker contract for hosted execution clients.
package hostedworker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cloudshuttle/drover-code/internal/tools/ukc"
	"github.com/cloudshuttle/drover-code/internal/workspace"
)

// RunInput configures one hosted worker contract execution.
type RunInput struct {
	WorkDir       string
	DownloadDir   string
	Prompt        string
	InstanceName  string
	Token         string
	Metro         string
	DefaultImage  string
	ExtraEnv      map[string]string
	OnStreamLine  func(string)
}

// NOTE (Option B - Warden):
// drover-code ships a default set of safe Beads (in beads/policies.jsonl) that are
// auto-discovered for the "unikernel" preset (and dev runs) when DROVER_WARDEN_BEADS_DIR
// is unset. See internal/warden/warden.go:resolveDefaultBeadsDir and the Dockerfile COPY.
// Cloud/UKC can still override for tenant-specific policies by:
//   1. Setting DROVER_WARDEN_BEADS_DIR=/warden/beads (or similar) in ExtraEnv.
//   2. Mounting the tenant/customer Beads (policies.jsonl + evals.jsonl) into the container
//      at that path (read-only volume recommended).
// The ukc-agent and drover-code entrypoints call warden.Init() which respects this env var.
// Input/Action/Output guards + unified participation in permissions.Engine are active.
// This gives semantic safety on tool args + LLM I/O for hosted jobs (earlier/lower-risk gate
// than the full Muster + Guard ReBAC binding path).


// RunOutput is the result of a hosted worker contract run.
type RunOutput struct {
	Output string
}

// Run provisions a worker instance, executes the worker contract, and downloads results.
func Run(ctx context.Context, in RunInput) (RunOutput, error) {
	mgr, err := managerForRun(in)
	if err != nil {
		return RunOutput{}, err
	}
	cfg := mgr.Config()

	token, err := ukc.RandToken()
	if err != nil {
		return RunOutput{}, err
	}
	name := strings.TrimSpace(in.InstanceName)
	if name == "" {
		name = fmt.Sprintf("drover-hosted-%d", time.Now().Unix())
	}

	env := map[string]string{
		"AGENT_TOKEN":                   token,
		"DROVER_CODE_HEADLESS":          "1",
		"DROVER_CODE_PERMISSION_PRESET": "unikernel",
	}
	for k, v := range in.ExtraEnv {
		if strings.TrimSpace(k) != "" && v != "" {
			env[k] = v
		}
	}
	if _, hasLLM := env["ANTHROPIC_AUTH_TOKEN"]; !hasLLM {
		if _, hasKey := env["ANTHROPIC_API_KEY"]; !hasKey {
			for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "ANTHROPIC_MODEL", "OPENAI_MODEL", "GEMINI_MODEL"} {
				if v := os.Getenv(k); v != "" {
					env[k] = v
				}
			}
		}
	}

	inst, err := ukc.CreateInstance(ctx, cfg, name, cfg.DefaultImage, 512, env)
	if err != nil {
		return RunOutput{}, err
	}

	// Record instance creation for lifecycle / cost tracking (structured for downstream tools)
	startedAt := time.Now().UTC().Format(time.RFC3339)
	_ = emitIfPossible(in.OnStreamLine, "status", fmt.Sprintf(`{"event":"ukc_instance_lifecycle","phase":"created","uuid":"%s","started_at":"%s"}`, inst.UUID, startedAt))

	// Robust cleanup: always attempt to delete the instance, with retries on best-effort background context.
	// This is critical for cost control and avoiding orphan instances on job failure/timeout/cancellation.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = cleanupInstance(cleanupCtx, cfg, inst.UUID)
	}()

	if inst.ServiceGroup != nil && inst.ServiceGroup.UUID != "" && len(inst.ServiceGroup.Domains) == 0 {
		if sg, err := ukc.GetServiceGroup(ctx, cfg, inst.ServiceGroup.UUID); err == nil {
			inst.ServiceGroup = &sg
		}
	}
	baseURL := ukc.InstanceHTTPSURL(inst)
	if baseURL == "" {
		return RunOutput{}, fmt.Errorf("worker instance has no public URL")
	}

	if in.DownloadDir != "" {
		_ = os.RemoveAll(in.DownloadDir)
	}

	client := ukc.New(baseURL, token, cfg.HTTPClient)
	safePrompt := strings.ReplaceAll(in.Prompt, "'", "'\\''")
	command := fmt.Sprintf("drover-code --headless --prompt '%s'", safePrompt)

	result, err := ukc.RunContract(ctx, client, ukc.ContractSpec{
		WorkDir:       in.WorkDir,
		DownloadDir:   in.DownloadDir,
		Command:       command,
		Limits:        workspace.DefaultWorkspaceLimits(),
		OnStreamLine:  in.OnStreamLine,
		MaxHealthWait: 90 * time.Second,
	})
	if err != nil {
		_ = emitIfPossible(in.OnStreamLine, "status", fmt.Sprintf(`{"event":"ukc_instance_lifecycle","phase":"destroyed","uuid":"%s","ended_at":"%s","reason":"error"}`, inst.UUID, time.Now().UTC().Format(time.RFC3339)))
		return RunOutput{Output: result.Output}, err
	}

	_ = emitIfPossible(in.OnStreamLine, "status", fmt.Sprintf(`{"event":"ukc_instance_lifecycle","phase":"destroyed","uuid":"%s","ended_at":"%s"}`, inst.UUID, time.Now().UTC().Format(time.RFC3339)))
	return RunOutput{Output: result.Output}, nil
}

func managerForRun(in RunInput) (*ukc.Manager, error) {
	token := strings.TrimSpace(in.Token)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("UKC_TOKEN"))
	}
	if token == "" {
		return nil, fmt.Errorf("UKC_TOKEN not configured")
	}
	metro := strings.TrimSpace(in.Metro)
	if metro == "" {
		metro = ukc.MetroFromEnv()
	}
	image := strings.TrimSpace(in.DefaultImage)
	if image == "" {
		image = strings.TrimSpace(os.Getenv("UKC_DEFAULT_AGENT_IMAGE"))
	}
	mgr, err := ukc.NewManagerWithCredentials(token, metro, image)
	if err != nil {
		return nil, err
	}
	return mgr, nil
}

// cleanupInstance performs a best-effort delete with retries. If normal delete
// persistently fails (stubborn UKC instance), it attempts a force StopInstance
// followed by another delete round. This keeps cost control tight and prevents
// orphan billing instances.
func cleanupInstance(ctx context.Context, cfg ukc.Config, uuid string) error {
	const maxAttempts = 3

	// inner helper for delete retries
	tryDelete := func() error {
		var lastErr error
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := ukc.DeleteInstance(ctx, cfg, uuid); err == nil {
				return nil
			} else {
				lastErr = err
			}
			select {
			case <-time.After(time.Duration(attempt) * 600 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return lastErr
	}

	if err := tryDelete(); err == nil {
		return nil
	}

	// Normal delete hung or failed — attempt force stop (power off) then delete again.
	// Stop is best-effort; even if it errors we still retry delete (instance may already be gone).
	_ = ukc.StopInstance(ctx, cfg, uuid)

	// brief settle time for stop to take effect on platform
	select {
	case <-time.After(1200 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}

	return tryDelete()
}

// emitIfPossible safely calls the optional OnStreamLine callback.
func emitIfPossible(onStream func(string), eventType, payload string) error {
	if onStream == nil {
		return nil
	}
	// Reuse the same JSON shape the rest of the system uses
	line := fmt.Sprintf(`{"stream":"%s","line":%q}`, eventType, payload)
	onStream(line)
	return nil
}
