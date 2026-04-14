package main

import (
	"context"
	"testing"
	"time"
)

func TestHeadlessTimeout(t *testing.T) {
	t.Setenv("DROVER_CODE_TIMEOUT_SECS", "0")
	ctx := context.Background()
	c, cancel := headlessTimeout(ctx)
	cancel()
	if deadline, ok := c.Deadline(); ok {
		t.Fatalf("expected no deadline, got %v", deadline)
	}

	t.Setenv("DROVER_CODE_TIMEOUT_SECS", "2")
	c, cancel = headlessTimeout(ctx)
	defer cancel()
	deadline, ok := c.Deadline()
	if !ok {
		t.Fatal("expected deadline")
	}
	if time.Until(deadline) > 3*time.Second || time.Until(deadline) < time.Second {
		t.Fatalf("unexpected deadline: %v", deadline)
	}
}

func TestHeadlessMaxSessionTokens(t *testing.T) {
	t.Setenv("DROVER_CODE_MAX_TOKENS", "")
	if n := headlessMaxSessionTokens(); n != 0 {
		t.Fatalf("got %d", n)
	}
	t.Setenv("DROVER_CODE_MAX_TOKENS", "4096")
	if n := headlessMaxSessionTokens(); n != 4096 {
		t.Fatalf("got %d", n)
	}
}
