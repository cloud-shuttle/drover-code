package main

import (
	"testing"
)

func TestEnvTruthy(t *testing.T) {
	t.Setenv("X", "1")
	if !envTruthy("X") {
		t.Fatal("1")
	}
	t.Setenv("X", "true")
	if !envTruthy("X") {
		t.Fatal("true")
	}
	t.Setenv("X", " TRUE ")
	if !envTruthy("X") {
		t.Fatal("TRUE")
	}
	t.Setenv("X", "")
	if envTruthy("X") {
		t.Fatal("empty")
	}
	t.Setenv("X", "0")
	if envTruthy("X") {
		t.Fatal("0")
	}
}

func TestEnvIntPositive(t *testing.T) {
	t.Setenv("N", "")
	if envIntPositive("N") != 0 {
		t.Fatal("empty")
	}
	t.Setenv("N", "0")
	if envIntPositive("N") != 0 {
		t.Fatal("zero")
	}
	t.Setenv("N", "-3")
	if envIntPositive("N") != 0 {
		t.Fatal("negative")
	}
	t.Setenv("N", " 42 ")
	if envIntPositive("N") != 42 {
		t.Fatalf("got %d", envIntPositive("N"))
	}
}

func TestAnthropicAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	if g := anthropicAPIKey(); g != "" {
		t.Fatalf("empty: got %q", g)
	}

	t.Setenv("ANTHROPIC_AUTH_TOKEN", "  moonshot  ")
	t.Setenv("ANTHROPIC_API_KEY", "")
	if g := anthropicAPIKey(); g != "moonshot" {
		t.Fatalf("auth token: got %q", g)
	}

	t.Setenv("ANTHROPIC_API_KEY", "anthropic-wins")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "other")
	if g := anthropicAPIKey(); g != "anthropic-wins" {
		t.Fatalf("api key precedence: got %q", g)
	}
}
