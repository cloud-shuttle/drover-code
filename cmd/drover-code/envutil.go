package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// envTruthy reports whether an environment variable is set to a common
// affirmative value (1, true, yes, on, y). Trims ASCII whitespace.
func envTruthy(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
	case "1", "true", "yes", "on", "y":
		return true
	default:
		return false
	}
}

// envIntPositive returns n>0 when the env var parses as a positive int; otherwise 0.
func envIntPositive(key string) int {
	s := strings.TrimSpace(os.Getenv(key))
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// anthropicAPIKey returns the first non-empty API key from the environment.
// It checks Anthropic, Gemini, and OpenAI keys to support API gateways.
func anthropicAPIKey() string {
	keys := []string{
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_AUTH_TOKEN",
		"GEMINI_API_KEY",
		"OPENAI_API_KEY",
	}
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func requireAnthropicAPIKey() string {
	if k := anthropicAPIKey(); k != "" {
		return k
	}
	fmt.Fprintln(os.Stderr, "error: set ANTHROPIC_API_KEY, GEMINI_API_KEY, or OPENAI_API_KEY")
	os.Exit(2)
	return ""
}
