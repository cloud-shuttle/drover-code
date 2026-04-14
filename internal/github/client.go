package github

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	githubAPIBase  = "https://api.github.com"
	userAgent      = "drover-code-webhook/1.0"
	confirmedParam = true
)

type Client struct {
	token      string
	httpClient *http.Client
	// apiBaseURL overrides https://api.github.com (for tests with httptest).
	apiBaseURL string
}

func (c *Client) effectiveAPIBase() string {
	if c.apiBaseURL != "" {
		return strings.TrimRight(c.apiBaseURL, "/")
	}
	return githubAPIBase
}

func NewClient(token string) *Client {
	return &Client{
		token: token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) PostIssueComment(ctx context.Context, owner, repo string, number int, body string) (int64, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", c.effectiveAPIBase(), owner, repo, number)
	payload := map[string]string{"body": body}
	var result struct {
		ID int64 `json:"id"`
	}
	if err := c.post(ctx, url, payload, &result); err != nil {
		return 0, fmt.Errorf("post comment: %w", err)
	}
	return result.ID, nil
}

func (c *Client) UpdateComment(ctx context.Context, owner, repo string, commentID int64, body string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/comments/%d", c.effectiveAPIBase(), owner, repo, commentID)
	payload := map[string]string{"body": body}
	return c.patch(ctx, url, payload, nil)
}

func (c *Client) PostReviewComment(ctx context.Context, owner, repo string, prNumber int, body, commitSHA, path string, line int) (int64, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/comments", c.effectiveAPIBase(), owner, repo, prNumber)
	payload := map[string]any{
		"body":      body,
		"commit_id": commitSHA,
		"path":      path,
		"line":      line,
		"side":      "RIGHT",
		"confirmed": confirmedParam,
	}
	var result struct {
		ID int64 `json:"id"`
	}
	if err := c.post(ctx, url, payload, &result); err != nil {
		return 0, fmt.Errorf("post review comment: %w", err)
	}
	return result.ID, nil
}

func (c *Client) CreateReviewWithComments(ctx context.Context, owner, repo string, prNumber int, commitSHA, body string, comments []ReviewComment) error {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/reviews", c.effectiveAPIBase(), owner, repo, prNumber)
	payload := map[string]any{
		"commit_id": commitSHA,
		"body":      body,
		"event":     "COMMENT",
		"comments":  comments,
	}
	return c.post(ctx, url, payload, nil)
}

type ReviewComment struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Side string `json:"side"`
	Body string `json:"body"`
}

func (c *Client) GetPRDiff(ctx context.Context, owner, repo string, prNumber int) (string, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.effectiveAPIBase(), owner, repo, prNumber)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.diff")
	c.setAuthHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get pr diff: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 500*1024))
	return string(data), err
}

func (c *Client) GetPR(ctx context.Context, owner, repo string, prNumber int) (*PullRequest, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.effectiveAPIBase(), owner, repo, prNumber)
	var pr PullRequest
	if err := c.get(ctx, url, &pr); err != nil {
		return nil, fmt.Errorf("get pr: %w", err)
	}
	return &pr, nil
}

func (c *Client) GetIssue(ctx context.Context, owner, repo string, number int) (*Issue, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d", c.effectiveAPIBase(), owner, repo, number)
	var issue Issue
	if err := c.get(ctx, url, &issue); err != nil {
		return nil, fmt.Errorf("get issue: %w", err)
	}
	return &issue, nil
}

func (c *Client) ListIssueComments(ctx context.Context, owner, repo string, number int) ([]Comment, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments?per_page=100", c.effectiveAPIBase(), owner, repo, number)
	var comments []Comment
	if err := c.get(ctx, url, &comments); err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	return comments, nil
}

func VerifySignature(body []byte, signatureHeader, secret string) error {
	if signatureHeader == "" {
		return fmt.Errorf("missing X-Hub-Signature-256 header")
	}
	if !strings.HasPrefix(signatureHeader, "sha256=") {
		return fmt.Errorf("unexpected signature format: %s", signatureHeader)
	}
	gotHex := strings.TrimPrefix(signatureHeader, "sha256=")
	got, err := hex.DecodeString(gotHex)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)
	if !hmac.Equal(got, expected) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

func (c *Client) get(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	c.setAuthHeaders(req)
	return c.doJSON(req, out)
}

func (c *Client) post(ctx context.Context, url string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	c.setAuthHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	return c.doJSON(req, out)
}

func (c *Client) patch(ctx context.Context, url string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	c.setAuthHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	return c.doJSON(req, out)
}

func (c *Client) setAuthHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", userAgent)
}

func (c *Client) doJSON(req *http.Request, out any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, out)
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
