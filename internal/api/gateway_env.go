package api

import (
	"os"
	"strings"
)

const (
	EnvGatewayDimAgentJobID = "DROVER_GATEWAY_DIM_AGENT_JOB_ID"
	EnvGatewayDimCustomerID = "DROVER_GATEWAY_DIM_CUSTOMER_ID"
	EnvAgentCredential      = "DROVER_AGENT_CREDENTIAL"
	EnvAnthropicBaseURL     = "ANTHROPIC_BASE_URL"
)

func gatewayMode() bool {
	return strings.TrimSpace(os.Getenv(EnvAnthropicBaseURL)) != ""
}

func virtualKeyFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN"))
}

// GatewayRequestHeadersFromEnv builds x-bf-dim-*, x-bf-vk, and MCP credential headers for hosted Gateway traffic.
func GatewayRequestHeadersFromEnv() map[string]string {
	out := map[string]string{}
	if v := strings.TrimSpace(os.Getenv(EnvGatewayDimAgentJobID)); v != "" {
		out["x-bf-dim-agent-job-id"] = v
	}
	if v := strings.TrimSpace(os.Getenv(EnvGatewayDimCustomerID)); v != "" {
		out["x-bf-dim-customer-id"] = v
	}
	if gatewayMode() {
		if vk := virtualKeyFromEnv(); vk != "" {
			out["x-bf-vk"] = vk
		}
	}
	if v := strings.TrimSpace(os.Getenv(EnvAgentCredential)); v != "" {
		out["x-drover-agent-credential"] = v
	}
	return out
}

// ApplyGatewayEnv configures base URL and trace/MCP headers from the process environment.
func ApplyGatewayEnv(c *Client) {
	if c == nil {
		return
	}
	if u := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")); u != "" {
		c.SetBaseURL(u)
	}
	if h := GatewayRequestHeadersFromEnv(); len(h) > 0 {
		c.SetExtraHeaders(h)
	}
}
