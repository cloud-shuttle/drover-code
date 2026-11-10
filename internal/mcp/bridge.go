package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/config"
	"github.com/cloudshuttle/drover-code/internal/tools"
)

// Caller is the MCP transport (stdio or HTTP).
type Caller interface {
	Call(ctx context.Context, method string, params any, result any) error
}

// MCPTool wraps an MCP client's tool to implement tools.Tool.
// name is the model-facing name (mcp_{server}_{tool}). callName is the name
// the server listed and expects in tools/call.
type MCPTool struct {
	client      Caller
	name        string
	callName    string
	description string
	inputSchema json.RawMessage
}

func (t *MCPTool) Name() string {
	return t.name
}

func (t *MCPTool) Description() string {
	return t.description
}

func (t *MCPTool) InputSchema() json.RawMessage {
	return t.inputSchema
}

func (t *MCPTool) NeedsPermission(input json.RawMessage) bool {
	return true // External MCP tools always prompt for permission
}

func (t *MCPTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var inputMap map[string]any
	if err := json.Unmarshal(input, &inputMap); err != nil {
		return "", err
	}

	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}

	callName := t.callName
	if callName == "" {
		callName = t.name
	}
	err := t.client.Call(ctx, "tools/call", map[string]any{
		"name":      callName,
		"arguments": inputMap,
	}, &res)

	if err != nil {
		return "", err
	}

	var out strings.Builder
	for _, part := range res.Content {
		if part.Type != "" && part.Type != "text" {
			continue
		}
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(part.Text)
	}
	if res.IsError {
		return out.String(), fmt.Errorf("mcp tool error: %s", out.String())
	}
	return out.String(), nil
}

// RegisterAll starts each configured MCP server and registers its tools.
// Warnings are for the operator. A server that fails to start is skipped.
func RegisterAll(ctx context.Context, reg *tools.Registry, settings config.Settings) []string {
	var warnings []string
	for name, srvConfig := range settings.MCPServers {
		caller, stop, err := dial(ctx, name, srvConfig)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}

		var listRes struct {
			Tools []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"inputSchema"`
			} `json:"tools"`
		}
		if err := caller.Call(ctx, "tools/list", map[string]any{}, &listRes); err != nil {
			warnings = append(warnings, fmt.Sprintf("mcp %s: list tools: %v", name, err))
			if stop != nil {
				_ = stop()
			}
			continue
		}

		for _, t := range listRes.Tools {
			reg.Register(&MCPTool{
				client:      caller,
				name:        fmt.Sprintf("mcp_%s_%s", name, t.Name),
				callName:    t.Name,
				description: fmt.Sprintf("[%s] %s", name, t.Description),
				inputSchema: t.InputSchema,
			})
		}

		if stop != nil {
			go func() {
				<-ctx.Done()
				_ = stop()
			}()
		}
	}
	return warnings
}

func dial(ctx context.Context, name string, cfg config.MCPServerConfig) (Caller, func() error, error) {
	hasCmd := len(cfg.Command) > 0
	hasURL := strings.TrimSpace(cfg.URL) != ""
	if hasCmd && hasURL {
		return nil, nil, fmt.Errorf("mcp %s: set command or url, not both", name)
	}
	if !hasCmd && !hasURL {
		return nil, nil, fmt.Errorf("mcp %s: missing command or url", name)
	}
	if hasURL {
		h := NewHTTPClient(cfg.URL, cfg.Headers)
		if err := h.Start(ctx); err != nil {
			return nil, nil, fmt.Errorf("mcp %s: %v", name, err)
		}
		return h, h.Stop, nil
	}

	client := NewClient(cfg.Command)
	if len(cfg.Env) > 0 {
		client.cmd.Env = os.Environ()
		for k, v := range cfg.Env {
			client.cmd.Env = append(client.cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}
	if err := client.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("mcp %s: start: %v", name, err)
	}
	return client, client.Stop, nil
}
