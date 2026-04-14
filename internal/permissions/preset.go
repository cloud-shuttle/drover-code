package permissions

import (
	"sort"
	"strings"
)

// PresetUnikernel is the value for DROVER_CODE_PERMISSION_PRESET that enables
// allowlist-style permissions suited to batch workers (design § Phase 3).
const PresetUnikernel = "unikernel"

// unikernelAllowTools is the default allowlist: full workspace tooling except
// git_push (often owned by the orchestrator). Paths are still constrained by
// each tool's WorkDir.
var unikernelAllowTools = []string{
	"read_file",
	"write_file",
	"edit_file",
	"list_directory",
	"file_info",
	"bash",
	"glob",
	"grep",
	"git_status",
	"git_diff",
	"git_log",
	"git_add",
	"git_commit",
	"git_create_branch",
	"web_fetch",
}

// unikernelDenyTools is always denied for the unikernel preset unless the
// merged deny list is the only source — deny wins over config allows.
var unikernelDenyTools = []string{
	"git_push",
}

// MergeUnikernelPreset combines the built-in unikernel allow/deny lists with
// settings.AllowedTools / DeniedTools from config. Deny always wins: any tool
// in the merged deny set is removed from the allow set and is rejected by the
// engine.
func MergeUnikernelPreset(configAllowed, configDenied []string) (allowed, denied []string) {
	allow := make(map[string]bool, len(unikernelAllowTools)+len(configAllowed))
	for _, t := range unikernelAllowTools {
		allow[strings.TrimSpace(t)] = true
	}
	for _, t := range configAllowed {
		t = strings.TrimSpace(t)
		if t != "" {
			allow[t] = true
		}
	}

	deny := make(map[string]bool, len(unikernelDenyTools)+len(configDenied))
	for _, t := range unikernelDenyTools {
		deny[strings.TrimSpace(t)] = true
	}
	for _, t := range configDenied {
		t = strings.TrimSpace(t)
		if t != "" {
			deny[t] = true
		}
	}

	for t := range deny {
		delete(allow, t)
	}

	allowed = keysSorted(allow)
	denied = keysSorted(deny)
	return allowed, denied
}

func keysSorted(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
