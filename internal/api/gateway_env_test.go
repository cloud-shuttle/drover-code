package api

import (
	"os"
	"testing"
)

func TestGatewayRequestHeadersFromEnv(t *testing.T) {
	t.Setenv(EnvGatewayDimAgentJobID, "aj-1")
	t.Setenv(EnvGatewayDimCustomerID, "cust-1")
	t.Setenv(EnvGatewayDimUser, "user-sub")
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
	if got := c.extraHeaders["x-bf-dim-user"]; got != "user-sub" {
		t.Fatalf("user header = %q", got)
	}
	if got := c.extraHeaders["x-drover-agent-credential"]; got != "agent-job-tok" {
		t.Fatalf("agent credential header = %q", got)
	}
	if got := c.extraHeaders["x-bf-vk"]; got != "sk-bf-test" {
		t.Fatalf("virtual key header = %q", got)
	}
}

func TestApplyGatewayEnv_OpenAI(t *testing.T) {
	t.Setenv(EnvLLMProvider, "openai")
	t.Setenv(EnvOpenAIBaseURL, "https://gateway.example/v1")
	t.Setenv(EnvOpenAIAPIKey, "sk-bf-agent")
	t.Setenv(EnvGatewayDimAgentJobID, "aj-9")
	t.Setenv(EnvGatewayDimCustomerID, "cust-9")
	t.Setenv(EnvGatewayDimUser, "user-9")
	t.Setenv(EnvAgentCredential, "agent-job-tok")
	os.Unsetenv(EnvAnthropicBaseURL)

	c := NewClientFromEnv("openai/gpt-4o-mini")
	if c.Protocol() != ProtocolOpenAI {
		t.Fatalf("protocol = %q", c.Protocol())
	}
	if c.baseURL != "https://gateway.example/v1" {
		t.Fatalf("baseURL = %q", c.baseURL)
	}
	if got := c.extraHeaders["x-bf-vk"]; got != "sk-bf-agent" {
		t.Fatalf("virtual key header = %q", got)
	}
	if got := c.extraHeaders["x-bf-dim-agent-job-id"]; got != "aj-9" {
		t.Fatalf("agent job id header = %q", got)
	}
	if got := c.extraHeaders["x-bf-dim-customer-id"]; got != "cust-9" {
		t.Fatalf("customer id header = %q", got)
	}
	if got := c.extraHeaders["x-bf-dim-user"]; got != "user-9" {
		t.Fatalf("user header = %q", got)
	}
	if got := c.extraHeaders["x-drover-agent-credential"]; got != "agent-job-tok" {
		t.Fatalf("agent credential header = %q", got)
	}
}

func TestGatewayRequestHeadersFromEnv_noVKWithoutGatewayBaseURL(t *testing.T) {
	t.Setenv(EnvGatewayDimAgentJobID, "aj-1")
	t.Setenv("ANTHROPIC_API_KEY", "sk-bf-test")
	os.Unsetenv(EnvAnthropicBaseURL)
	os.Unsetenv(EnvOpenAIBaseURL)
	os.Unsetenv(EnvLLMProvider)

	if got := GatewayRequestHeadersFromEnv()["x-bf-vk"]; got != "" {
		t.Fatalf("expected no x-bf-vk without gateway base URL, got %q", got)
	}
}

func TestGatewayRequestHeadersFromEnv_empty(t *testing.T) {
	os.Unsetenv(EnvGatewayDimAgentJobID)
	os.Unsetenv(EnvGatewayDimCustomerID)
	os.Unsetenv(EnvGatewayDimUser)
	os.Unsetenv(EnvAgentCredential)
	os.Unsetenv(EnvOpenAIBaseURL)
	os.Unsetenv(EnvAnthropicBaseURL)
	if len(GatewayRequestHeadersFromEnv()) != 0 {
		t.Fatal("expected no headers")
	}
}

func TestGatewayRequestHeadersFromEnv_userDimWithoutHostedMarkers(t *testing.T) {
	os.Unsetenv(EnvGatewayDimAgentJobID)
	os.Unsetenv(EnvGatewayDimCustomerID)
	os.Unsetenv(EnvAgentCredential)
	os.Unsetenv(EnvOpenAIBaseURL)
	os.Unsetenv(EnvAnthropicBaseURL)
	t.Setenv(EnvGatewayDimUser, "user-sub")

	got := GatewayRequestHeadersFromEnv()
	if got["x-bf-dim-user"] != "user-sub" {
		t.Fatalf("user header = %q", got["x-bf-dim-user"])
	}
	if _, ok := got["x-bf-dim-customer-id"]; ok {
		t.Fatal("customer-id should stay unset: user dim is not a hosted-worker marker")
	}
	if _, ok := got["x-bf-dim-agent-job-id"]; ok {
		t.Fatal("agent-job-id should stay unset")
	}
}

func TestGatewayRequestHeadersFromEnv_blankUserIgnored(t *testing.T) {
	t.Setenv(EnvGatewayDimUser, "   ")
	os.Unsetenv(EnvGatewayDimAgentJobID)
	os.Unsetenv(EnvGatewayDimCustomerID)
	os.Unsetenv(EnvAgentCredential)
	os.Unsetenv(EnvOpenAIBaseURL)
	os.Unsetenv(EnvAnthropicBaseURL)
	if _, ok := GatewayRequestHeadersFromEnv()["x-bf-dim-user"]; ok {
		t.Fatal("blank user dim should be omitted")
	}
}
