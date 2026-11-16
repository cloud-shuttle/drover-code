package agent

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/internal/tools"
	fspkg "github.com/cloudshuttle/drover-code/internal/tools/fs"
	searchpkg "github.com/cloudshuttle/drover-code/internal/tools/search"
)

const (
	exploreMaxTurns = 8
	exploreMaxRunes = 8_000
)

const exploreSystemPrompt = `You are a read-only explore agent. Search and read the working tree to answer the task.
You may only use read_file, list_directory, file_info, glob, and grep.
Do not edit files, run commands, or claim you changed anything.
End with a short map: each path on its own line, and one clause on why it matters.
Do not paste large file bodies. Use line ranges when a file is long.`

// Explore runs one read-only search and returns the child's final text.
// The child has its own conversation. Nothing it reads is written to the caller.
// ponytail: explore stays read-only; delegate is the separate coding loop.
func Explore(ctx context.Context, driver InferenceDriver, workDir, task string) (string, error) {
	if driver == nil {
		return "", fmt.Errorf("explore: no model configured")
	}
	task = strings.TrimSpace(task)
	if task == "" {
		return "", fmt.Errorf("explore: task is empty")
	}

	reg := readOnlyRegistry(workDir)
	mgr := convo.NewManagerWithSystem(exploreSystemPrompt)
	loop := NewLoop(driver, mgr, NewDefaultToolExecutor(reg, nil, nil), reg, nil)
	loop.SetAutoCompaction(false)
	loop.SetMaxTurns(exploreMaxTurns)
	if err := loop.Run(ctx, task); err != nil {
		return "", fmt.Errorf("explore: %w", err)
	}
	out := strings.TrimSpace(loop.LastOutput())
	if out == "" {
		return "", fmt.Errorf("explore: empty result")
	}
	return clipRunes(out, exploreMaxRunes), nil
}

func readOnlyRegistry(workDir string) *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(&fspkg.ReadFile{WorkDir: workDir})
	reg.Register(&fspkg.ListDirectory{WorkDir: workDir})
	reg.Register(&fspkg.FileInfo{WorkDir: workDir})
	reg.Register(&searchpkg.Glob{WorkDir: workDir})
	reg.Register(&searchpkg.Grep{WorkDir: workDir})
	return reg
}

func clipRunes(s string, n int) string {
	if n < 1 || utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for k := range s {
		if i == n-1 {
			return s[:k] + "…"
		}
		i++
	}
	return s
}
