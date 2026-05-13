// Package coordinator implements the multi-agent coordinator mode.
package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/api"
	"github.com/cloudshuttle/drover-code/internal/config"
	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/internal/permissions"
	"github.com/cloudshuttle/drover-code/internal/tools"
	"github.com/cloudshuttle/drover-code/internal/tools/ukc"
)

const maxCoordinatorSubtasks = 8

type Subtask struct {
	Index       int
	Description string
	IsolatedDir string
}

type WorkerResult struct {
	Index   int
	Task    string
	Output  string
	IsError bool
}

// ExecuteOutcome is the coordinator result, including per-worker outputs for
// richer transcripts (e.g. Dream consolidation after coordinator mode).
type ExecuteOutcome struct {
	Summary string
	Workers []WorkerResult
}

type Coordinator struct {
	client     *api.Client
	registry   *tools.Registry
	workDir    string
	eventCh    chan<- agent.Event
	settings   config.Settings
	MaxWorkers int
	gitMu      sync.Mutex
}

func New(client *api.Client, registry *tools.Registry, workDir string, eventCh chan<- agent.Event, settings config.Settings) *Coordinator {
	return &Coordinator{
		client:     client,
		registry:   registry,
		workDir:    workDir,
		eventCh:    eventCh,
		settings:   settings,
		MaxWorkers: 4,
	}
}

func (c *Coordinator) Execute(ctx context.Context, task string) (string, error) {
	out, err := c.ExecuteWithResults(ctx, task)
	return out.Summary, err
}

func (c *Coordinator) ExecuteWithResults(ctx context.Context, task string) (ExecuteOutcome, error) {
	var z ExecuteOutcome
	
	c.eventCh <- agent.TextDeltaEvent{Text: "Decomposing task into subtasks via Anthropic API...\n"}
	
	subtasks, err := c.decompose(ctx, task)
	if err != nil {
		return z, fmt.Errorf("coordinator: decompose: %w", err)
	}
	if len(subtasks) == 0 {
		return z, fmt.Errorf("coordinator: no subtasks generated")
	}

	results, err := c.executeWorkers(ctx, subtasks)
	if err != nil {
		return z, fmt.Errorf("coordinator: workers: %w", err)
	}

	summary, err := c.synthesise(ctx, task, results)
	if err != nil {
		return z, err
	}
	z.Summary = summary
	z.Workers = results
	return z, nil
}

func (c *Coordinator) decompose(ctx context.Context, task string) ([]Subtask, error) {
	prompt := fmt.Sprintf(`You are a coordinator agent. Break the following task into 2-4 parallel subtasks
that can be executed independently by separate worker agents.

Each worker has access to: read_file, write_file, edit_file, bash, glob, grep, git tools.

Return ONLY a JSON array of subtask descriptions (strings). No other text.
Example: ["Refactor authentication module", "Update unit tests for auth", "Update API documentation"]

Task: %s`, task)

	mgr := convo.NewManager()
	mgr.Append(api.UserMessage(prompt))

	stream, err := c.client.StreamMessage(ctx, api.StreamRequest{
		Messages:  mgr.Messages(),
		MaxTokens: 512,
	})
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	var raw strings.Builder
	for stream.Next() {
		if e, ok := stream.Event().(api.ContentBlockDeltaEvent); ok {
			if td, ok := e.Delta.(api.TextDelta); ok {
				raw.WriteString(td.Text)
				select {
				case c.eventCh <- agent.TextDeltaEvent{Text: td.Text}:
				default:
				}
			}
		}
	}
	if stream.Err() != nil {
		return nil, stream.Err()
	}
	select {
	case c.eventCh <- agent.TextDeltaEvent{Text: "\n"}:
	default:
	}

	jsonStr := extractJSON(raw.String())
	descriptions := ParseSubtaskDescriptionsJSON(jsonStr, task)
	subtasks := make([]Subtask, len(descriptions))
	for i, d := range descriptions {
		subtasks[i] = Subtask{Index: i, Description: d}
	}
	return subtasks, nil
}

func (c *Coordinator) executeWorkers(ctx context.Context, subtasks []Subtask) ([]WorkerResult, error) {
	for i := range subtasks {
		dir, err := IsolatedWorkDir(c.workDir, subtasks[i].Index)
		if err != nil {
			return nil, fmt.Errorf("coordinator: worker dir: %w", err)
		}
		subtasks[i].IsolatedDir = dir
	}

	results := make([]WorkerResult, len(subtasks))

	var customImage string
	if c.settings.CoordinatorRemote {
		var err error
		customImage, err = c.buildCustomToolchain(ctx)
		if err != nil {
			return nil, fmt.Errorf("custom toolchain: %w", err)
		}
	}

	sem := make(chan struct{}, c.MaxWorkers)
	g, gctx := errgroup.WithContext(ctx)
	var mu sync.Mutex

	for _, st := range subtasks {
		st := st
		sem <- struct{}{}

		g.Go(func() error {
			defer func() { <-sem }()

			if err := gctx.Err(); err != nil {
				mu.Lock()
				results[st.Index] = WorkerResult{
					Index: st.Index, Task: st.Description, Output: err.Error(), IsError: true,
				}
				mu.Unlock()
				return err
			}
			var result WorkerResult
			var err error
			if c.settings.CoordinatorRemote {
				result, err = c.runWorkerRemote(gctx, st, customImage)
			} else {
				result, err = c.runWorker(gctx, st)
			}
			mu.Lock()
			results[st.Index] = result
			mu.Unlock()
			return err
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return results, nil
}

func (c *Coordinator) runWorker(ctx context.Context, st Subtask) (WorkerResult, error) {
	workerMgr := convo.NewManagerWithSystem(workerSystemPrompt(st.Description))
	config.ApplyConvoHeuristics(workerMgr, c.settings)

	absBase, errAbs := filepath.Abs(c.workDir)
	if errAbs != nil {
		absBase = c.workDir
	}
	repoMount := filepath.Join(st.IsolatedDir, "workspace")
	_ = os.Remove(repoMount)
	if errLink := os.Symlink(absBase, repoMount); errLink != nil {
		repoMount = absBase
	}
	reg := tools.NewRegistry()
	tools.RegisterAll(reg, repoMount)

	workerEvents := make(chan agent.Event, 128)
	go c.forwardWorkerEvents(st.Index, workerEvents)

	workerLoop := agent.NewLoop(
		c.client,
		workerMgr,
		reg,
		permissions.NewEngine(permissions.ModeBypass, nil, nil, "", tools.AllowAll),
		workerEvents,
	)
	config.ApplyAgentLoopOptions(workerLoop, c.settings)

	var output strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range workerEvents {
			if td, ok := ev.(agent.TextDeltaEvent); ok {
				output.WriteString(td.Text)
			}
		}
	}()

	err := workerLoop.Run(ctx, st.Description)
	close(workerEvents)
	<-done

	if err != nil {
		wr := WorkerResult{
			Index:   st.Index,
			Task:    st.Description,
			Output:  err.Error(),
			IsError: true,
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return wr, err
		}
		return wr, nil
	}
	return WorkerResult{
		Index:  st.Index,
		Task:   st.Description,
		Output: output.String(),
	}, nil
}

func (c *Coordinator) buildCustomToolchain(ctx context.Context) (string, error) {
	dockerfilePath := filepath.Join(c.workDir, "drover-worker.Dockerfile")
	if _, err := os.Stat(dockerfilePath); os.IsNotExist(err) {
		return "", nil // No custom toolchain
	}

	mgr, ok, err := ukc.NewManagerFromEnv()
	if !ok || err != nil {
		return "", fmt.Errorf("could not load ukc config to determine namespace: %v", err)
	}

	defaultImage := mgr.Config().DefaultImage
	parts := strings.Split(defaultImage, "/")
	
	var registry, namespace string
	if len(parts) >= 3 {
		registry = parts[0]
		namespace = parts[1]
	} else if len(parts) == 2 {
		registry = "docker.io"
		namespace = parts[0]
	} else {
		return "", fmt.Errorf("invalid default image format for parsing namespace: %s", defaultImage)
	}

	projectName := filepath.Base(c.workDir)
	customImage := fmt.Sprintf("%s/%s/drover-worker-%s:latest", registry, namespace, projectName)

	c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n🔨 Found drover-worker.Dockerfile! Building custom toolchain image: %s\n", customImage)}

	// Build
	buildCmd := exec.CommandContext(ctx, "docker", "build", "--platform", "linux/amd64", "-t", customImage, "-f", dockerfilePath, c.workDir)
	// Optionally capture output or redirect to coordinator's stdout
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return "", fmt.Errorf("docker build failed: %v", err)
	}

	// Push
	c.eventCh <- agent.TextDeltaEvent{Text: "☁️ Pushing custom image to Kraftcloud registry...\n"}
	pushCmd := exec.CommandContext(ctx, "docker", "push", customImage)
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return "", fmt.Errorf("docker push failed: %v", err)
	}

	c.eventCh <- agent.TextDeltaEvent{Text: "✅ Custom toolchain ready.\n"}
	return customImage, nil
}

func (c *Coordinator) runWorkerRemote(ctx context.Context, st Subtask, customImage string) (WorkerResult, error) {
	mgr, ok, err := ukc.NewManagerFromEnv()
	if !ok || err != nil {
		return WorkerResult{Index: st.Index, Task: st.Description, IsError: true, Output: "UKC_TOKEN not set or invalid for remote coordinator"}, fmt.Errorf("remote coordinator needs UKC_TOKEN")
	}

	name := fmt.Sprintf("drover-worker-%d-%d", st.Index, time.Now().Unix())
	c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n[worker %d] Provisioning UKC instance %s...\n", st.Index+1, name)}

	// Create instance
	token, _ := ukc.RandToken()
	cfg := mgr.Config()
	env := map[string]string{
		"AGENT_TOKEN": token,
	}
	// Forward LLM API keys and configuration to the remote workers
	for _, k := range []string{
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY",
		"ANTHROPIC_MODEL", "OPENAI_MODEL", "GEMINI_MODEL",
	} {
		if v := os.Getenv(k); v != "" {
			env[k] = v
		}
	}

	img := cfg.DefaultImage
	if customImage != "" {
		img = customImage
	}

	inst, err := ukc.CreateInstance(ctx, cfg, name, img, 512, env)
	if err != nil {
		return WorkerResult{Index: st.Index, Task: st.Description, IsError: true, Output: err.Error()}, err
	}

	// Fetch the complete Service Group object to get the Domains if they aren't included inline
	if inst.ServiceGroup != nil && inst.ServiceGroup.UUID != "" && len(inst.ServiceGroup.Domains) == 0 {
		sg, err := ukc.GetServiceGroup(ctx, cfg, inst.ServiceGroup.UUID)
		if err == nil {
			inst.ServiceGroup = &sg
		} else {
			return WorkerResult{Index: st.Index, Task: st.Description, IsError: true, Output: "failed to get service group: " + err.Error()}, err
		}
	}

	instURL := ukc.InstanceHTTPSURL(inst)
	if instURL == "" {
		// Cleanup the instance if we couldn't get a URL
		_ = ukc.DeleteInstance(context.Background(), cfg, inst.UUID)
		return WorkerResult{Index: st.Index, Task: st.Description, IsError: true, Output: "Could not determine public HTTPS URL for Unikraft instance"}, fmt.Errorf("empty instance URL")
	}

	c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n[worker %d] Instance created, waiting for health check at %s...\n", st.Index+1, instURL)}

	// Always cleanup
	defer func() {
		// err := ukc.DeleteInstance(context.Background(), cfg, inst.UUID)
		// if err != nil {
		// 	c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n[worker %d] ⚠️ Failed to destroy UKC instance %s: %v\n", st.Index+1, name, err)}
		// } else {
		// 	c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n[worker %d] Destroyed UKC instance %s\n", st.Index+1, name)}
		// }
	}()

	if err := ukc.WaitForHealth(ctx, cfg.HTTPClient, instURL, token, cfg.MaxHealthWait); err != nil {
		return WorkerResult{Index: st.Index, Task: st.Description, IsError: true, Output: "instance health timeout: " + err.Error()}, err
	}

	c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n[worker %d] Uploading local workspace to cloud instance...\n", st.Index+1)}
	if err := ukc.UploadWorkspace(ctx, cfg, inst, c.workDir, token); err != nil {
		return WorkerResult{Index: st.Index, Task: st.Description, IsError: true, Output: "upload workspace failed: " + err.Error()}, err
	}

	c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n[worker %d] Instance ready, executing headless task...\n", st.Index+1)}

	safeTask := strings.ReplaceAll(st.Description, "'", "'\\''")
	command := fmt.Sprintf("drover-code --headless --prompt '%s'", safeTask)

	body, _ := json.Marshal(map[string]string{"command": command})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(instURL, "/")+"/exec", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := cfg.HTTPClient.Do(req)
	if err != nil {
		return WorkerResult{Index: st.Index, Task: st.Description, IsError: true, Output: "exec POST: " + err.Error()}, err
	}
	defer resp.Body.Close()
	var postResp struct { JobID string `json:"job_id"` }
	if err := json.NewDecoder(resp.Body).Decode(&postResp); err != nil {
		return WorkerResult{Index: st.Index, Task: st.Description, IsError: true, Output: "exec POST decode: " + err.Error()}, err
	}

	streamURL := strings.TrimRight(instURL, "/") + "/exec/" + postResp.JobID + "/stream"
	var onLine func(string)
	if c.settings.Verbose {
		onLine = func(payload string) {
			var m map[string]any
			if err := json.Unmarshal([]byte(payload), &m); err != nil {
				return
			}
			lineStr, ok := m["line"].(string)
			if !ok {
				return
			}
			var ev map[string]any
			if err := json.Unmarshal([]byte(lineStr), &ev); err != nil {
				return
			}
			typ, _ := ev["type"].(string)
			switch typ {
			case "tool_start":
				name, _ := ev["name"].(string)
				c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("[worker %d] 🔨 using tool: %s\n", st.Index+1, name)}
			case "heartbeat":
				turn, _ := ev["turn"].(float64)
				c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("[worker %d] 🧠 thinking... (turn %d)\n", st.Index+1, int(turn))}
			}
		}
	}
	outStr, exitCode, err := ukc.ReadExecStream(ctx, cfg.HTTPClient, streamURL, token, onLine)

	if err != nil {
		return WorkerResult{Index: st.Index, Task: st.Description, IsError: true, Output: err.Error()}, err
	}
	if exitCode != 0 {
		return WorkerResult{Index: st.Index, Task: st.Description, IsError: true, Output: outStr}, fmt.Errorf("remote task exited with %d", exitCode)
	}

	c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n[worker %d] Task complete. Downloading modified workspace...\n", st.Index+1)}
	downloadDir := filepath.Join(st.IsolatedDir, "workspace_downloaded")
	_ = os.RemoveAll(downloadDir)
	if err := ukc.DownloadWorkspace(ctx, cfg, inst, downloadDir, token); err != nil {
		c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n[worker %d] ⚠️ Failed to download workspace: %v\n", st.Index+1, err)}
	} else {
		if err := c.syncDownloadedWorkspace(ctx, st, downloadDir); err != nil {
			c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n[worker %d] ⚠️ Failed to merge workspace: %v\n", st.Index+1, err)}
		}
	}

	return WorkerResult{
		Index:  st.Index,
		Task:   st.Description,
		Output: outStr,
	}, nil
}

func (c *Coordinator) forwardWorkerEvents(workerIdx int, ch <-chan agent.Event) {
	for ev := range ch {
		switch e := ev.(type) {
		case agent.ToolStartEvent:
			e.CallIndex = workerIdx*100 + e.CallIndex
			select {
			case c.eventCh <- e:
			default:
			}
		case agent.ToolDoneEvent:
			e.CallIndex = workerIdx*100 + e.CallIndex
			select {
			case c.eventCh <- e:
			default:
			}
		case agent.CompactionStartEvent:
			select {
			case c.eventCh <- e:
			default:
			}
		case agent.CompactionDoneEvent:
			select {
			case c.eventCh <- e:
			default:
			}
		}
	}
}

func (c *Coordinator) synthesise(ctx context.Context, originalTask string, results []WorkerResult) (string, error) {
	if c.settings.CoordinatorRemote {
		var b strings.Builder
		fmt.Fprintf(&b, "\n## 🤖 Parallel Execution Complete\n\n")
		for _, r := range results {
			status := "✅ Completed"
			if r.IsError {
				status = "❌ Failed"
			}
			if r.Output != "" {
				status += "\n  " + strings.TrimSpace(r.Output)
			}
			fmt.Fprintf(&b, "- **Worker %d** (%s): %s\n", r.Index+1, r.Task, status)
		}
		fmt.Fprintf(&b, "\n✨ Parallel tasks complete.\n")
		
		// Send it to the terminal exactly like the stream does
		c.eventCh <- agent.TextDeltaEvent{Text: b.String()}
		return b.String(), nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Original task: %s\n\nWorker results:\n\n", originalTask)
	for _, r := range results {
		status := "✓"
		if r.IsError {
			status = "✗"
		}
		fmt.Fprintf(&b, "%s Worker %d (%s):\n%s\n\n", status, r.Index+1, r.Task, r.Output)
	}
	b.WriteString("Synthesise the above results into a single clear response for the user. " +
		"Summarise what was accomplished, note any errors, and provide a coherent overview.")

	mgr := convo.NewManager()
	mgr.Append(api.UserMessage(b.String()))

	stream, err := c.client.StreamMessage(ctx, api.StreamRequest{
		Messages:  mgr.Messages(),
		MaxTokens: 2048,
	})
	if err != nil {
		return "", err
	}
	defer stream.Close()

	var summary strings.Builder
	for stream.Next() {
		if e, ok := stream.Event().(api.ContentBlockDeltaEvent); ok {
			if td, ok := e.Delta.(api.TextDelta); ok {
				summary.WriteString(td.Text)
				select {
				case c.eventCh <- agent.TextDeltaEvent{Text: td.Text}:
				default:
				}
			}
		}
	}
	if stream.Err() != nil {
		return "", stream.Err()
	}
	return summary.String(), nil
}

func (c *Coordinator) syncDownloadedWorkspace(ctx context.Context, st Subtask, downloadedDir string) error {
	c.gitMu.Lock()
	defer c.gitMu.Unlock()

	// Check if working tree is dirty
	cmd := exec.CommandContext(ctx, "git", "-C", c.workDir, "status", "--porcelain")
	out, err := cmd.Output()
	if err == nil && len(bytes.TrimSpace(out)) > 0 {
		c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n[worker %d] ⚠️ Working tree is dirty! Skipping auto-merge. Downloaded files are saved in %s\n", st.Index+1, downloadedDir)}
		return nil
	}

	// Get current branch for auto-merge
	var currentBranch string
	branchCmd := exec.CommandContext(ctx, "git", "-C", c.workDir, "branch", "--show-current")
	if out, err := branchCmd.Output(); err == nil {
		currentBranch = strings.TrimSpace(string(out))
	}

	// Create new branch
	branchName := fmt.Sprintf("drover/task-%d-%d", st.Index+1, time.Now().Unix())
	if err := exec.CommandContext(ctx, "git", "-C", c.workDir, "checkout", "-b", branchName).Run(); err != nil {
		return fmt.Errorf("git checkout -b: %v", err)
	}

	// Copy files (using rsync to handle deletes, but fallback to cp if needed)
	rsyncCmd := exec.CommandContext(ctx, "rsync", "-a", "--delete", "--exclude=.git", "--exclude=.drover-code-workers", "--exclude=.unikraft", downloadedDir+"/", c.workDir+"/")
	if err := rsyncCmd.Run(); err != nil {
		c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n[worker %d] ⚠️ Rsync failed, falling back to cp: %v\n", st.Index+1, err)}
		// fallback to cp if rsync is not available
		cpCmd := exec.CommandContext(ctx, "cp", "-R", downloadedDir+"/", c.workDir+"/")
		if err := cpCmd.Run(); err != nil {
			return fmt.Errorf("failed to copy files: %v", err)
		}
	}

	// Commit changes
	if err := exec.CommandContext(ctx, "git", "-C", c.workDir, "add", ".").Run(); err != nil {
		return fmt.Errorf("git add: %v", err)
	}
	
	// We check if there's anything to commit first
	statusCmd := exec.CommandContext(ctx, "git", "-C", c.workDir, "status", "--porcelain")
	outBytes, _ := statusCmd.Output()
	if len(bytes.TrimSpace(outBytes)) == 0 {
		c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n[worker %d] ⚠️ No changes produced by worker to commit.\n", st.Index+1)}
	} else {
		if err := exec.CommandContext(ctx, "git", "-C", c.workDir, "commit", "-m", fmt.Sprintf("AI Gen: %s", st.Description)).Run(); err != nil {
			c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n[worker %d] ⚠️ Failed to commit changes: %v\n", st.Index+1, err)}
		} else {
			c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n[worker %d] ✅ Workspace committed to branch `%s`\n", st.Index+1, branchName)}
			
			if c.settings.AcceptCmd != "" {
				c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("[worker %d] 🔄 Running acceptance criteria: %s\n", st.Index+1, c.settings.AcceptCmd)}
				acceptCmd := exec.CommandContext(ctx, "/bin/sh", "-c", c.settings.AcceptCmd)
				acceptCmd.Dir = c.workDir
				if err := acceptCmd.Run(); err != nil {
					c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("[worker %d] ❌ Acceptance criteria failed: %v. Leaving changes in branch `%s`\n", st.Index+1, err, branchName)}
				} else {
					if currentBranch != "" {
						c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("[worker %d] ✅ Acceptance criteria passed! Auto-merging into `%s`\n", st.Index+1, currentBranch)}
						if err := exec.CommandContext(ctx, "git", "-C", c.workDir, "checkout", currentBranch).Run(); err == nil {
							exec.CommandContext(ctx, "git", "-C", c.workDir, "merge", branchName).Run()
							return nil // successfully merged and checked out
						}
					} else {
						c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("[worker %d] ⚠️ Acceptance passed, but could not determine original branch to auto-merge.\n", st.Index+1)}
					}
				}
			}
		}
	}

	// Return to previous branch
	if err := exec.CommandContext(ctx, "git", "-C", c.workDir, "checkout", "-").Run(); err != nil {
		c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n[worker %d] ⚠️ Failed to restore original branch: %v\n", st.Index+1, err)}
	}
	return nil
}

func workerSystemPrompt(task string) string {
	return fmt.Sprintf(`You are a worker agent. Your assigned task is:

%s

Complete this task using the available tools. Be precise and focused.
Do not attempt tasks outside your assignment. Report what you did concisely.`, task)
}

// ParseSubtaskDescriptionsJSON parses a JSON array of subtasks; only string
// elements are kept (non-strings skipped), trimmed, empties dropped, capped at
// maxCoordinatorSubtasks. If nothing valid remains, fallback is returned alone.
func ParseSubtaskDescriptionsJSON(jsonStr, fallback string) []string {
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return []string{fallback}
	}
	var out []string
	for _, rm := range raw {
		if len(out) >= maxCoordinatorSubtasks {
			break
		}
		var s string
		if err := json.Unmarshal(rm, &s); err != nil {
			continue
		}
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return []string{fallback}
	}
	return out
}

func extractJSON(s string) string {
	if idx := strings.Index(s, "```"); idx >= 0 {
		s = s[idx+3:]
		if idx2 := strings.Index(s, "```"); idx2 >= 0 {
			s = s[:idx2]
		}
		s = strings.TrimPrefix(s, "json")
	}
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start < 0 || end < 0 || end <= start {
		return "[]"
	}
	return s[start : end+1]
}

func IsolatedWorkDir(baseDir string, workerIdx int) (string, error) {
	dir := filepath.Join(baseDir, ".drover-code-workers", fmt.Sprintf("worker-%d", workerIdx))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
