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

// anthropicAPIKey returns the first non-empty of ANTHROPIC_API_KEY or
// ANTHROPIC_AUTH_TOKEN (Moonshot/Kimi and some other shims use the latter).
func anthropicAPIKey() string {
	if v := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")); v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv("ANTHROPIC_AUTH_TOKEN"))
}

func requireAnthropicAPIKey() string {
	if k := anthropicAPIKey(); k != "" {
		return k
	}
	fmt.Fprintln(os.Stderr, "error: set ANTHROPIC_API_KEY or ANTHROPIC_AUTH_TOKEN")
	os.Exit(2)
	return ""
}
