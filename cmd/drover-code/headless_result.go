package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/agent"
)

const headlessResultSchemaVersion = 1

// headlessResultV1 is written to --result-json / DROVER_CODE_RESULT_PATH (design § Phase 4).
type headlessResultV1 struct {
	SchemaVersion int `json:"schema_version"`
	OK            bool   `json:"ok"`
	Branch        string `json:"branch,omitempty"`
	CommitSHA     string `json:"commit_sha,omitempty"`
	TestsPassed   *bool  `json:"tests_passed,omitempty"`
	PRURL         string `json:"pr_url,omitempty"`
	Error         string `json:"error,omitempty"`
	TurnsUsed     int    `json:"turns_used,omitempty"`
	TokensInput   int    `json:"tokens_input,omitempty"`
	TokensOutput  int    `json:"tokens_output,omitempty"`
	TokensUsed    int    `json:"tokens_used,omitempty"`
	PostHookOwner string `json:"post_hook_owner,omitempty"`
}

func writeHeadlessResultFile(workDir string, loop *agent.Loop, ok bool, sumTurns int, errMsg string) {
	p := headlessResultPath()
	if p == "" {
		return
	}
	in, out := 0, 0
	if loop != nil {
		in = loop.SessionInputTokens()
		out = loop.SessionOutputTokens()
	}
	r := buildHeadlessResult(workDir, ok, errMsg, sumTurns, in, out)
	if err := writeHeadlessResultAtomic(p, r); err != nil {
		fmt.Fprintf(os.Stderr, "headless: write result file: %v\n", err)
	}
}

func headlessResultPath() string {
	if s := strings.TrimSpace(startupFlags.ResultJSON); s != "" {
		return s
	}
	return strings.TrimSpace(os.Getenv("DROVER_CODE_RESULT_PATH"))
}

func gitWorkspaceHead(workDir string) (branch, sha string) {
	run := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", workDir}, args...)...)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = nil
		if err := cmd.Run(); err != nil {
			return ""
		}
		return strings.TrimSpace(out.String())
	}
	branch = run("rev-parse", "--abbrev-ref", "HEAD")
	sha = run("rev-parse", "HEAD")
	if branch == "HEAD" {
		// detached HEAD — keep SHA, clear misleading branch name
		branch = ""
	}
	return branch, sha
}

func writeHeadlessResultAtomic(path string, r headlessResultV1) error {
	r.SchemaVersion = headlessResultSchemaVersion
	if r.TokensUsed == 0 && (r.TokensInput > 0 || r.TokensOutput > 0) {
		r.TokensUsed = r.TokensInput + r.TokensOutput
	}

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".drover-code-result-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_, werr := tmp.Write(data)
	if syncErr := tmp.Sync(); syncErr != nil && werr == nil {
		werr = syncErr
	}
	if closeErr := tmp.Close(); closeErr != nil && werr == nil {
		werr = closeErr
	}
	if werr != nil {
		_ = os.Remove(tmpPath)
		return werr
	}
	return os.Rename(tmpPath, path)
}

func buildHeadlessResult(workDir string, ok bool, errMsg string, sumTurns, inTok, outTok int) headlessResultV1 {
	r := headlessResultV1{
		OK:           ok,
		TurnsUsed:    sumTurns,
		TokensInput:  inTok,
		TokensOutput: outTok,
		TokensUsed:   inTok + outTok,
	}
	if errMsg != "" {
		r.Error = errMsg
	}
	if b, ok := envOptionalBool("DROVER_CODE_TESTS_PASSED"); ok {
		r.TestsPassed = &b
	}
	if s := strings.TrimSpace(os.Getenv("DROVER_CODE_PR_URL")); s != "" {
		r.PRURL = s
	}
	if s := strings.TrimSpace(os.Getenv("DROVER_CODE_POST_HOOK_OWNER")); s != "" {
		r.PostHookOwner = s
	}
	br, sha := gitWorkspaceHead(workDir)
	r.Branch = br
	r.CommitSHA = sha
	return r
}

func envOptionalBool(key string) (val bool, set bool) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return false, false
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on", "y":
		return true, true
	case "0", "false", "no", "off", "n":
		return false, true
	default:
		return false, false
	}
}
