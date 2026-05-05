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
				result, err = c.runWorkerRemote(gctx, st)
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

func (c *Coordinator) runWorkerRemote(ctx context.Context, st Subtask) (WorkerResult, error) {
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

	inst, err := ukc.CreateInstance(ctx, cfg, name, cfg.DefaultImage, 512, env)
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
		err := ukc.DeleteInstance(context.Background(), cfg, inst.UUID)
		if err != nil {
			c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n[worker %d] ⚠️ Failed to destroy UKC instance %s: %v\n", st.Index+1, name, err)}
		} else {
			c.eventCh <- agent.TextDeltaEvent{Text: fmt.Sprintf("\n[worker %d] Destroyed UKC instance %s\n", st.Index+1, name)}
		}
	}()

	if err := ukc.WaitForHealth(ctx, cfg.HTTPClient, instURL, token, cfg.MaxHealthWait); err != nil {
		return WorkerResult{Index: st.Index, Task: st.Description, IsError: true, Output: "instance health timeout: " + err.Error()}, err
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
