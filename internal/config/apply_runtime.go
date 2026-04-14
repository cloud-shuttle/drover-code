package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/agent"
	"github.com/cloudshuttle/drover-code/internal/convo"
)

// ApplyConvoHeuristics sets context limit and chars-per-token on mgr from
// merged settings, with DROVER_CODE_CONTEXT_LIMIT_EST and
// DROVER_CODE_CHARS_PER_TOKEN env overrides (same semantics as the CLI).
func ApplyConvoHeuristics(mgr *convo.Manager, s Settings) {
	n := s.ContextLimitEstimate
	if v := envIntPositive("DROVER_CODE_CONTEXT_LIMIT_EST"); v > 0 {
		n = v
	}
	if n > 0 {
		mgr.SetContextLimit(n)
	}

	c := s.CharsPerTokenEstimate
	if v := envIntPositive("DROVER_CODE_CHARS_PER_TOKEN"); v > 0 {
		c = v
	}
	if c > 0 {
		mgr.SetCharsPerToken(c)
	}
}

// ApplyAgentLoopOptions applies workflow flags from merged settings (e.g. auto-compaction).
func ApplyAgentLoopOptions(loop *agent.Loop, s Settings) {
	loop.ApplyWorkflowSettings(EffectiveDisableAutoCompaction(s))
}

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
