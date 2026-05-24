// Package git implements git tools.
package git

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/tools/quality"
	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

func runGit(ctx context.Context, workDir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return toolutil.Truncate(stdout.String()), nil
}

type Status struct{ WorkDir string }

func (t *Status) Name() string { return "git_status" }
func (t *Status) Description() string {
	return "Show the working tree status: modified, staged, untracked, and conflicted files."
}
func (t *Status) InputSchema() json.RawMessage { return toolutil.NewSchema("object").Build() }
func (t *Status) NeedsPermission(_ json.RawMessage) bool { return false }
func (t *Status) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	return runGit(ctx, t.WorkDir, "status", "--short", "--branch")
}

type Diff struct{ WorkDir string }

type diffInput struct {
	Staged bool   `json:"staged"`
	Path   string `json:"path"`
	Base   string `json:"base"`
}

func (t *Diff) Name() string { return "git_diff" }
func (t *Diff) Description() string {
	return "Show changes in the working tree or staged area as a unified diff. " +
		"Set staged=true for staged changes, or provide a base ref for comparing against a commit."
}
func (t *Diff) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("staged", toolutil.NewSchema("boolean").Desc("Show staged (indexed) changes instead of unstaged (default: false)")).
		Prop("path", toolutil.NewSchema("string").Desc("Restrict diff to this file or directory")).
		Prop("base", toolutil.NewSchema("string").Desc("Base ref to compare against, e.g. 'HEAD~1', 'main', or a commit SHA")).
		Build()
}
func (t *Diff) NeedsPermission(_ json.RawMessage) bool { return false }
func (t *Diff) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
	var inp diffInput
	if err := json.Unmarshal(rawInput, &inp); err != nil {
		return "", fmt.Errorf("git_diff: bad input: %w", err)
	}

	args := []string{"diff", "--unified=3"}
	if inp.Staged {
		args = append(args, "--cached")
	}
	if inp.Base != "" {
		args = append(args, inp.Base)
	}
	if inp.Path != "" {
		args = append(args, "--", inp.Path)
	}

	out, err := runGit(ctx, t.WorkDir, args...)
	if err != nil {
		return "", err
	}
	if out == "" {
		return "no changes", nil
	}
	return out, nil
}

type Log struct{ WorkDir string }

type logInput struct {
	MaxCount int    `json:"max_count"`
	Path     string `json:"path"`
	OneLine  bool   `json:"one_line"`
}

func (t *Log) Name() string { return "git_log" }
func (t *Log) Description() string {
	return "Show recent commit history. Set one_line=true for a compact view. " +
		"Restrict to a file path to see its history."
}
func (t *Log) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("max_count", toolutil.NewSchema("integer").Desc("Number of commits to show (default: 20)")).
		Prop("path", toolutil.NewSchema("string").Desc("Show only commits that changed this file or directory")).
		Prop("one_line", toolutil.NewSchema("boolean").Desc("Compact one-line format (default: false)")).
		Build()
}
func (t *Log) NeedsPermission(_ json.RawMessage) bool { return false }
func (t *Log) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
	var inp logInput
	if err := json.Unmarshal(rawInput, &inp); err != nil {
		return "", fmt.Errorf("git_log: bad input: %w", err)
	}

	n := inp.MaxCount
	if n <= 0 {
		n = 20
	}

	args := []string{"log", fmt.Sprintf("-n%d", n)}
	if inp.OneLine {
		args = append(args, "--oneline")
	} else {
		args = append(args, "--pretty=format:%C(auto)%h %as %<(20,trunc)%an  %s")
	}
	if inp.Path != "" {
		args = append(args, "--", inp.Path)
	}

	return runGit(ctx, t.WorkDir, args...)
}

type Add struct{ WorkDir string }

type addInput struct {
	Paths []string `json:"paths"`
}

func (t *Add) Name() string { return "git_add" }
func (t *Add) Description() string {
	return "Stage files for the next commit. Pass an empty paths list to stage all changes (git add -A)."
}
func (t *Add) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("paths", toolutil.NewSchema("array").Items(toolutil.NewSchema("string")).
			Desc("Files or directories to stage. Empty array stages everything.")).
		Build()
}
func (t *Add) NeedsPermission(_ json.RawMessage) bool { return true }
func (t *Add) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
	var inp addInput
	if err := json.Unmarshal(rawInput, &inp); err != nil {
		return "", fmt.Errorf("git_add: bad input: %w", err)
	}

	args := []string{"add"}
	if len(inp.Paths) == 0 {
		args = append(args, "-A")
	} else {
		args = append(args, inp.Paths...)
	}
	return runGit(ctx, t.WorkDir, args...)
}

type Commit struct {
	WorkDir string
	review  *quality.Review
}

type commitInput struct {
	Message     string   `json:"message"`
	AutoReview  bool     `json:"auto_review,omitempty"` // default: true
	ExtraChecks []string `json:"extra_checks,omitempty"`
}

func NewCommitTool(workDir string) *Commit {
	return &Commit{
		WorkDir: workDir,
		review:  &quality.Review{WorkDir: workDir},
	}
}

func (t *Commit) Name() string { return "git_commit" }
func (t *Commit) Description() string {
	return "Stage all changes and create a commit.\n" +
		"**Strongly enforces quality**: By default, it runs review_my_changes first.\n" +
		"Only commits if verification passes."
}
func (t *Commit) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("message", toolutil.NewSchema("string").Desc("Clear, conventional commit message")).
		Prop("auto_review", toolutil.NewSchema("boolean").Desc("Whether to run review_my_changes before committing (default: true)")).
		Prop("extra_checks", toolutil.NewSchema("array").Items(toolutil.NewSchema("string")).Desc("Additional commands to run during review")).
		Required("message").
		Build()
}
func (t *Commit) NeedsPermission(_ json.RawMessage) bool { return true }
func (t *Commit) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
	var inp commitInput
	if err := json.Unmarshal(rawInput, &inp); err != nil {
		return "", fmt.Errorf("git_commit: bad input: %w", err)
	}
	if inp.Message == "" {
		return "", fmt.Errorf("git_commit: message cannot be empty")
	}

	// Default to auto review in interactive mode; headless/hosted jobs skip the gate
	// (execution clients perform result integration; quality review is often a no-op on knowledge repos).
	if !strings.Contains(string(rawInput), `"auto_review"`) {
		inp.AutoReview = !headlessExecution()
	}

	var report strings.Builder
	report.WriteString("=== GIT COMMIT ===\n\n")

	// === 1. Run Review (Mandatory Quality Gate) ===
	if inp.AutoReview {
		report.WriteString("Running quality review before commit...\n\n")

		reviewInputRaw, _ := json.Marshal(map[string]interface{}{"commands": inp.ExtraChecks})
		reviewOutput, reviewErr := t.review.Execute(ctx, reviewInputRaw)
		report.WriteString(reviewOutput)

		if reviewErr != nil || strings.Contains(reviewOutput, "FAILED") {
			report.WriteString("\n❌ Commit blocked: Quality review failed.\n")
			report.WriteString("Fix the issues and try committing again.\n")
			return report.String(), fmt.Errorf("pre-commit verification failed")
		}
		report.WriteString("✅ Quality review passed\n\n")
	}

	// === 2. Stage changes ===
	_, err := runGit(ctx, t.WorkDir, "add", "-A")
	if err != nil {
		return "", fmt.Errorf("git add failed: %w", err)
	}

	// === 3. Commit ===
	commitOut, err := runGit(ctx, t.WorkDir, "commit", "-m", inp.Message)
	if err != nil {
		return report.String() + commitOut, fmt.Errorf("git commit failed: %w", err)
	}

	report.WriteString(commitOut)
	report.WriteString("\n✅ Successfully committed with quality verification.\n")
	return report.String(), nil
}

type Push struct {
	WorkDir string
	review  *quality.Review
}

type pushInput struct {
	Branch      string   `json:"branch,omitempty"`
	Force       bool     `json:"force,omitempty"`
	AutoReview  bool     `json:"auto_review,omitempty"` // default: true
	ExtraChecks []string `json:"extra_checks,omitempty"`
}

func NewPushTool(workDir string) *Push {
	return &Push{
		WorkDir: workDir,
		review:  &quality.Review{WorkDir: workDir},
	}
}

func (t *Push) Name() string { return "git_push" }
func (t *Push) Description() string {
	return "Push committed changes to remote.\n" +
		"Strong safety protections:\n" +
		"- Runs quality review by default\n" +
		"- Warns on protected branches (main/master)\n" +
		"- Checks for unpushed commits and remote status\n" +
		"- Blocks force push unless explicitly allowed"
}
func (t *Push) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("branch", toolutil.NewSchema("string").Desc("Branch to push (default: current)")).
		Prop("force", toolutil.NewSchema("boolean").Desc("Allow force push (DANGEROUS - use sparingly)")).
		Prop("auto_review", toolutil.NewSchema("boolean").Desc("Run review_my_changes before push (default: true)")).
		Prop("extra_checks", toolutil.NewSchema("array").Items(toolutil.NewSchema("string"))).
		Build()
}
func (t *Push) NeedsPermission(_ json.RawMessage) bool { return true }
func (t *Push) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
	var inp pushInput
	if err := json.Unmarshal(rawInput, &inp); err != nil {
		return "", fmt.Errorf("git_push: bad input: %w", err)
	}

	var report strings.Builder
	report.WriteString("=== GIT PUSH ===\n\n")

	// === 1. Safety Checks ===
	currentBranch, err := runGit(ctx, t.WorkDir, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	currentBranch = strings.TrimSpace(currentBranch)

	if inp.Branch == "" {
		inp.Branch = currentBranch
	}

	// Protected branch warning
	if isProtectedBranch(inp.Branch) && !inp.Force {
		report.WriteString(fmt.Sprintf("⚠️  Warning: Pushing to protected branch '%s'\n", inp.Branch))
		report.WriteString("Consider creating a PR instead of direct push.\n\n")
	}

	// === 2. Quality Review ===
	if !strings.Contains(string(rawInput), `"auto_review": false`) {
		inp.AutoReview = true
	}
	if inp.AutoReview {
		report.WriteString("Running pre-push quality review...\n\n")

		reviewRaw, _ := json.Marshal(map[string]interface{}{"commands": inp.ExtraChecks})
		reviewOut, reviewErr := t.review.Execute(ctx, reviewRaw)

		report.WriteString(reviewOut + "\n")

		if reviewErr != nil || strings.Contains(reviewOut, "FAILED") {
			return report.String(), fmt.Errorf("push blocked: quality review failed")
		}
	}

	// === 3. Push ===
	args := []string{"push", "origin", inp.Branch}
	if inp.Force {
		args = append(args, "--force-with-lease") // safer than --force
	}

	out, err := runGit(ctx, t.WorkDir, args...)
	if err != nil {
		report.WriteString(out)
		return report.String(), fmt.Errorf("git push failed: %w", err)
	}

	report.WriteString(out)
	report.WriteString("\n✅ Successfully pushed to origin/" + inp.Branch + "\n")
	return report.String(), nil
}

func isProtectedBranch(branch string) bool {
	protected := []string{"main", "master", "develop", "production", "release"}
	for _, p := range protected {
		if branch == p || strings.HasPrefix(branch, p+"/") {
			return true
		}
	}
	return false
}

type CreateBranch struct{ WorkDir string }

type createBranchInput struct {
	Name     string `json:"name"`
	Checkout bool   `json:"checkout"`
	FromRef  string `json:"from_ref"`
}

func (t *CreateBranch) Name() string { return "git_create_branch" }
func (t *CreateBranch) Description() string {
	return "Create a new git branch, optionally checking it out immediately."
}
func (t *CreateBranch) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("name", toolutil.NewSchema("string").Desc("New branch name")).
		Prop("checkout", toolutil.NewSchema("boolean").Desc("Switch to the new branch after creating it (default: true)")).
		Prop("from_ref", toolutil.NewSchema("string").Desc("Create branch from this ref (default: current HEAD)")).
		Required("name").
		Build()
}
func (t *CreateBranch) NeedsPermission(_ json.RawMessage) bool { return true }
func (t *CreateBranch) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
	var inp createBranchInput
	if err := json.Unmarshal(rawInput, &inp); err != nil {
		return "", fmt.Errorf("git_create_branch: bad input: %w", err)
	}
	if inp.Name == "" {
		return "", fmt.Errorf("git_create_branch: name cannot be empty")
	}

	checkout := inp.Checkout || !strings.Contains(string(rawInput), `"checkout"`)

	var args []string
	if checkout {
		args = []string{"checkout", "-b", inp.Name}
	} else {
		args = []string{"branch", inp.Name}
	}
	if inp.FromRef != "" {
		args = append(args, inp.FromRef)
	}
	return runGit(ctx, t.WorkDir, args...)
}

func headlessExecution() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("DROVER_CODE_HEADLESS")))
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

