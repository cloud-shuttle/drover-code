package ukc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

// Exec runs a shell command on the remote agent and returns combined logs + exit code.
type Exec struct{ M *Manager }

func (t *Exec) Name() string { return "ukc_exec" }
func (t *Exec) Description() string {
	return "Run a shell command on a Unikraft Cloud instance via the in-VM agent (POST /exec + SSE). " +
		"Uses the instance registry from ukc_create."
}
func (t *Exec) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("instance_id", toolutil.NewSchema("string").Desc("UUID returned by ukc_create")).
		Prop("command", toolutil.NewSchema("string").Desc("Shell command (sh -c)")).
		Prop("timeout_seconds", toolutil.NewSchema("integer").Desc("Max time for the whole operation (HTTP + stream)")).
		Required("instance_id", "command", "timeout_seconds").
		Build()
}
func (t *Exec) NeedsPermission(_ json.RawMessage) bool { return true }

type execInput struct {
	InstanceID string `json:"instance_id"`
	Command    string `json:"command"`
	TimeoutSec int    `json:"timeout_seconds"`
}

func (t *Exec) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	if t.M == nil {
		return "", fmt.Errorf("ukc_exec: manager not configured (set UKC_TOKEN)")
	}
	var in execInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return "", fmt.Errorf("ukc_exec: bad input: %w", err)
	}
	in.InstanceID = strings.TrimSpace(in.InstanceID)
	in.Command = strings.TrimSpace(in.Command)
	if in.InstanceID == "" || in.Command == "" {
		return "", fmt.Errorf("ukc_exec: instance_id and command are required")
	}
	if in.TimeoutSec <= 0 {
		return "", fmt.Errorf("ukc_exec: timeout_seconds must be positive")
	}

	t.M.mu.Lock()
	ent, ok := t.M.entries[in.InstanceID]
	cfg := t.M.cfg
	t.M.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("ukc_exec: unknown instance_id %q (try ukc_list)", in.InstanceID)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(in.TimeoutSec)*time.Second)
	defer cancel()

	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	jobID, err := PostExecAt(ctx, client, ent.URL, ent.Token, in.Command)
	if err != nil {
		return "", fmt.Errorf("ukc_exec: %w", err)
	}

	streamURL := ExecStreamURL(ent.URL, jobID)
	out, code, err := ReadExecStream(ctx, client, streamURL, ent.Token, nil)
	if err != nil {
		return "", err
	}
	summary := fmt.Sprintf("exit_code: %d\n\n%s", code, out)
	return toolutil.Truncate(summary), nil
}
