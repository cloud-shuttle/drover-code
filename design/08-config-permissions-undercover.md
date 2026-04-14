# 08 — Config, Permission Engine, and Undercover Mode

**Packages:** `internal/config`, `internal/permissions`, `internal/undercover`  
**Files:** `config/loader.go`, `permissions/engine.go`, `undercover/undercover.go`  
**Depends on:** `internal/tools` (PermissionFunc type)  
**Depended on by:** `cmd/drover-code`, `internal/tui`

---

## Purpose

These three packages form the policy layer of drover-code. They answer three
distinct questions:

- **Config:** what are the user's preferences for this session/project?
- **Permissions:** is this specific tool call authorised?
- **Undercover:** does the model need to hide its AI identity?

They are separate packages because the concerns are separate. A tool call
flows through all three: config determines the permission mode, the permission
engine applies it to each call, and undercover mode shapes what the model
writes in commits and PRs.

---

## 1. Config Loader (`config/loader.go`)

### 1.1 The three-level hierarchy

```
~/.claude/settings.json          ← global (applies to all projects)
<workDir>/.claude/settings.json  ← project (committed, shared)
<workDir>/.claude/settings.local.json  ← local (gitignored, personal)
```

Each level can override the level below it. The merge is field-level: a
non-zero value at a higher priority level wins; a zero/empty value at a higher
level does not wipe out a lower-level value.

This matters in practice. Suppose the project sets:

```json
{ "allowedTools": ["read_file", "glob"] }
```

And the user's global config sets:

```json
{ "model": "claude-opus-4-6" }
```

After merging, the session has both `allowedTools` and `model` set. Neither
file needed to know about the other's settings.

### 1.2 Field-level merge

```go
func mergeInto(dst *Settings, src Settings) {
    if src.Model != ""           { dst.Model = src.Model }
    if src.PermissionMode != ""  { dst.PermissionMode = src.PermissionMode }
    if len(src.AllowedTools) > 0 { dst.AllowedTools = append(dst.AllowedTools, src.AllowedTools...) }
    if len(src.DeniedTools) > 0  { dst.DeniedTools  = append(dst.DeniedTools, src.DeniedTools...) }
    if src.MaxTokens != 0        { dst.MaxTokens = src.MaxTokens }
    if src.CoordinatorMode       { dst.CoordinatorMode = true }
    if src.DreamEnabled          { dst.DreamEnabled = true }
    if src.UndercoverMode != nil  { dst.UndercoverMode = src.UndercoverMode }
    for k, v := range src.Env {
        if dst.Env == nil { dst.Env = make(map[string]string) }
        dst.Env[k] = v
    }
}
```

Note that `AllowedTools` and `DeniedTools` **accumulate** across levels — they
are not replaced. This means the project can add tools to the allow list and
the global config can add different tools, and both sets are active. The final
list is the union.

`UndercoverMode` uses `*bool` (pointer to bool) rather than `bool`. This
distinguishes three states:
- `nil` — not set at this level (auto-detect)
- `&true` — explicitly enabled
- `&false` — explicitly disabled

A plain `bool` field can only distinguish `false` (default) and `true`. It
cannot express "I explicitly want auto-detection, overriding any lower-level
explicit setting." The pointer enables this. At merge time, a non-nil pointer
wins over nil regardless of its value.

### 1.3 CLAUDE.md injection

```go
func (l *Loader) loadClaudeMD() string {
    var files []string

    // Global: ~/.claude/CLAUDE.md (lowest priority)
    if home, err := os.UserHomeDir(); err == nil {
        if global := filepath.Join(home, ".claude", "CLAUDE.md"); fileExists(global) {
            files = append(files, global)
        }
    }

    // Walk upward from workDir to home
    dir := l.workDir
    home, _ := os.UserHomeDir()
    for {
        if c := filepath.Join(dir, "CLAUDE.md"); fileExists(c) {
            files = append(files, c)
        }
        parent := filepath.Dir(dir)
        if parent == dir || dir == home { break }
        dir = parent
    }

    // Concatenate in order: global first, then outermost to innermost
    // (later entries override earlier for human reading, not technically)
    ...
}
```

The walk direction is important. The files slice is built in order from global
→ repository root → project subdirectory, so the innermost (most specific)
instructions come last in the concatenated string. The model reads them in
order and the later instructions take precedence in context (models generally
give more weight to instructions that appear later).

**What goes in CLAUDE.md?** Project-specific conventions:

```markdown
# Project conventions

## Code style
- Use early returns over nested if statements
- Prefer table-driven tests
- All exported functions must have godoc comments

## Architecture
- HTTP handlers live in internal/handlers/
- Business logic in internal/service/
- Database queries in internal/store/

## Testing
- Run `go test -race ./...` before committing
- Integration tests require `TEST_DB_URL` to be set
```

This context is injected into every session's system prompt, so the model
understands project conventions without being told each time.

### 1.4 `Save()` — writing settings back

```go
func (l *Loader) Save(delta Settings) error {
    path := filepath.Join(l.projectDir, "settings.json")
    // 1. Read existing project settings
    // 2. Merge delta into existing
    // 3. Write back atomically
}
```

`Save` always writes to the **project level** (`.claude/settings.json`), never
the global level. This is correct for the `/permissions` slash command (which
persists rule changes for the current project) and for any other runtime
settings changes.

Writing to the user's global config from within a project would be surprising
and potentially disruptive to other projects. The user can always manually
edit `~/.claude/settings.json` for global changes.

### 1.5 Settings fields reference

| Field | Type | Default | Purpose |
|---|---|---|---|
| `model` | `string` | `""` | Override model string |
| `permissionMode` | `string` | `""` → "default" | `default`, `plan`, `bypassPermissions` |
| `allowedTools` | `[]string` | `[]` | Auto-approved tool names |
| `deniedTools` | `[]string` | `[]` | Always-denied tool names |
| `maxTokens` | `int` | `0` → 8096 | Per-request token cap |
| `coordinatorMode` | `bool` | `false` | Enable multi-agent coordinator |
| `dreamEnabled` | `bool` | `false` | Background memory consolidation |
| `undercoverMode` | `*bool` | `nil` → auto | Override repo visibility detection |
| `env` | `map[string]string` | `{}` | Extra env vars for bash tool |

### 1.6 Testing strategy

```go
// Three-level merge
global  := Settings{Model: "claude-opus-4-6"}
project := Settings{AllowedTools: []string{"read_file"}}
local   := Settings{PermissionMode: "plan"}

result := merge(global, project, local)
assert(t, result.Model == "claude-opus-4-6")
assert(t, result.AllowedTools == []string{"read_file"})
assert(t, result.PermissionMode == "plan")

// Higher level wins for scalar fields
global  = Settings{Model: "claude-sonnet-4-5"}
project = Settings{Model: "claude-opus-4-6"}  // project overrides global
result  = merge(global, project)
assert(t, result.Model == "claude-opus-4-6")

// Tool lists accumulate
global  = Settings{AllowedTools: []string{"read_file"}}
project = Settings{AllowedTools: []string{"glob"}}
result  = merge(global, project)
assert(t, len(result.AllowedTools) == 2)

// *bool nil vs explicit
trueVal := true
falseVal := false
global  = Settings{UndercoverMode: &trueVal}
project = Settings{UndercoverMode: &falseVal}  // project explicitly disables
result  = merge(global, project)
assert(t, *result.UndercoverMode == false)

// nil does not override explicit
global  = Settings{UndercoverMode: &trueVal}
project = Settings{}  // nil — not set
result  = merge(global, project)
assert(t, *result.UndercoverMode == true)  // global value preserved
```

---

## 2. Permission Engine (`permissions/engine.go`)

### 2.1 The rule priority chain

Every tool call that reports `NeedsPermission: true` passes through this chain:

```
1. ModeBypass?           → Allow (no further checks)
2. Config deny list?     → Deny
3. Persisted deny rule?  → Deny
4. Config allow list?    → Allow
5. Persisted allow rule? → Allow
6. Call promptFn         → blocks until user decides
```

The chain is checked in order and short-circuits at the first match. A tool in
the deny list is refused even if it's also in the allow list — deny wins.

### 2.2 Why deny before allow?

The deny-wins convention is the security-safe choice. If both a deny rule and
an allow rule match the same tool, something is wrong with the configuration.
Defaulting to allow when rules conflict would silently grant permissions the
user may not have intended. Defaulting to deny surfaces the conflict (the tool
is refused) and prompts the user to investigate.

This matches the principle of least privilege: when the policy is ambiguous,
restrict rather than permit.

### 2.3 Mode details

**`ModeDefault`** — the interactive mode. Every tool call that `NeedsPermission: true`
passes through the full chain. If no persisted rule matches, `promptFn` is
called and the user sees a permission prompt.

**`ModePlan`** — read-only tools are auto-approved; write/execute tools are
queued. The agent loop implements this by running the first pass with all tools
in the allow list, collecting the proposed operations, and presenting them as
a batch for the user to review before execution.

Note: `ModePlan` is partially implemented. The current implementation treats it
identically to `ModeDefault` — the batch approval UI is not yet built. The mode
is exposed in settings so users can configure it now, with the understanding
that the batch UI is coming.

**`ModeBypass`** — approves everything without checking. Used for:
- Worker agents in coordinator mode (coordinator made the permission decision)
- GitHub webhook runner (no interactive user)
- Headless piped mode (`tools.AllowAll` is used directly)

### 2.4 Persisted rules

```go
type Rule struct {
    Tool string   `json:"tool"`
    Kind RuleKind `json:"kind"`  // 0 = allow, 1 = deny
}
```

Rules are stored in `.claude/permissions.json`:

```json
[
    {"tool": "bash", "kind": 0},
    {"tool": "write_file", "kind": 0},
    {"tool": "dangerous_tool", "kind": 1}
]
```

### 2.5 `AlwaysAllow` creates a persisted rule

```go
func (e *Engine) Check(ctx context.Context, toolName string, input json.RawMessage) (tools.Decision, error) {
    // ... priority chain checks ...

    decision := e.promptFn(ctx, tools.PermissionRequest{...})

    if decision == tools.AlwaysAllow {
        e.addRule(Rule{Tool: toolName, Kind: RuleAllow})  // persist
    }

    return decision, nil
}
```

When the user responds `a` (always allow) to a permission prompt, the engine
immediately adds a persisted allow rule for that tool. Future calls to that
tool skip the prompt — they hit step 5 in the priority chain and are approved
without interaction.

The rule is written to disk atomically (temp file + rename). The next session
loads it and the approval persists across sessions for that project.

### 2.6 Deduplication

```go
func (e *Engine) addRule(r Rule) {
    e.mu.Lock()
    defer e.mu.Unlock()
    for _, existing := range e.rules {
        if existing.Tool == r.Tool && existing.Kind == r.Kind {
            return  // already exists
        }
    }
    e.rules = append(e.rules, r)
    _ = e.saveLocked()
}
```

Adding a duplicate rule is a no-op. Without this, repeated "always allow"
answers to the same tool would grow the rules file indefinitely. The rules
slice is small (typically < 20 entries) so linear search is fine.

### 2.7 `WrapPermitFn()` — the integration point

```go
func (e *Engine) WrapPermitFn() tools.PermissionFunc {
    return func(ctx context.Context, req tools.PermissionRequest) tools.Decision {
        d, _ := e.Check(ctx, req.ToolName, req.Input)
        return d
    }
}
```

`WrapPermitFn()` returns a `tools.PermissionFunc` that routes through the full
priority chain. This is what the agent loop receives — it replaces the naive
`tools.AllowAll` used in Phase 1.

The permission engine sits between the agent loop and the TUI's prompt function:

```
agent loop calls permitFn
    → WrapPermitFn → e.Check
        → priority chain (modes, lists, persisted rules)
        → if none match: call e.promptFn (the TUI prompt)
            → PermissionRequestEvent sent to TUI
            → user responds
            → decision returned
```

### 2.8 Thread safety

```go
type Engine struct {
    mu           sync.RWMutex
    mode         Mode
    rules        []Rule
    rulesPath    string
    promptFn     tools.PermissionFunc
    deniedTools  map[string]bool
    allowedTools map[string]bool
}
```

All state is behind `sync.RWMutex`. `Check()` acquires a read lock for the
priority chain checks. `addRule()` acquires a write lock to append and persist.

The `promptFn` call happens **outside** the lock. This is important: `promptFn`
blocks until the user responds (potentially for many seconds). Holding a lock
during a blocking call would prevent any other tool from being checked — even
read-only tools that would be instantly approved. Releasing the lock before the
prompt means concurrent read-only tool checks proceed while the user is thinking.

```go
func (e *Engine) Check(ctx context.Context, toolName string, input json.RawMessage) (tools.Decision, error) {
    e.mu.RLock()
    // ... fast priority chain checks ...
    e.mu.RUnlock()

    // promptFn called outside the lock
    decision := e.promptFn(ctx, tools.PermissionRequest{...})

    if decision == tools.AlwaysAllow {
        e.addRule(...)  // acquires write lock separately
    }
    return decision, nil
}
```

### 2.9 Input-aware permission decisions

The current implementation ignores the `input json.RawMessage` parameter in
`Check()`. It treats all calls to the same tool identically — either `bash`
is allowed or it isn't.

A future refinement would inspect the input to make finer-grained decisions:

```go
// Future: allow read-only bash but prompt for write operations
func bashNeedsPermission(input json.RawMessage) bool {
    var inp struct{ Command string `json:"command"` }
    json.Unmarshal(input, &inp)
    return looksDestructive(inp.Command)
}

func looksDestructive(cmd string) bool {
    destructivePatterns := []string{
        "rm ", "rmdir", "mv ", "cp ",
        "chmod ", "chown ", "sudo ",
        "curl.*|.*bash", "> /",
        "dd if=",
    }
    for _, p := range destructivePatterns {
        if matchesPattern(cmd, p) { return true }
    }
    return false
}
```

This is deliberately not implemented yet. Pattern matching on shell commands is
inherently fragile — `echo "rm -rf /"` matches but is harmless; `${cmd}` doesn't
match but could be anything. The `plan` mode (review all operations before
executing) is a better architectural answer.

### 2.10 Testing strategy

```go
// ModeBypass approves everything
engine := NewEngine(ModeBypass, nil, nil, "", tools.AllowAll)
d, _ := engine.Check(ctx, "bash", nil)
assert(t, d == tools.Allow)

// Config deny list overrides everything
engine = NewEngine(ModeDefault, nil, []string{"bash"}, "", tools.AllowAll)
d, _ = engine.Check(ctx, "bash", nil)
assert(t, d == tools.Deny)

// Config allow list skips prompt
prompted := false
engine = NewEngine(ModeDefault, []string{"read_file"}, nil, "",
    func(_ context.Context, _ tools.PermissionRequest) tools.Decision {
        prompted = true
        return tools.Allow
    })
d, _ = engine.Check(ctx, "read_file", nil)
assert(t, d == tools.Allow)
assert(t, !prompted)  // prompt was never called

// AlwaysAllow persists a rule
rulesFile := t.TempDir() + "/permissions.json"
prompted = false
callCount := 0
engine = NewEngine(ModeDefault, nil, nil, rulesFile,
    func(_ context.Context, _ tools.PermissionRequest) tools.Decision {
        callCount++
        return tools.AlwaysAllow
    })

// First call: prompts and persists
d, _ = engine.Check(ctx, "write_file", nil)
assert(t, d == tools.AlwaysAllow)
assert(t, callCount == 1)

// Second call: uses persisted rule, no prompt
d, _ = engine.Check(ctx, "write_file", nil)
assert(t, d == tools.Allow)  // Allow (not AlwaysAllow — that's only on first grant)
assert(t, callCount == 1)    // prompt not called again

// New engine loading same rules file
engine2 := NewEngine(ModeDefault, nil, nil, rulesFile, func(...) tools.Decision {
    t.Fatal("should not prompt after loading persisted rule")
    return tools.Deny
})
d, _ = engine2.Check(ctx, "write_file", nil)
assert(t, d == tools.Allow)  // loaded from file

// Deny wins over allow
engine = NewEngine(ModeDefault, []string{"bash"}, []string{"bash"}, "", tools.AllowAll)
d, _ = engine.Check(ctx, "bash", nil)
assert(t, d == tools.Deny)  // deny wins
```

---

## 3. Undercover Mode (`undercover/undercover.go`)

### 3.1 Background

From the leaked Claude Code source: when Anthropic employees use Claude Code
to contribute to public open-source repositories, the model must not reveal:

- Internal model codenames (Capybara, Tengu, Kestrel, etc.)
- Unreleased model version numbers
- Internal project names or tooling references
- That the commit or PR was written by an AI

This is enforced by injecting a system prompt fragment that instructs the model
to behave as a human developer.

In drover-code, this mode activates automatically when the current repository has
a public remote, and can be overridden via `undercoverMode: true/false` in
settings.

### 3.2 Detection algorithm

```go
func Detect(repoPath string) Status {
    remote, err := gitRemoteURL(repoPath)
    if err != nil || remote == "" {
        return Status{Active: false, Reason: "no git remote"}
    }

    remote = strings.ToLower(remote)

    if strings.Contains(remote, "github.com") {
        if isInternalDomain(remote) {
            return Status{Active: false, Reason: "internal github"}
        }
        return Status{Active: true, Reason: "public github: " + remote}
    }
    if strings.Contains(remote, "gitlab.com") ||
       strings.Contains(remote, "bitbucket.org") {
        return Status{Active: true, Reason: "public hosting: " + remote}
    }
    return Status{Active: false, Reason: "unrecognised remote"}
}

func gitRemoteURL(repoPath string) (string, error) {
    cmd := exec.Command("git", "remote", "get-url", "origin")
    cmd.Dir = repoPath
    var out bytes.Buffer
    cmd.Stdout = &out
    if err := cmd.Run(); err != nil {
        return "", err
    }
    return strings.TrimSpace(out.String()), nil
}
```

The algorithm is conservative: it defaults to "not active" when uncertain.
The cases where it activates are well-defined public hosting services.

### 3.3 Internal domain heuristics

```go
func isInternalDomain(url string) bool {
    internalPatterns := []string{
        "github.anthropic.com",
        "github.internal.",
        "github.corp.",
        "ghe.",        // common GitHub Enterprise prefix
    }
    for _, p := range internalPatterns {
        if strings.Contains(url, p) { return true }
    }
    return false
}
```

These patterns cover the most common GitHub Enterprise deployment conventions.
They are not exhaustive — a company could use any hostname for GitHub Enterprise.
The `settings.undercoverMode` override allows explicit control for cases the
heuristic misclassifies.

### 3.4 The system prompt fragment

```go
const SystemPromptFragment = `## UNDERCOVER MODE — CRITICAL

You are operating in a PUBLIC or OPEN-SOURCE repository.
Your commit messages, PR titles, PR bodies, and code comments
MUST NOT contain ANY Anthropic-internal information.

NEVER include:
- Internal model codenames (animal names: Capybara, Tengu, Kestrel, etc.)
- Unreleased model version numbers (e.g. opus-4-7, sonnet-4-8)
- Internal repository or project names
- Internal tooling references (Slack channels, short links, etc.)
- The phrase "Claude Code" or any indication that you are an AI assistant
- Co-Authored-By lines or any other AI attribution

Write commit messages and PR descriptions as a human developer would.
Do not blow your cover under any circumstances.`
```

The word "CRITICAL" and the ALL-CAPS "NEVER" are intentional. The model gives
more weight to imperatives than to polite requests. The specific examples
(Capybara, Tengu) anchor the instruction to concrete cases the model can
recognise.

"Do not blow your cover" is colloquial but effective — it frames the entire
instruction as a role constraint, not a list of rules to follow literally.

### 3.5 Injection into the system prompt

```go
func buildSystemPrompt(workDir, claudeMDInjection string, undercoverActive bool) string {
    var b strings.Builder
    b.WriteString(basePrompt(workDir))
    if undercoverActive {
        b.WriteString("\n\n")
        b.WriteString(undercover.SystemPromptFragment)
    }
    if claudeMDInjection != "" {
        b.WriteString("\n\n")
        b.WriteString(claudeMDInjection)
    }
    return b.String()
}
```

Undercover mode is injected **before** the CLAUDE.md content. This ordering
matters: the model encounters undercover instructions first, establishing the
constraint, and then reads project-specific instructions within that frame.
If a CLAUDE.md file happened to mention "generated by drover-code", undercover
mode having been established first would prevent the model from echoing that
in commits.

### 3.6 What undercover mode does NOT do

Undercover mode instructs the model. It does not:

- **Filter model outputs.** The commit messages the model generates are shown
  to the user before being committed (via the permission prompt). The user
  sees them and can reject them. There is no automated post-processing that
  strips AI-identifying phrases.

- **Prevent all AI attribution.** A developer reviewing the commit can still
  infer AI involvement from writing style, unusual speed of changes, or the
  drover-code footer on GitHub @drover-code comments (which explicitly attributes).
  Undercover mode is about not *advertising* AI involvement in the git history
  itself, not about making AI involvement undetectable.

- **Apply to the TUI conversation.** The model can freely mention that it is
  Claude in conversation with the user. Undercover mode only applies to
  content that ends up in the repository (commits, PR descriptions, review
  comments).

### 3.7 GitHub webhook integration

The GitHub webhook runner always operates in undercover mode for the
conversation content (commit messages, PR comments). However, the `web_fetch`
and response formatting add an explicit attribution footer:

```
---
_via [drover-code](https://github.com/cloudshuttle/drover-code)_
```

This footer is added by the runner code, not the model. The model is instructed
not to attribute itself; the infrastructure adds attribution at the delivery
layer. This is transparent: anyone reading the GitHub comment knows it came
from an automated tool, while the content itself reads as human-written code
review.

Whether to include or suppress this footer is a policy choice for the operator.
A configuration option `webhookAttribution: false` would suppress it.

### 3.8 Testing strategy

```go
// Detect: no remote → not active
repo := initGitRepo(t)  // git init, no remote
status := undercover.Detect(repo)
assert(t, !status.Active)
assert(t, strings.Contains(status.Reason, "no git remote"))

// Detect: github.com remote → active
addRemote(t, repo, "origin", "https://github.com/someuser/somerepo.git")
status = undercover.Detect(repo)
assert(t, status.Active)
assert(t, strings.Contains(status.Reason, "github"))

// Detect: internal github → not active
addRemote(t, repo, "origin", "https://github.anthropic.com/internal/repo.git")
status = undercover.Detect(repo)
assert(t, !status.Active)

// Detect: gitlab.com → active
addRemote(t, repo, "origin", "https://gitlab.com/user/repo.git")
status = undercover.Detect(repo)
assert(t, status.Active)

// Detect: self-hosted → not active
addRemote(t, repo, "origin", "https://git.mycompany.internal/repo.git")
status = undercover.Detect(repo)
assert(t, !status.Active)

// Detect: no git repo (not inside a git repo) → not active
status = undercover.Detect("/tmp")
assert(t, !status.Active)

// SystemPromptFragment contains expected keywords
assert(t, strings.Contains(undercover.SystemPromptFragment, "CRITICAL"))
assert(t, strings.Contains(undercover.SystemPromptFragment, "Capybara"))
assert(t, strings.Contains(undercover.SystemPromptFragment, "Co-Authored-By"))

// Settings override: explicit false disables auto-detection
falseVal := false
cfg := config.Settings{UndercoverMode: &falseVal}
// even if repo has public remote, settings.UndercoverMode == &false → not active
active := resolveUndercoverMode(cfg, publicRepo)
assert(t, !active)
```

---

## 4. How the Three Systems Interact at Session Start

```go
func main() {
    // 1. Load config (three-level merge + CLAUDE.md)
    cfg := config.NewLoader(workDir)
    cfg.Load()
    settings := cfg.Get()

    // 2. Resolve undercover mode
    undercoverActive := resolveUndercoverMode(settings, workDir)

    // 3. Build system prompt (base + undercover + CLAUDE.md)
    sysPrompt := buildSystemPrompt(workDir, cfg.SystemInjection(), undercoverActive)
    mgr := convo.NewManagerWithSystem(sysPrompt)

    // 4. Build permission engine
    eng := permissions.NewEngine(
        permissions.ParseMode(settings.PermissionMode),
        settings.AllowedTools,
        settings.DeniedTools,
        filepath.Join(workDir, ".claude", "permissions.json"),
        tuiPermitFn,  // or AllowAll in headless mode
    )

    // 5. Wire permit function into agent loop
    loop := agent.NewLoop(client, mgr, registry, eng.WrapPermitFn(), eventCh)
}
```

The config flows into the permission engine (mode, allow/deny lists) and into
the system prompt (CLAUDE.md injection, undercover mode). The engine wraps its
priority chain into a `PermissionFunc` that the agent loop uses. Everything
is decided at session start; the agent loop has no direct knowledge of config
or undercover mode.

### 4.1 Runtime changes

Some settings can change mid-session:

- `/model <name>` — calls `client.SetModel(name)` and updates the status bar
- `/permissions allow bash` — calls `engine.AddAllowed("bash")`
- `/compact` — triggers `mgr.Summarise()`
- CLAUDE.md changes — `fsnotify` watcher (Phase 4) calls `mgr.SetSystemPrompt()`

The config loader supports `OnChange(fn func(Settings))` callbacks for when
settings files change on disk. Connecting this to the live system prompt and
permission engine is a Phase 4 task.

---

## 5. Edge Cases and Known Issues

### Config: race between `Load()` and settings file write

If two drover-code instances run simultaneously for the same project (e.g. two
terminals open), they could both load settings, one modifies a rule, the other
writes a different rule — last write wins. The temp-file + rename atomic write
prevents corruption but not logical conflicts. A file locking mechanism would
be needed for true multi-process safety. In practice, running two instances
simultaneously is unusual and the consequences of a lost rule write are minor.

### Permissions: no expiry on persisted rules

Once a rule is added ("always allow bash"), it persists indefinitely. If a user
approves a tool for a one-time task and then forgets about the rule, future
sessions will auto-approve that tool without any prompt. A future improvement:
timestamp rules and optionally expire them, or show the user their current
rules at session start if any were added in the previous session.

### Permissions: `ModePlan` batch UI not implemented

As noted, `ModePlan` currently behaves identically to `ModeDefault`. The batch
approval UI (show all proposed operations, approve/reject as a group) is a
significant TUI addition. Users who set `permissionMode: "plan"` will get
individual prompts, which is safe but not the intended UX.

### Undercover: GitHub API visibility check

The current implementation uses URL heuristics to detect public repos. A full
implementation would call the GitHub API (`GET /repos/{owner}/{repo}`) to check
`"private": false`. This would correctly handle:
- Private GitHub repos (where heuristic incorrectly activates undercover mode)
- GitHub Enterprise repos that are public (where heuristic correctly detects)

The API call requires a GitHub token and adds latency at session start. The
heuristic covers the common case (public github.com repos) correctly enough
for the primary use case (Anthropic employees contributing to open source).

### CLAUDE.md: no live reload

CLAUDE.md files are read once at session start. If the user edits CLAUDE.md
during a session, the changes are not picked up until the next session. An
`fsnotify` watcher would address this but adds complexity. For most workflows,
sessions are short enough that reload is not needed.

---

*Previous: [`07-tui.md`](./07-tui.md)*  
*Next: [`09-advanced-systems.md`](./09-advanced-systems.md) — Dream Memory and Coordinator*
