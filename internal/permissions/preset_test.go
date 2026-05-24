package permissions

import (
	"slices"
	"testing"
)

func TestMergeUnikernelPreset_defaults(t *testing.T) {
	allow, deny := MergeUnikernelPreset(nil, nil)
	if !slices.Contains(allow, "read_file") || !slices.Contains(allow, "git_commit") || !slices.Contains(allow, "review_my_changes") {
		t.Fatalf("missing expected allows: %v", allow)
	}
	if slices.Contains(allow, "git_push") {
		t.Fatal("git_push must not be allowlisted by default")
	}
	if !slices.Contains(deny, "git_push") {
		t.Fatalf("expected git_push denied, got %v", deny)
	}
	for _, tool := range []string{"ukc_create", "ukc_exec", "ukc_delete", "ukc_delete_all", "ukc_list"} {
		if slices.Contains(allow, tool) {
			t.Fatalf("%s must not be allowlisted for sandbox agent jobs", tool)
		}
		if !slices.Contains(deny, tool) {
			t.Fatalf("expected %s denied, got deny=%v", tool, deny)
		}
	}
}

func TestMergeUnikernelPreset_denyWins(t *testing.T) {
	allow, deny := MergeUnikernelPreset([]string{"git_push"}, nil)
	if slices.Contains(allow, "git_push") {
		t.Fatal("config allow must not override preset deny")
	}
	if !slices.Contains(deny, "git_push") {
		t.Fatalf("deny: %v", deny)
	}
}

func TestMergeUnikernelPreset_extraAllowAndDeny(t *testing.T) {
	allow, deny := MergeUnikernelPreset([]string{"custom_tool"}, []string{"web_fetch"})
	if !slices.Contains(allow, "custom_tool") {
		t.Fatal("expected extra allow")
	}
	if slices.Contains(allow, "web_fetch") {
		t.Fatal("web_fetch must be stripped when denied")
	}
	if !slices.Contains(deny, "web_fetch") {
		t.Fatalf("deny: %v", deny)
	}
}
