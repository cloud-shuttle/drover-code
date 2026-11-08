package api

import (
	"os"
	"strings"
)

const (
	EnvLLMProvider     = "DROVER_LLM_PROVIDER"
	EnvOpenAIBaseURL   = "OPENAI_BASE_URL"
	EnvOpenAIAPIKey    = "OPENAI_API_KEY"
	EnvOpenAIModel     = "OPENAI_MODEL"
	EnvSGLANGAPIKey    = "SGLANG_API_KEY"
	EnvServedModelName = "SERVED_MODEL_NAME"
)

// ProviderFromEnv selects the LLM wire format.
//
//   - DROVER_LLM_PROVIDER=openai|openai-compat|sglang → OpenAI Chat Completions
//   - DROVER_LLM_PROVIDER=anthropic → Anthropic Messages
//   - otherwise, OPENAI_BASE_URL set → OpenAI; else Anthropic
func ProviderFromEnv() Protocol {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvLLMProvider))) {
	case "openai", "openai-compat", "sglang":
		return ProtocolOpenAI
	case "anthropic":
		return ProtocolAnthropic
	}
	if strings.TrimSpace(os.Getenv(EnvOpenAIBaseURL)) != "" {
		return ProtocolOpenAI
	}
	return ProtocolAnthropic
}

// APIKeyFromEnv returns the API key for the given protocol.
// OpenAI accepts OPENAI_API_KEY, then SGLANG_API_KEY (Akamai / SGLang deploy kit).
func APIKeyFromEnv(p Protocol) string {
	if p == ProtocolOpenAI {
		if v := strings.TrimSpace(os.Getenv(EnvOpenAIAPIKey)); v != "" {
			return v
		}
		return strings.TrimSpace(os.Getenv(EnvSGLANGAPIKey))
	}
	if v := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN"))
}

// ModelFromEnv returns a model override for the protocol, or empty if unset.
func ModelFromEnv(p Protocol) string {
	if p == ProtocolOpenAI {
		if v := strings.TrimSpace(os.Getenv(EnvOpenAIModel)); v != "" {
			return v
		}
		return strings.TrimSpace(os.Getenv(EnvServedModelName))
	}
	return strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL"))
}

// NewClientFromEnv builds a Client from process environment and the given model id.
func NewClientFromEnv(model string) *Client {
	p := ProviderFromEnv()
	key := APIKeyFromEnv(p)
	var c *Client
	if p == ProtocolOpenAI {
		c = NewOpenAIClient(key, model)
	} else {
		c = NewClient(key, model)
	}
	ApplyGatewayEnv(c)
	return c
}
