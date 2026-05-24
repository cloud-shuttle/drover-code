package commands

import (
	"context"
	"fmt"
	"os"

	droverwarden "github.com/cloud-shuttle/drover-warden/warden"
	"github.com/cloudshuttle/drover-code/internal/warden"
	"github.com/cloudshuttle/drover-code/pkg/guardclient"
)
type Executor struct {
	guardClient *guardclient.Client
	registry    *CommandRegistry
	expander    *TemplateExpander
}

func NewExecutor(registry *CommandRegistry, expander *TemplateExpander, guardClient *guardclient.Client) *Executor {
	return &Executor{
		registry:    registry,
		expander:    expander,
		guardClient: guardClient,
	}
}

// GetRegistry returns the underlying command registry.
func (e *Executor) GetRegistry() *CommandRegistry {
	return e.registry
}

// EvaluateAndExpand checks if a command exists. If it does, it expands the template,
// checks with Drover Guard, and returns the expanded prompt and the command definition.
func (e *Executor) EvaluateAndExpand(ctx context.Context, cmdName string, rawArgs []string) (string, *CommandDefinition, error) {
	cmd, exists := e.registry.Get(cmdName)
	if !exists {
		return "", nil, fmt.Errorf("command %s not found", cmdName)
	}

	prompt, err := e.expander.Expand(ctx, cmd.Template, rawArgs)
	if err != nil {
		return "", nil, err
	}

	agentID := os.Getenv("DROVER_AGENT_ID")
	if agentID == "" {
		agentID = "drover-code-agent"
	}
	tenantID := os.Getenv("DROVER_TENANT_ID")
	if tenantID == "" {
		tenantID = "default"
	}

	if e.guardClient != nil {

		action := guardclient.EvaluateRequest{
			TenantID:     tenantID,
			AgentID:      agentID,
			Action:       "command.execute",
			ResourceType: "custom_command",
			ResourceID:   cmd.Name,
			Permission:   "execute",
			RiskTier:     cmd.RiskTier,
			Payload: map[string]interface{}{
				"command":  cmd.Name,
				"args":     rawArgs,
				"template": cmd.Template,
				"subtask":  cmd.Subtask,
			},
		}

		decision, err := e.guardClient.Evaluate(ctx, action)
		if err != nil {
			// Fail closed if the evaluation fails and guard is configured.
			return "", nil, fmt.Errorf("guard evaluation failed: %w", err)
		}

		if !decision.Allowed {
			return "", nil, fmt.Errorf("command blocked by Drover Guard: %s", decision.Reason)
		}

		if decision.HITLRequired {
			// For now, we return an error. True HITL requires suspending execution.
			return "", nil, fmt.Errorf("command requires HITL approval (not yet supported in CLI): %s", decision.Reason)
		}
	}

	// Warden Action Guard (semantic safety via JSONL Beads) — Option B start
	var args map[string]any
	// rawArgs is []string in this context; put them under a generic key for policies
	if len(rawArgs) > 0 {
		args = map[string]any{"args": rawArgs}
	}

	if wdec := warden.CheckAction(ctx, &droverwarden.GuardRequest{
		TenantID: tenantID,
		ToolCall: &droverwarden.ToolCall{
			ToolName: cmd.Name,
			Args:     args,
		},
		Context: map[string]any{
			"agent_id": agentID,
		},
	}); !wdec.Allowed {
		return "", nil, fmt.Errorf("command blocked by Warden: %s", wdec.Result.Reason)
	}

	return prompt, &cmd, nil
}
