package client

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cloudshuttle/drover-code/internal/tools/ukc"
	"github.com/cloudshuttle/drover-code/pkg/workercontract/workspace"
)

// JobSpec describes a full worker contract execution lifecycle.
type JobSpec struct {
	Name         string
	Image        string
	MemoryMB     int
	Env          map[string]string
	Command      string
	WorkDir      string
	DownloadDir  string
	Limits       workspace.Limits
	OnEvent      func(string) // Optional: for logging
	OnStreamLine func(string) // Optional: for SSE stream lines
}

// JobResult is the outcome of a worker contract run.
type JobResult struct {
	Output   string
	ExitCode int
}

// RunAgentJob provisions a UKC instance, uploads the workspace, executes the command,
// downloads the result, and guarantees destruction of the instance.
func RunAgentJob(ctx context.Context, cfg ukc.Config, spec JobSpec) (JobResult, error) {
	if spec.Limits == (workspace.Limits{}) {
		spec.Limits = workspace.DefaultLimits()
	}

	if spec.OnEvent != nil {
		spec.OnEvent(fmt.Sprintf("Provisioning UKC instance %s...", spec.Name))
	}

	// Make sure we have a token
	token := spec.Env["AGENT_TOKEN"]
	if token == "" {
		token, _ = ukc.RandToken()
		if spec.Env == nil {
			spec.Env = make(map[string]string)
		}
		spec.Env["AGENT_TOKEN"] = token
	}

	inst, err := ukc.CreateInstance(ctx, cfg, spec.Name, spec.Image, spec.MemoryMB, spec.Env)
	if err != nil {
		return JobResult{}, fmt.Errorf("create instance: %w", err)
	}

	// Fetch the complete Service Group object to get the Domains if they aren't included inline
	if inst.ServiceGroup != nil && inst.ServiceGroup.UUID != "" && len(inst.ServiceGroup.Domains) == 0 {
		sg, err := ukc.GetServiceGroup(ctx, cfg, inst.ServiceGroup.UUID)
		if err == nil {
			inst.ServiceGroup = &sg
		}
	}

	instURL := ukc.InstanceHTTPSURL(inst)
	if instURL == "" {
		_ = ukc.DeleteInstance(context.Background(), cfg, inst.UUID)
		return JobResult{}, fmt.Errorf("empty instance URL for instance %s", inst.Name)
	}

	if spec.OnEvent != nil {
		spec.OnEvent(fmt.Sprintf("Instance created, waiting for health check at %s...", instURL))
	}

	defer func() {
		_ = ukc.UnregisterActiveJob(inst.UUID)
		if err := ukc.DeleteInstance(context.Background(), cfg, inst.UUID); err != nil {
			if spec.OnEvent != nil {
				spec.OnEvent(fmt.Sprintf("⚠️ Failed to destroy UKC instance %s: %v", spec.Name, err))
			}
		} else {
			if spec.OnEvent != nil {
				spec.OnEvent(fmt.Sprintf("Destroyed UKC instance %s", spec.Name))
			}
		}
	}()
	_ = ukc.RegisterActiveJob(inst.UUID, spec.Name)

	c := New(instURL, token, cfg.HTTPClient)

	maxWait := cfg.MaxHealthWait
	if maxWait == 0 {
		maxWait = 60 * time.Second
	}

	if err := c.WaitReady(ctx, maxWait); err != nil {
		return JobResult{}, fmt.Errorf("wait ready: %w", err)
	}

	if spec.OnEvent != nil {
		spec.OnEvent("Uploading local workspace to cloud instance...")
	}

	summary, err := workspace.PlanUpload(spec.WorkDir, spec.Limits)
	if err != nil {
		return JobResult{}, fmt.Errorf("workspace plan: %w", err)
	}
	if err := ukc.MaybeConfirmUpload(os.Stdin, os.Stdout, ukc.UploadSummary{
		FileCount:  summary.FileCount,
		TotalBytes: summary.TotalBytes,
	}); err != nil {
		return JobResult{}, err
	}

	if err := c.UploadWorkspace(ctx, spec.WorkDir, spec.Limits); err != nil {
		return JobResult{}, fmt.Errorf("upload workspace: %w", err)
	}

	if spec.OnEvent != nil {
		spec.OnEvent("Instance ready, executing headless task...")
	}

	outStr, exitCode, err := c.Exec(ctx, spec.Command, spec.OnStreamLine)
	if err != nil {
		return JobResult{Output: outStr, ExitCode: exitCode}, fmt.Errorf("worker exec: %w", err)
	}
	
	if spec.DownloadDir != "" {
		if spec.OnEvent != nil {
			spec.OnEvent("Task complete. Downloading modified workspace...")
		}
		if err := c.DownloadWorkspace(ctx, spec.DownloadDir); err != nil {
			return JobResult{Output: outStr, ExitCode: exitCode}, fmt.Errorf("download workspace: %w", err)
		}
	}

	return JobResult{
		Output:   outStr,
		ExitCode: exitCode,
	}, nil
}
