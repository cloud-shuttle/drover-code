// internal/tools/git/create_pr.go
package git

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/tools/quality"
	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
	"github.com/google/go-github/v62/github"
)

type CreatePR struct {
	WorkDir string
	review  *quality.Review
	client  *github.Client
}

type prInput struct {
	Title       string   `json:"title"`
	Body        string   `json:"body,omitempty"`
	Base        string   `json:"base,omitempty"`     // default: main/master
	Head        string   `json:"head,omitempty"`     // branch to merge from
	Draft       bool     `json:"draft,omitempty"`
	AutoReview  bool     `json:"auto_review,omitempty"` // default: true
	ExtraChecks []string `json:"extra_checks,omitempty"`
}

func NewCreatePRTool(workDir string) *CreatePR {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}

	var client *github.Client
	if token != "" {
		client = github.NewClient(nil).WithAuthToken(token)
	}

	return &CreatePR{
		WorkDir: workDir,
		review:  &quality.Review{WorkDir: workDir},
		client:  client,
	}
}

func (t *CreatePR) Name() string { return "create_pr" }

func (t *CreatePR) Description() string {
	return `Create a GitHub Pull Request.
Safely handles branch creation, quality review, and PR creation.
Highly recommended for all non-trivial changes.`
}

func (t *CreatePR) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("title", toolutil.NewSchema("string").Desc("PR title")).
		Prop("body", toolutil.NewSchema("string").Desc("Detailed PR description")).
		Prop("base", toolutil.NewSchema("string").Desc("Target branch (default: main)")).
		Prop("head", toolutil.NewSchema("string").Desc("Source branch")).
		Prop("draft", toolutil.NewSchema("boolean").Desc("Create as draft PR")).
		Prop("auto_review", toolutil.NewSchema("boolean").Desc("Run review before creating PR (default: true)")).
		Required("title").
		Build()
}

func (t *CreatePR) NeedsPermission(rawInput json.RawMessage) bool { return true }

func (t *CreatePR) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
	if t.client == nil {
		return "", fmt.Errorf("GITHUB_TOKEN or GH_TOKEN environment variable is required for create_pr")
	}

	var inp prInput
	if err := json.Unmarshal(rawInput, &inp); err != nil {
		return "", err
	}
	if inp.Title == "" {
		return "", fmt.Errorf("title is required")
	}

	var report strings.Builder
	report.WriteString("=== CREATE PULL REQUEST ===\n\n")

	// === 1. Quality Review ===
	if !strings.Contains(string(rawInput), `"auto_review": false`) {
		inp.AutoReview = true
	}
	if inp.AutoReview {
		report.WriteString("Running pre-PR quality review...\n\n")

		reviewRaw, _ := json.Marshal(map[string]interface{}{"commands": inp.ExtraChecks})
		reviewOut, err := t.review.Execute(ctx, reviewRaw)

		report.WriteString(reviewOut + "\n")
		if err != nil || strings.Contains(reviewOut, "FAILED") {
			return report.String(), fmt.Errorf("PR creation blocked: quality review failed")
		}
	}

	// === 2. Get repo info ===
	owner, repo, err := t.getRepoInfo(ctx)
	if err != nil {
		return "", err
	}

	// === 3. Determine branches ===
	base := inp.Base
	if base == "" {
		base = t.getDefaultBranch(ctx)
	}
	head := inp.Head
	if head == "" {
		head = t.getCurrentBranch(ctx)
	}

	// === 4. Create PR ===
	pr := &github.NewPullRequest{
		Title: github.String(inp.Title),
		Body:  github.String(inp.Body),
		Base:  github.String(base),
		Head:  github.String(head),
		Draft: github.Bool(inp.Draft),
	}

	createdPR, _, err := t.client.PullRequests.Create(ctx, owner, repo, pr)
	if err != nil {
		return "", fmt.Errorf("failed to create PR: %w", err)
	}

	report.WriteString(fmt.Sprintf("✅ Pull Request created successfully!\n"))
	report.WriteString(fmt.Sprintf("Title: %s\n", inp.Title))
	report.WriteString(fmt.Sprintf("URL: %s\n", createdPR.GetHTMLURL()))
	report.WriteString(fmt.Sprintf("From: %s → %s\n", head, base))

	if inp.Draft {
		report.WriteString("Status: Draft PR\n")
	}

	return report.String(), nil
}

// Helper methods
func (t *CreatePR) getRepoInfo(ctx context.Context) (owner, repo string, err error) {
	// Simple remote parsing - improve if needed
	out, err := runGit(ctx, t.WorkDir, "remote", "get-url", "origin")
	if err != nil {
		return "", "", err
	}
	// Parse github.com/owner/repo.git or git@github.com:owner/repo.git
	remote := strings.TrimSpace(out)
	
	// Handle git@github.com:owner/repo.git
	if strings.HasPrefix(remote, "git@") {
		parts := strings.Split(remote, ":")
		if len(parts) == 2 {
			repoParts := strings.Split(parts[1], "/")
			if len(repoParts) == 2 {
				owner = repoParts[0]
				repo = strings.TrimSuffix(repoParts[1], ".git")
				return owner, repo, nil
			}
		}
	}
	
	// Handle https://github.com/owner/repo.git
	parts := strings.Split(remote, "/")
	if len(parts) >= 2 {
		repo = strings.TrimSuffix(parts[len(parts)-1], ".git")
		owner = parts[len(parts)-2]
	}
	return owner, repo, nil
}

func (t *CreatePR) getCurrentBranch(ctx context.Context) string {
	out, _ := runGit(ctx, t.WorkDir, "branch", "--show-current")
	return strings.TrimSpace(out)
}

func (t *CreatePR) getDefaultBranch(ctx context.Context) string {
	out, _ := runGit(ctx, t.WorkDir, "symbolic-ref", "refs/remotes/origin/HEAD")
	if out != "" {
		return strings.TrimPrefix(strings.TrimSpace(out), "refs/remotes/origin/")
	}
	return "main"
}
