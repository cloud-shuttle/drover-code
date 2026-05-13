// Package quality implements tools for verifying codebase quality.
package quality

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudshuttle/drover-code/internal/tools/toolutil"
)

type Review struct {
	WorkDir string
}

type reviewInput struct {
	Commands []string `json:"commands,omitempty"`
}

func (t *Review) Name() string { return "review_my_changes" }

func (t *Review) Description() string {
	return `Run comprehensive verification (tests, linting, static analysis, security) on recent changes.
**Strongly recommended** before committing or declaring any task complete.
Auto-detects project type by default.`
}

func (t *Review) InputSchema() json.RawMessage {
	return toolutil.NewSchema("object").
		Prop("commands", toolutil.NewSchema("array").
			Items(toolutil.NewSchema("string")).
			Desc("Optional: override with specific commands")).
		Build()
}

func (t *Review) NeedsPermission(rawInput json.RawMessage) bool { return true }

func (t *Review) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
	var inp reviewInput
	if len(rawInput) > 0 {
		_ = json.Unmarshal(rawInput, &inp)
	}

	cmds := inp.Commands
	if len(cmds) == 0 {
		cmds = t.autoDetectCommands()
	}

	if len(cmds) == 0 {
		return "No project type detected and no commands provided.", nil
	}

	var report strings.Builder
	report.WriteString("=== DROVER CODE QUALITY REVIEW ===\n\n")

	for _, cmd := range cmds {
		report.WriteString(fmt.Sprintf("▶ $ %s\n", cmd))
		output, err := t.runCommand(ctx, cmd)

		report.WriteString(output)
		report.WriteString("\n")

		if err != nil {
			report.WriteString("\n❌ VERIFICATION FAILED\n")
			report.WriteString("Fix the issues above. The agent will now attempt autonomous recovery.\n")
			return report.String(), nil
		}
		report.WriteString("✅ Passed\n\n")
	}

	report.WriteString("=== ALL CHECKS PASSED SUCCESSFULLY ===\n")
	return report.String(), nil
}

func (t *Review) autoDetectCommands() []string {
	var cmds []string

	if t.fileExists("go.mod") {
		cmds = append(cmds,
			"go build ./...",
			"go vet ./...",
			"go test ./... -short -race",
		)
		if t.hasCommand("gosec") {
			cmds = append(cmds, "gosec ./...")
		}
		if t.hasCommand("golangci-lint") {
			cmds = append(cmds, "golangci-lint run --timeout=4m")
		}
	} else if t.fileExists("package.json") {
		cmds = append(cmds, "CI=true npm test", "npm run lint --if-present", "npm audit --if-present")
	} else if t.fileExists("pyproject.toml") || t.fileExists("requirements.txt") {
		cmds = append(cmds, "pytest -q --tb=no")
	} else if t.fileExists("Makefile") {
		cmds = append(cmds, "make test")
	}

	return cmds
}

func (t *Review) fileExists(name string) bool {
	_, err := os.Stat(filepath.Join(t.WorkDir, name))
	return err == nil
}

func (t *Review) hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func (t *Review) runCommand(ctx context.Context, cmdStr string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
	cmd.Dir = t.WorkDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String() + "\n" + stderr.String()

	return toolutil.Truncate(strings.TrimSpace(output)), err
}
