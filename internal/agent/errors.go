package agent

import "errors"

// ErrTokenBudgetExceeded is returned when DROVER_CODE_MAX_TOKENS is exceeded
// for a headless session (Phase 5). Only cumulative assistant output tokens
// count; input/context tokens from the API are excluded.
var ErrTokenBudgetExceeded = errors.New("drover-code: session output token budget exceeded")
