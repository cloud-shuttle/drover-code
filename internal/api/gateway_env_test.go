package api

import (
	"os"
	"testing"
)

func TestGatewayRequestHeadersFromEnv(t *testing.T) {
	t.Setenv(EnvGatewayDimAgentJobID, "aj-1")
	t.Setenv(EnvGatewayDimCustomerID, "cust-1")
	t.Setenv(EnvAgentCredential, "agent-job-tok")
	t.Setenv(EnvAnthropicBaseURL, "http://gateway:8080/anthropic")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "sk-bf-test")

	c := NewClient("sk-bf-test", "m")
	ApplyGatewayEnv(c)
	if c.baseURL != "http://gateway:8080/anthropic" {
		t.Fatalf("baseURL = %q", c.baseURL)
	}
	if got := c.extraHeaders["x-bf-dim-agent-job-id"]; got != "aj-1" {
		t.Fatalf("agent job id header = %q", got)
	}
	if got := c.extraHeaders["x-bf-dim-customer-id"]; got != "cust-1" {
		t.Fatalf("customer id header = %q", got)
	}
	if got := c.extraHeaders["x-drover-agent-credential"]; got != "agent-job-tok" {
		t.Fatalf("agent credential header = %q", got)
	}
	if got := c.extraHeaders["x-bf-vk"]; got != "sk-bf-test" {
		t.Fatalf("virtual key header = %q", got)
	}
}

func TestGatewayRequestHeadersFromEnv_noVKWithoutGatewayBaseURL(t *testing.T) {
	t.Setenv(EnvGatewayDimAgentJobID, "aj-1")
	t.Setenv("ANTHROPIC_API_KEY", "sk-bf-test")
	os.Unsetenv(EnvAnthropicBaseURL)

	if got := GatewayRequestHeadersFromEnv()["x-bf-vk"]; got != "" {
		t.Fatalf("expected no x-bf-vk without gateway base URL, got %q", got)
	}
}

func TestGatewayRequestHeadersFromEnv_empty(t *testing.T) {
	os.Unsetenv(EnvGatewayDimAgentJobID)
	os.Unsetenv(EnvGatewayDimCustomerID)
	os.Unsetenv(EnvAgentCredential)
	if len(GatewayRequestHeadersFromEnv()) != 0 {
		t.Fatal("expected no headers")
	}
}
