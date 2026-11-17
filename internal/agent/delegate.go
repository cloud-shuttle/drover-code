package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudshuttle/drover-code/internal/convo"
	"github.com/cloudshuttle/drover-code/internal/tools"
	fspkg "github.com/cloudshuttle/drover-code/internal/tools/fs"
	gitpkg "github.com/cloudshuttle/drover-code/internal/tools/git"
	searchpkg "github.com/cloudshuttle/drover-code/internal/tools/search"
	shellpkg "github.com/cloudshuttle/drover-code/internal/tools/shell"
	webpkg "github.com/cloudshuttle/drover-code/internal/tools/web"
)

const (
	delegateMaxTurns = 16
	delegateMaxRunes = 8_000
)

// DelegateOpts configures the child coding loop.
type DelegateOpts struct {
	Sandbox         bool
	AllowBackground bool
	Gopls           bool
}

// Delegate runs one coding task and returns the child's final text.
// The child has its own conversation. The parent already approved the call,
// so the child does not prompt again.
// ponytail: one level deep; a child cannot start another delegate.
func Delegate(ctx context.Context, driver InferenceDriver, workDir, task string, opts DelegateOpts) (string, error) {
	if driver == nil {
		return "", fmt.Errorf("delegate: no model configured")
	}
	task = strings.TrimSpace(task)
	if task == "" {
		return "", fmt.Errorf("delegate: task is empty")
	}
	if strings.TrimSpace(workDir) == "" {
		return "", fmt.Errorf("delegate: working directory is empty")
	}

	reg := delegateRegistry(workDir, opts)
	if b, ok := reg.Get("bash").(*shellpkg.Bash); ok {
		defer b.StopAll()
	}
	mgr := convo.NewManagerWithSystem(delegatePrompt(opts))
	loop := NewLoop(driver, mgr, NewDefaultToolExecutor(reg, nil, nil), reg, nil)
	loop.SetAutoCompaction(false)
	loop.SetMaxTurns(delegateMaxTurns)
	if err := loop.Run(ctx, task); err != nil {
		return "", fmt.Errorf("delegate: %w", err)
	}
	out := strings.TrimSpace(loop.LastOutput())
	if out == "" {
		return "", fmt.Errorf("delegate: empty result")
	}
	return clipRunes(out, delegateMaxRunes), nil
}

func delegatePrompt(opts DelegateOpts) string {
	prompt := `You are a coding subagent. Complete the task in this working tree.
You can read, edit, and run bash. You cannot ask the user a question and you cannot start another subagent.
End with a short result: what changed, what you ran, and anything left unfinished.
Do not paste large file bodies.`
	if opts.Sandbox {
		prompt += "\nBash is sandboxed: it can write only inside this tree and a private temp directory, and it cannot use the network."
	}
	return prompt
}

func delegateRegistry(workDir string, opts DelegateOpts) *tools.Registry {
	reg := tools.NewRegistry()
	reg.Register(&fspkg.ReadFile{WorkDir: workDir})
	reg.Register(&fspkg.WriteFile{WorkDir: workDir, Gopls: opts.Gopls})
	reg.Register(&fspkg.EditFile{WorkDir: workDir, Gopls: opts.Gopls})
	reg.Register(&fspkg.ListDirectory{WorkDir: workDir})
	reg.Register(&fspkg.FileInfo{WorkDir: workDir})
	reg.Register(&shellpkg.Bash{WorkDir: workDir, AllowBackground: opts.AllowBackground, Sandbox: opts.Sandbox})
	reg.Register(&searchpkg.Glob{WorkDir: workDir})
	reg.Register(&searchpkg.Grep{WorkDir: workDir})
	reg.Register(&gitpkg.Status{WorkDir: workDir})
	reg.Register(&gitpkg.Diff{WorkDir: workDir})
	reg.Register(&gitpkg.Log{WorkDir: workDir})
	reg.Register(&gitpkg.Add{WorkDir: workDir})
	reg.Register(&gitpkg.CreateBranch{WorkDir: workDir})
	reg.Register(webpkg.NewFetch())
	return reg
}
