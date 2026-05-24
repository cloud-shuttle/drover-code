package ukc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// PostExecAt starts headless execution on a worker runtime and returns the job id.
func PostExecAt(ctx context.Context, client *http.Client, baseURL, token, command string) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	body, err := json.Marshal(map[string]string{"command": command})
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
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("worker exec: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var post struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(respBody, &post); err != nil {
		return "", fmt.Errorf("worker exec: decode: %w", err)
	}
	post.JobID = strings.TrimSpace(post.JobID)
	if post.JobID == "" {
		return "", fmt.Errorf("worker exec: missing job_id")
	}
	return post.JobID, nil
}

// ExecStreamURL returns the SSE stream URL for a worker exec job.
func ExecStreamURL(baseURL, jobID string) string {
	return strings.TrimRight(baseURL, "/") + "/exec/" + jobID + "/stream"
}
