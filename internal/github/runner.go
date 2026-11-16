package github

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/api"
	"github.com/cloudshuttle/drover-code/internal/config"
	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/internal/permissions"
	"github.com/cloudshuttle/drover-code/internal/tools"
)

type Runner struct {
	ghClient  *Client
	apiClient *api.Client
	workBase  string
}

func NewRunner(ghClient *Client, apiClient *api.Client, workBase string) *Runner {
	return &Runner{
		ghClient:  ghClient,
		apiClient: apiClient,
		workBase:  workBase,
	}
}

func (r *Runner) Handle(ctx context.Context, trigger *Trigger) error {
	target := trigger.ReplyTarget

	placeholder := fmt.Sprintf("_Processing: %s…_", truncate(trigger.Request, 80))
	commentID, err := r.ghClient.PostIssueComment(
		ctx, target.Owner, target.Repo, target.Number, placeholder,
	)
	if err != nil {
		return fmt.Errorf("github runner: placeholder: %w", err)
	}

	response, runErr := r.run(ctx, trigger)
	if runErr != nil {
		response = fmt.Sprintf("❌ An error occurred:\n\n```\n%s\n```", runErr.Error())
	}

	if updateErr := r.ghClient.UpdateComment(
		ctx, target.Owner, target.Repo, commentID, response,
	); updateErr != nil {
		return fmt.Errorf("github runner: update: %w", updateErr)
	}
	return runErr
}

func (r *Runner) run(ctx context.Context, trigger *Trigger) (string, error) {
	repoDir, cleanup, err := r.cloneRepo(ctx, trigger)
	if err != nil {
		return "", fmt.Errorf("clone: %w", err)
	}
	defer cleanup()

	sysPrompt := buildGitHubSystemPrompt(trigger, repoDir)
	mgr := convo.NewManagerWithSystem(sysPrompt)

	cfgLoader := config.NewLoader(repoDir)
	if err := cfgLoader.Load(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: config: %v\n", err)
	}
	repoSettings := cfgLoader.Get()
	config.ApplyConvoHeuristics(mgr, repoSettings)

	registry := tools.NewRegistry()
	tools.RegisterAll(registry, repoDir)

	eventCh := make(chan agent.Event, 256)
	var output strings.Builder
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for ev := range eventCh {
			if td, ok := ev.(agent.TextDeltaEvent); ok {
				output.WriteString(td.Text)
			}
		}
	}()

	eng := permissions.NewEngine(
		permissions.ModeBypass,
		nil,
		nil,
		filepath.Join(repoDir, ".claude", "permissions.json"),
		tools.AllowAll,
	)
	driver := agent.NewAnthropicInferenceDriver(r.apiClient)
	executor := agent.NewDefaultToolExecutor(registry, eng, eventCh)
	loop := agent.NewLoop(driver, mgr, executor, registry, eventCh)
	config.ApplyAgentLoopOptions(loop, repoSettings)
	runErr := loop.Run(ctx, trigger.Request)
	close(eventCh)
	<-drained

	if runErr != nil {
		return "", runErr
	}

	resp := strings.TrimSpace(output.String())
	if resp == "" {
		resp = "_No response generated._"
	}
	return resp + "\n\n---\n_via drover-code_", nil
}

func (r *Runner) cloneRepo(ctx context.Context, trigger *Trigger) (string, func(), error) {
	ref := trigger.Context.PRHead
	if ref == "" {
		ref = trigger.Context.Repo.DefaultBranch
	}
	if ref == "" {
		ref = "main"
	}

	jobDir := filepath.Join(r.workBase,
		fmt.Sprintf("job-%s-%d",
			strings.NewReplacer("/", "-", ".", "-").Replace(trigger.Context.Repo.FullName),
			trigger.ReplyTarget.Number,
		),
	)
	cleanup := func() { _ = os.RemoveAll(jobDir) }

	cloneURL := trigger.Context.Repo.CloneURL
	if cloneURL == "" {
		cloneURL = fmt.Sprintf("https://github.com/%s.git", trigger.Context.Repo.FullName)
	}
	if r.ghClient.token != "" {
		cloneURL = strings.Replace(cloneURL,
			"https://github.com/",
			fmt.Sprintf("https://x-access-token:%s@github.com/", r.ghClient.token),
			1,
		)
	}

	if err := execGit(ctx, "", "clone", "--depth=1",
		"--branch="+ref, cloneURL, jobDir); err != nil {
		cleanup()
		return "", nil, err
	}
	return jobDir, cleanup, nil
}

func execGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("git %s: %s", args[0], msg)
	}
	return nil
}

func buildGitHubSystemPrompt(trigger *Trigger, repoDir string) string {
	ctx := trigger.Context
	var b strings.Builder

	fmt.Fprintf(&b, "You are an AI assistant helping with a GitHub repository.\n")
	fmt.Fprintf(&b, "Repository: %s\nWorking directory: %s\n\n", ctx.Repo.FullName, repoDir)

	if ctx.PRNumber > 0 {
		fmt.Fprintf(&b, "Pull Request #%d: %s\n", ctx.PRNumber, ctx.IssuTitle)
		if ctx.PRHead != "" {
			fmt.Fprintf(&b, "Branch: %s → %s\n", ctx.PRHead, ctx.PRBase)
		}
		b.WriteString("\n")
	} else if ctx.IssueNumber > 0 {
		fmt.Fprintf(&b, "Issue #%d: %s\n\n", ctx.IssueNumber, ctx.IssuTitle)
	}

	if ctx.IssueBody != "" {
		fmt.Fprintf(&b, "Description:\n%s\n\n", truncate(ctx.IssueBody, 1000))
	}

	if ctx.DiffContext != "" {
		fmt.Fprintf(&b, "Comment on %s (line %d):\n```diff\n%s\n```\n\n",
			ctx.FilePath, ctx.DiffLine, ctx.DiffContext)
	}

	b.WriteString("Tools available: read files, search code, run bash commands, git operations.\n")
	b.WriteString("Do not commit or push unless explicitly asked.\n")
	b.WriteString("Respond in GitHub Flavored Markdown. Be concise — this appears as a comment.\n")
	b.WriteString("Do not mention that you are an AI or include AI attribution footers.\n")

	return b.String()
}

func truncate(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}
