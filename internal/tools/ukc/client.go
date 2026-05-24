package ukc

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cloudshuttle/drover-code/internal/workspace"
)

// Client talks to a worker runtime over the shared worker contract HTTP API.
type Client struct {
	Config  Config
	Inst    *Instance
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New returns a worker runtime client. baseURL is the public HTTPS origin (no trailing path).
func New(baseURL, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    httpClient,
	}
}

// Provision creates a Unikraft Cloud instance for this client to connect to.
func (c *Client) Provision(ctx context.Context, cfg Config, name, img string, memoryMB int, env map[string]string) error {
	c.Config = cfg
	c.Token = cfg.Token
	c.HTTP = cfg.HTTPClient
	if c.HTTP == nil {
		c.HTTP = http.DefaultClient
	}
	inst, err := CreateInstance(ctx, cfg, name, img, memoryMB, env)
	if err != nil {
		return err
	}
	// Fetch the complete Service Group object to get the Domains if they aren't included inline
	if inst.ServiceGroup != nil && inst.ServiceGroup.UUID != "" && len(inst.ServiceGroup.Domains) == 0 {
		sg, err := GetServiceGroup(ctx, cfg, inst.ServiceGroup.UUID)
		if err == nil {
			inst.ServiceGroup = &sg
		} else {
			_ = DeleteInstance(context.Background(), cfg, inst.UUID)
			return fmt.Errorf("failed to get service group: %w", err)
		}
	}
	c.Inst = &inst
	c.BaseURL = InstanceHTTPSURL(inst)
	if c.BaseURL == "" {
		_ = c.Destroy(context.Background())
		return fmt.Errorf("empty instance URL")
	}
	return nil
}

// Destroy deletes the Unikraft Cloud instance if one was provisioned.
func (c *Client) Destroy(ctx context.Context) error {
	if c.Inst != nil && c.Inst.UUID != "" {
		return DeleteInstance(ctx, c.Config, c.Inst.UUID)
	}
	return nil
}

// WaitReady polls GET /health until the worker runtime responds 200 or maxWait elapses.
func (c *Client) WaitReady(ctx context.Context, maxWait time.Duration) error {
	return WaitForHealth(ctx, c.HTTP, c.BaseURL, c.Token, maxWait)
}

// UploadWorkspace sends a tar.gz workspace payload built from localDir.
func (c *Client) UploadWorkspace(ctx context.Context, localDir string, limits workspace.WorkspaceLimits) error {
	return UploadWorkspaceAt(ctx, c.HTTP, c.BaseURL, c.Token, localDir, limits)
}

// Exec runs a shell command via POST /exec and reads the SSE stream until completion.
func (c *Client) Exec(ctx context.Context, command string, onLine func(string)) (output string, exitCode int, err error) {
	jobID, err := PostExecAt(ctx, c.HTTP, c.BaseURL, c.Token, command)
	if err != nil {
		return "", 0, err
	}
	streamURL := ExecStreamURL(c.BaseURL, jobID)
	return ReadExecStream(ctx, c.HTTP, streamURL, c.Token, onLine)
}

// DownloadWorkspace fetches the result payload and extracts it to destDir.
func (c *Client) DownloadWorkspace(ctx context.Context, destDir string) error {
	return DownloadWorkspaceAt(ctx, c.HTTP, c.BaseURL, c.Token, destDir)
}

// ContractSpec describes one worker-contract execution (upload → exec → download).
type ContractSpec struct {
	WorkDir      string
	DownloadDir  string
	Command      string
	Limits       workspace.WorkspaceLimits
	OnStreamLine func(string)
	MaxHealthWait time.Duration
}

// ContractResult is the outcome of a worker contract run.
type ContractResult struct {
	Output   string
	ExitCode int
}

// RunContract executes upload → exec → optional download against the worker runtime.
func RunContract(ctx context.Context, c *Client, spec ContractSpec) (ContractResult, error) {
	if spec.Limits == (workspace.WorkspaceLimits{}) {
		spec.Limits = workspace.DefaultWorkspaceLimits()
	}
	maxWait := spec.MaxHealthWait
	if maxWait == 0 {
		maxWait = 60 * time.Second
	}

	if err := c.WaitReady(ctx, maxWait); err != nil {
		return ContractResult{}, err
	}
	if err := c.UploadWorkspace(ctx, spec.WorkDir, spec.Limits); err != nil {
		return ContractResult{}, fmt.Errorf("upload workspace: %w", err)
	}
	out, code, err := c.Exec(ctx, spec.Command, spec.OnStreamLine)
	if err != nil {
		return ContractResult{Output: out, ExitCode: code}, err
	}
	if code != 0 {
		return ContractResult{Output: out, ExitCode: code}, fmt.Errorf("worker exec exit %d", code)
	}
	if spec.DownloadDir != "" {
		if err := c.DownloadWorkspace(ctx, spec.DownloadDir); err != nil {
			return ContractResult{Output: out, ExitCode: code}, fmt.Errorf("download workspace: %w", err)
		}
	}
	return ContractResult{Output: out, ExitCode: code}, nil
}
