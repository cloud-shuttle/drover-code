package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/cloudshuttle/drover-code/internal/config"
	"github.com/cloudshuttle/drover-code/internal/tools"
)

// MCPTool wraps an MCP client's tool to implement tools.Tool.
type MCPTool struct {
	client      *Client
	name        string
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

	err := t.client.Call(ctx, "tools/call", map[string]any{
		"name":      t.name,
		"arguments": inputMap,
	}, &res)

	if err != nil {
		return "", err
	}

	if len(res.Content) > 0 {
		out := res.Content[0].Text
		if res.IsError {
			return out, fmt.Errorf("mcp tool error: %s", out)
		}
		return out, nil
	}

	return "", nil
}

// RegisterAll reads config settings and starts MCP servers, registering their tools.
func RegisterAll(ctx context.Context, reg *tools.Registry, settings config.Settings) {
	for name, srvConfig := range settings.MCPServers {
		if len(srvConfig.Command) == 0 {
			continue
		}

		client := NewClient(srvConfig.Command)
		
		// If command uses env vars, we might need to set them on the exec.Cmd
		if len(srvConfig.Env) > 0 {
			client.cmd.Env = os.Environ()
			for k, v := range srvConfig.Env {
				client.cmd.Env = append(client.cmd.Env, fmt.Sprintf("%s=%s", k, v))
			}
		}

		if err := client.Start(ctx); err != nil {
			log.Printf("warning: mcp server %s failed to start: %v", name, err)
			continue
		}
		
		// Fetch tools
		var listRes struct {
			Tools []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"inputSchema"`
			} `json:"tools"`
		}
		
		if err := client.Call(ctx, "tools/list", map[string]any{}, &listRes); err != nil {
			log.Printf("warning: mcp server %s failed to list tools: %v", name, err)
			continue
		}

		for _, t := range listRes.Tools {
			// Prefix tool name to avoid collisions
			prefixedName := fmt.Sprintf("mcp_%s_%s", name, t.Name)
			mcpTool := &MCPTool{
				client:      client,
				name:        prefixedName,
				description: fmt.Sprintf("[%s] %s", name, t.Description),
				inputSchema: t.InputSchema,
			}
			reg.Register(mcpTool)
		}

		// Ensure client stops on shutdown
		go func() {
			<-ctx.Done()
			client.Stop()
		}()
	}
}
