// Package tools provides the tool Registry and the Register function that
// wires all tool implementations into it.
package tools

import (
	"fmt"
	"os"

	fspkg "github.com/cloudshuttle/drover-code/internal/tools/fs"
	gitpkg "github.com/cloudshuttle/drover-code/internal/tools/git"
	planningpkg "github.com/cloudshuttle/drover-code/internal/tools/planning"
	qualitypkg "github.com/cloudshuttle/drover-code/internal/tools/quality"
	searchpkg "github.com/cloudshuttle/drover-code/internal/tools/search"
	shellpkg "github.com/cloudshuttle/drover-code/internal/tools/shell"
	ukcpkg "github.com/cloudshuttle/drover-code/internal/tools/ukc"
	webpkg "github.com/cloudshuttle/drover-code/internal/tools/web"
)

// RegisterAll registers every built-in tool on the given Registry.
// workDir is the project root — tools use it to resolve relative paths and
// as the default working directory for shell commands.
func RegisterAll(r *Registry, workDir string) {
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	// ── File system ─────────────────────────────────────────────────────────
	r.Register(&fspkg.ReadFile{WorkDir: workDir})
	r.Register(&fspkg.WriteFile{WorkDir: workDir})
	r.Register(&fspkg.EditFile{WorkDir: workDir})
	r.Register(&fspkg.ListDirectory{WorkDir: workDir})
	r.Register(&fspkg.FileInfo{WorkDir: workDir})

	// ── Shell ────────────────────────────────────────────────────────────────
	r.Register(&shellpkg.Bash{WorkDir: workDir})

	// ── Search ───────────────────────────────────────────────────────────────
	r.Register(&searchpkg.Glob{WorkDir: workDir})
	r.Register(&searchpkg.Grep{WorkDir: workDir})

	// ── Git ──────────────────────────────────────────────────────────────────
	r.Register(&gitpkg.Status{WorkDir: workDir})
	r.Register(&gitpkg.Diff{WorkDir: workDir})
	r.Register(&gitpkg.Log{WorkDir: workDir})
	r.Register(&gitpkg.Add{WorkDir: workDir})
	r.Register(gitpkg.NewCommitTool(workDir))
	r.Register(gitpkg.NewPushTool(workDir))
	r.Register(&gitpkg.CreateBranch{WorkDir: workDir})
	r.Register(gitpkg.NewCreatePRTool(workDir))

	// ── Web ──────────────────────────────────────────────────────────────────
	r.Register(webpkg.NewFetch())

	// ── Quality / Review ─────────────────────────────────────────────────────
	r.Register(&qualitypkg.Review{WorkDir: workDir})

	// ── Planning & Memory ────────────────────────────────────────────────────
	r.Register(&planningpkg.WritePlan{WorkDir: workDir})

	// ── Unikraft Cloud (optional; requires UKC_TOKEN) ─────────────────────────
	registerUKCTools(r)
}

func registerUKCTools(r *Registry) {
	mgr, ok, err := ukcpkg.NewManagerFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: ukc tools disabled: %v\n", err)
		return
	}
	if !ok {
		return
	}
	r.Register(&ukcpkg.Create{M: mgr})
	r.Register(&ukcpkg.Exec{M: mgr})
	r.Register(&ukcpkg.Delete{M: mgr})
	r.Register(&ukcpkg.DeleteAll{M: mgr})
	r.Register(&ukcpkg.List{M: mgr})
	r.Register(&ukcpkg.BuildTemplate{M: mgr, Cache: mgr.Templates})
}
