// Package workerclient implements the hosted-worker contract protocol used by
// drover-cloud (ContractRunner) and drover-code (hostedworker.Run) to execute
// agent jobs on a provisioned UKC instance.
//
// The contract sequence is:
//
//  1. WaitForHealth — poll GET /health until the instance is ready.
//  2. UploadWorkspace — stream a tar.gz of the local workspace to POST /workspace.
//  3. RunExec — POST /exec with the agent command; receive a job_id.
//  4. StreamExec — consume the SSE stream at GET /exec/{job_id}/stream until done.
//  5. DownloadWorkspace — fetch the modified workspace from GET /workspace.
//
// All steps respect the provided context for cancellation and deadline propagation.
package workerclient

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cloudshuttle/drover-code/internal/tools/ukc"
)

// Client holds the connection parameters for a single worker instance.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// New returns a Client pointed at a worker instance.
// httpClient may be nil — http.DefaultClient is used in that case.
func New(baseURL, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: httpClient,
	}
}

// ContractSpec configures a single full contract execution.
type ContractSpec struct {
	// WorkDir is the local directory to upload as the initial workspace.
	WorkDir string

	// DownloadDir is where the modified workspace is extracted after execution.
	// If empty, the result workspace is not downloaded.
	DownloadDir string

	// Command is the shell command to run on the worker instance
	// (e.g. "drover-code --headless --prompt 'fix the bug'").
	Command string

	// OnStreamLine is an optional callback invoked for every SSE line received
	// from the exec stream. May be nil.
	OnStreamLine func(string)

	// MaxHealthWait is how long to poll /health before giving up.
	// Defaults to 90 seconds when zero.
	MaxHealthWait time.Duration
}

// ContractResult holds the outcome of a full contract execution.
type ContractResult struct {
	// Output is the accumulated stdout/stderr text from the agent execution stream.
	Output string

	// ExitCode is the exit code reported by the worker's exec stream.
	ExitCode int
}

// RunContract executes the full hosted-worker contract sequence against client.
// It is safe to call concurrently with different clients.
func RunContract(ctx context.Context, client *Client, spec ContractSpec) (ContractResult, error) {
	maxWait := spec.MaxHealthWait
	if maxWait <= 0 {
		maxWait = 90 * time.Second
	}

	// ── 1. Wait for the instance to become healthy ──────────────────────────
	if err := ukc.WaitForHealth(ctx, client.httpClient, client.baseURL, client.token, maxWait); err != nil {
		return ContractResult{}, fmt.Errorf("workerclient: health check: %w", err)
	}

	// ── 2. Upload workspace ─────────────────────────────────────────────────
	if spec.WorkDir != "" {
		if err := ukc.UploadWorkspaceAt(
			ctx, client.httpClient, client.baseURL, client.token,
			spec.WorkDir, ukc.DefaultWorkspaceLimits(),
		); err != nil {
			return ContractResult{}, fmt.Errorf("workerclient: upload workspace: %w", err)
		}
	}

	// ── 3. Submit exec command ──────────────────────────────────────────────
	jobID, err := ukc.PostExecAt(ctx, client.httpClient, client.baseURL, client.token, spec.Command)
	if err != nil {
		return ContractResult{}, fmt.Errorf("workerclient: post exec: %w", err)
	}

	// ── 4. Stream exec output ───────────────────────────────────────────────
	streamURL := ukc.ExecStreamURL(client.baseURL, jobID)
	output, exitCode, err := ukc.ReadExecStream(ctx, client.httpClient, streamURL, client.token, spec.OnStreamLine)
	if err != nil {
		return ContractResult{Output: output, ExitCode: exitCode},
			fmt.Errorf("workerclient: exec stream: %w", err)
	}
	if exitCode != 0 {
		return ContractResult{Output: output, ExitCode: exitCode},
			fmt.Errorf("workerclient: agent exited with code %d", exitCode)
	}

	// ── 5. Download modified workspace ──────────────────────────────────────
	if spec.DownloadDir != "" {
		if err := ukc.DownloadWorkspaceAt(
			ctx, client.httpClient, client.baseURL, client.token, spec.DownloadDir,
		); err != nil {
			return ContractResult{Output: output, ExitCode: exitCode},
				fmt.Errorf("workerclient: download workspace: %w", err)
		}
	}

	return ContractResult{Output: output, ExitCode: exitCode}, nil
}
