package main

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"
)

// headlessTimeout wraps ctx with a wall-clock deadline when DROVER_CODE_TIMEOUT_SECS
// is set (>0). The cancel function must be called to release the timer.
func headlessTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	s := strings.TrimSpace(os.Getenv("DROVER_CODE_TIMEOUT_SECS"))
	if s == "" {
		return ctx, func() {}
	}
	sec, err := strconv.Atoi(s)
	if err != nil || sec <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(sec)*time.Second)
}

// headlessMaxSessionTokens returns DROVER_CODE_MAX_TOKENS (0 = disabled).
// The cap applies to cumulative assistant output tokens only, not API input/context.
func headlessMaxSessionTokens() int {
	s := strings.TrimSpace(os.Getenv("DROVER_CODE_MAX_TOKENS"))
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
