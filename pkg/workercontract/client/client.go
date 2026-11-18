package client

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/cloudshuttle/drover-code/internal/tools/ukc"
	"github.com/cloudshuttle/drover-code/pkg/workercontract/workspace"
)

// Client talks to a worker runtime over the shared worker contract HTTP API.
type Client struct {
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

// WaitReady polls GET /health until the worker runtime responds 200 or maxWait elapses.
func (c *Client) WaitReady(ctx context.Context, maxWait time.Duration) error {
	return ukc.WaitForHealth(ctx, c.HTTP, c.BaseURL, c.Token, maxWait)
}

// UploadWorkspace sends a tar.gz workspace payload built from localDir.
func (c *Client) UploadWorkspace(ctx context.Context, localDir string, limits workspace.Limits) error {
	return ukc.UploadWorkspaceAt(ctx, c.HTTP, c.BaseURL, c.Token, localDir, limits)
}

// Exec runs a shell command via POST /exec and reads the SSE stream until completion.
func (c *Client) Exec(ctx context.Context, command string, onLine func(string)) (output string, exitCode int, err error) {
	jobID, err := ukc.PostExecAt(ctx, c.HTTP, c.BaseURL, c.Token, command)
	if err != nil {
		return "", 0, err
	}
	streamURL := ukc.ExecStreamURL(c.BaseURL, jobID)
	return ukc.ReadExecStream(ctx, c.HTTP, streamURL, c.Token, onLine)
}

// DownloadWorkspace fetches the result payload and extracts it to destDir.
func (c *Client) DownloadWorkspace(ctx context.Context, destDir string) error {
	return ukc.DownloadWorkspaceAt(ctx, c.HTTP, c.BaseURL, c.Token, destDir)
}
