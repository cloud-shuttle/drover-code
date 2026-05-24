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
	"review_my_changes",
	"web_fetch",
}

// unikernelDenyTools is always denied for the unikernel preset unless the
// merged deny list is the only source — deny wins over config allows.
var unikernelDenyTools = []string{
	"git_push",
	"ukc_create",
	"ukc_exec",
	"ukc_delete",
	"ukc_delete_all",
	"ukc_list",
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

// IntersectWithApproved further restricts an allowlist to only the tools explicitly
// approved for a governed hosted job (via DROVER_APPROVED_TOOLS and
// DROVER_APPROVED_MCP_CLIENTS environment variables).
//
// This is used for real UKC/Kraftcloud workers to enforce the Muster-approved
// allowlist as defense-in-depth inside the worker, in addition to the Gateway/Guard layer.
func IntersectWithApproved(allow, approvedTools, approvedClients []string) []string {
	if len(approvedTools) == 0 && len(approvedClients) == 0 {
		return allow
	}

	approved := make(map[string]bool, len(approvedTools)+len(approvedClients))
	for _, t := range approvedTools {
		t = strings.TrimSpace(t)
		if t != "" {
			approved[t] = true
		}
	}
	for _, c := range approvedClients {
		c = strings.TrimSpace(c)
		if c != "" {
			approved[c] = true
		}
	}

	if len(approved) == 0 {
		return allow
	}

	result := make([]string, 0, len(allow))
	for _, t := range allow {
		t = strings.TrimSpace(t)
		if t != "" && approved[t] {
			result = append(result, t)
		}
	}
	return result
}
