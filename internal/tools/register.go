// Package tools provides the tool Registry and the Register function that
// wires all tool implementations into it.
package tools

import (
	"os"

	fspkg "github.com/cloudshuttle/drover-code/internal/tools/fs"
	gitpkg "github.com/cloudshuttle/drover-code/internal/tools/git"
	searchpkg "github.com/cloudshuttle/drover-code/internal/tools/search"
	shellpkg "github.com/cloudshuttle/drover-code/internal/tools/shell"
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
	r.Register(&gitpkg.Commit{WorkDir: workDir})
	r.Register(&gitpkg.Push{WorkDir: workDir})
	r.Register(&gitpkg.CreateBranch{WorkDir: workDir})

	// ── Web ──────────────────────────────────────────────────────────────────
	r.Register(webpkg.NewFetch())
}

