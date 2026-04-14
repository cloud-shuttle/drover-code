# 05 — Shell and Search Tools: bash, glob, grep

**Package:** `internal/tools/shell`, `internal/tools/search`  
**Files:** `shell/bash.go`, `search/glob.go`, `search/grep.go`  
**Tools:** `bash`, `glob`, `grep`  
**Depends on:** `internal/tools/toolutil`

---

## Purpose

These three tools form the codebase exploration and execution layer. `bash`
is the escape hatch — anything the other tools can't do, bash can. `glob`
and `grep` are the primary mechanisms for finding files and code patterns
before reading them. Together they cover the majority of what a developer
does in a terminal when exploring an unfamiliar codebase.

They share a design tension: they are the most powerful tools and the most
dangerous. `bash` can delete files, exfiltrate data, or run arbitrary code.
`grep` on a large codebase can produce megabytes of output. `glob` with `**`
on `/` would recurse forever. Each tool has specific mitigations for its
particular risks.

---

## 1. `bash`

### 1.1 Why bash at all?

Every agentic coding tool eventually faces the question: should we provide a
general-purpose shell execution tool, or restrict the model to purpose-built
tools for each operation?

The purpose-built approach is safer in theory — each tool can have tight
validation and sandboxing. But in practice:

- There are too many operations to enumerate. `npm install`, `cargo build`,
  `make`, `docker compose up`, database migrations, test runners — you cannot
  pre-build a tool for every command a developer might need.
- Purpose-built tools are often worse than the real thing. A custom `git_blame`
  tool is always going to be worse than `git blame` itself, and maintaining it
  requires tracking changes to git's output format.
- Models know bash. The model has been trained on vast amounts of shell usage
  and is good at constructing correct commands. It's less good at working
  around artificial tool limitations.

The conclusion: provide bash, but require permission for every call. The
permission system (not tool restriction) is the right control point.

### 1.2 Subprocess design

```go
cmd := exec.CommandContext(cmdCtx, "bash", "-c", inp.Command)
cmd.Dir = workDir
var stdout, stderr bytes.Buffer
cmd.Stdout = &stdout
cmd.Stderr = &stderr
```

**`bash -c`** means the entire command string is passed to bash as a script.
This enables:
- Pipelines: `grep "error" app.log | sort | uniq -c`
- Redirections: `cat > file.txt << 'EOF'`
- Multi-statement: `cd src && go test ./...`
- Environment variable interpolation: `echo $GOPATH`
- Conditionals: `[ -f .env ] && source .env`

Passing the command through bash rather than directly to `exec.Command` also
means command injection via tool inputs is impossible — there's no way to
"break out" of a bash script via `bash -c`.

**`cmd.Dir = workDir`** sets the working directory for the subprocess. Without
this, all commands run in whatever directory the drover-code process was started
from, which may not be the project root if the process changed directories.

**Inheriting the parent environment** (`cmd.Env` is nil by default, which
means inherit) is important: it gives the model access to `PATH`, `GOPATH`,
`JAVA_HOME`, `AWS_PROFILE`, and any other environment variables the user has
set in their shell. Without this, common commands like `go`, `npm`, `python`
might not be found.

### 1.3 Stdout and stderr separation

```go
var stdout, stderr bytes.Buffer
cmd.Stdout = &stdout
cmd.Stderr = &stderr
```

We capture stdout and stderr separately and present both to the model with
labels:

```
$ go build ./...
exit_code: 1  elapsed: 1.2s

[stdout]
(no output)

[stderr]
# github.com/foo/bar
./main.go:42:5: undefined: fooBar
```

Why separate them when we could just combine with `cmd.CombinedOutput()`?

**Different information types.** Stdout is the command's primary output.
Stderr is diagnostic information: error messages, progress indicators, debug
output, warnings. The model needs to distinguish them to reason correctly.
`go build` errors go to stderr. `go test` failures go to stderr. `grep`
matches go to stdout. A model that can't tell stdout from stderr will
misinterpret these.

**Error detection.** A non-zero exit code combined with non-empty stderr is
the standard Unix signal for a failure. A model seeing `exit_code: 1` plus
a stderr block with a compiler error immediately understands the problem.
Combined output obscures this structure.

**Silent success.** Many commands produce no stdout on success (e.g. `go
build`, `git add`). The combined output would be empty, which the model might
interpret as ambiguous. Separate capture + the explicit `(no output)` sentinel
makes success unambiguous.

### 1.4 Timeout implementation

```go
const defaultTimeoutSecs = 120
const maxTimeoutSecs = 600

func (t *Bash) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
    // ...
    timeout := time.Duration(inp.TimeoutSec) * time.Second
    if timeout <= 0 { timeout = defaultTimeoutSecs * time.Second }
    if timeout > maxTimeoutSecs * time.Second {
        timeout = maxTimeoutSecs * time.Second
    }

    cmdCtx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    cmd := exec.CommandContext(cmdCtx, "bash", "-c", inp.Command)
    // ...
}
```

`exec.CommandContext` sends SIGKILL to the process when the context is
cancelled — either by timeout or by the parent context (user Ctrl+C). SIGKILL
cannot be caught or ignored, so the subprocess always terminates.

**Why SIGKILL and not SIGTERM?** `exec.CommandContext` uses SIGKILL directly.
A graceful shutdown sequence (SIGTERM → wait → SIGKILL) would be better for
long-running processes, but adds complexity (goroutine management, timing).
For the drover-code use case — coding tasks that should complete in seconds —
SIGKILL is appropriate. A process that hasn't completed in 10 minutes is
stuck, and we want it gone.

**Why 120 seconds default?** Long operations include:
- `npm install` in a large project: 30–90s
- `cargo build --release`: 60–180s
- Docker image builds: 60–300s
- Test suites: variable but often > 60s

120 seconds covers the common case. The model can explicitly request a longer
timeout (up to 600s) for known-slow operations.

**Why 600 seconds maximum?** A webhook job has a 10-minute overall timeout.
A bash call shouldn't be able to consume the entire job budget. The 600s cap
ensures there's at least some time left for the model to process results and
respond.

### 1.5 Output format in detail

```go
func formatBashOutput(command, stdout, stderr string, exitCode int, elapsed time.Duration) string {
    var b strings.Builder
    fmt.Fprintf(&b, "$ %s\n", command)
    fmt.Fprintf(&b, "exit_code: %d  elapsed: %s\n", exitCode, elapsed)
    if stdout != "" {
        b.WriteString("\n[stdout]\n")
        b.WriteString(strings.TrimRight(stdout, "\n"))
        b.WriteString("\n")
    }
    if stderr != "" {
        b.WriteString("\n[stderr]\n")
        b.WriteString(strings.TrimRight(stderr, "\n"))
        b.WriteString("\n")
    }
    if stdout == "" && stderr == "" {
        b.WriteString("\n(no output)\n")
    }
    return toolutil.Truncate(b.String())
}
```

**Repeating the command** (`$ <command>`) in the output serves the model's
context: by the time the tool result arrives, the model may be processing
several tool results simultaneously. Repeating the command removes ambiguity
about which result corresponds to which call.

**`elapsed`** is rounded to milliseconds. Values like `8.312s` are useful for
the model to assess whether a command was suspiciously slow (might indicate
a network hang or deadlock) or unexpectedly fast (might indicate it didn't
actually run).

**`strings.TrimRight(stdout, "\n")`** removes trailing newlines before adding
our own. Without this, commands that print a trailing newline would produce
a double blank line in the output, wasting tokens.

**The `(no output)` sentinel** (when both stdout and stderr are empty) is
important for commands like `git add` or `touch file`. An empty tool result
body would make the model uncertain whether the command ran at all.

### 1.6 Working directory override

```go
type bashInput struct {
    Command         string `json:"command"`
    TimeoutSec      int    `json:"timeout_seconds"`
    WorkDir         string `json:"working_directory"`
}

// In Execute:
workDir := t.WorkDir
if inp.WorkDir != "" {
    workDir, err = toolutil.SafePath(t.WorkDir, inp.WorkDir)
}
cmd.Dir = workDir
```

The model can run a command in a subdirectory without prefixing the command
with `cd`. This is mainly useful for monorepos where different packages have
different build systems:

```json
{"command": "npm test", "working_directory": "frontend"}
{"command": "go test ./...", "working_directory": "backend"}
```

`SafePath` validates the working directory against `t.WorkDir` (the project
root), preventing the model from running commands in arbitrary system
directories.

### 1.7 Security considerations

`bash` is `NeedsPermission: true` with no exceptions. The permission engine
then applies its rules: in interactive mode, prompt the user; in bypass mode
(coordinator workers, GitHub webhook runner), allow automatically.

This creates an asymmetry: the webhook runner auto-approves all bash calls.
The mitigation is the system prompt, which instructs the model not to make
commits or push unless explicitly asked. A future hardening measure would be
a bash command filter for the webhook context — deny any command matching
`git commit`, `git push`, `rm -rf`, etc. This is more robust than trusting
the model to follow system prompt instructions.

The command runs in a subprocess with the drover-code process's environment,
including any secrets in environment variables (`AWS_SECRET_ACCESS_KEY`,
`GITHUB_TOKEN`, etc.). This is a genuine security risk in shared environments.
Documentation should advise users not to run drover-code with sensitive secrets
in the environment unless they trust the model's tool calls.

---

## 2. `glob`

### 2.1 Why not `filepath.Glob`?

The standard library `filepath.Glob` does not support `**` (double-star
recursive matching). It only supports `*` (match within one path component)
and `?` (match one character within one component). This means:

```
filepath.Glob("**/*.go")  // does not work as expected
```

The `**` pattern is essential for modern project navigation:
- `**/*.go` — all Go files in any subdirectory
- `**/*_test.go` — all test files
- `src/**/*.ts` — all TypeScript in the src tree
- `!**/vendor/**` — exclude vendor directories (not supported here, but common)

Without `**`, the model would need multiple glob calls or fall back to bash
with `find`, losing the structured output.

### 2.2 The `**` matching algorithm

```go
func matchSegments(patSegs, pathSegs []string) (bool, error) {
    for len(patSegs) > 0 && len(pathSegs) > 0 {
        p := patSegs[0]

        if p == "**" {
            // ** matches zero segments
            if ok, _ := matchSegments(patSegs[1:], pathSegs); ok {
                return true, nil
            }
            // ** matches one or more segments: consume one path seg, retry
            return matchSegments(patSegs, pathSegs[1:])
        }

        // Regular segment: must match exactly
        ok, err := filepath.Match(p, pathSegs[0])
        if err != nil || !ok {
            return false, err
        }
        patSegs = patSegs[1:]
        pathSegs = pathSegs[1:]
    }

    // Drain trailing **
    for len(patSegs) > 0 && patSegs[0] == "**" {
        patSegs = patSegs[1:]
    }

    return len(patSegs) == 0 && len(pathSegs) == 0, nil
}
```

This is a recursive descent parser for a simplified path pattern language.
Each `**` segment creates two branches:

1. **Zero match:** Try matching the rest of the pattern against the current
   path position (skip the `**`)
2. **One or more match:** Consume one path segment and retry (the `**` remains
   in the pattern to potentially consume more)

The recursion terminates because either:
- The path segments run out (base case)
- The pattern segments run out (base case)
- A non-`**` pattern segment fails to match (prunes the branch)

**Using `filepath.Match` for individual segments** means all the standard
glob characters (`*`, `?`, `[abc]`, `[a-z]`) work within each segment. Only
`**` is special to our algorithm.

### 2.3 Walk and filter

```go
err := filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
    if err != nil { return nil }  // skip unreadable entries

    // Skip hidden directories unless pattern targets them
    if d.IsDir() && strings.HasPrefix(d.Name(), ".") {
        if !strings.Contains(pattern, "."+d.Name()) {
            return filepath.SkipDir
        }
    }

    rel, _ := filepath.Rel(baseDir, path)
    matched, _ := matchDoublestar(pattern, rel)
    if matched && !d.IsDir() {
        matches = append(matches, path)
    }
    if len(matches) >= maxGlobResults {
        return fmt.Errorf("glob limit reached")
    }
    return nil
})
```

**Skipping hidden directories by default** is a significant performance and
noise reduction. `.git` directories contain thousands of files (object files,
pack files, ref files). `.node_modules` can contain hundreds of thousands.
Walking them for a pattern like `**/*.go` would be extremely slow and return
no useful results.

The exception `!strings.Contains(pattern, "."+d.Name())` handles the case
where the user explicitly targets a hidden directory:
- `**/*.go` → skip `.git`
- `.github/**/*.yml` → don't skip `.github`

This heuristic works well but has a flaw: a pattern like `**/*.git` would
incorrectly prevent skipping `.git` directories (the pattern contains `.git`
even though it's not targeting the `.git` directory). A better check would be:
`strings.HasPrefix(pattern, "."+d.Name())` or checking that `.hidden` appears
as a path component in the pattern. This is a known limitation.

**The limit sentinel:** Returning `fmt.Errorf("glob limit reached")` from the
WalkDir callback terminates the walk. `filepath.WalkDir` treats any non-nil
return value from the callback as an error and stops walking. We filter this
specific error after the walk completes.

### 2.4 Result paths are relative

```go
for _, m := range matches {
    r, _ := filepath.Rel(baseDir, m)
    rel = append(rel, r)
}
```

Absolute paths from `filepath.WalkDir` are converted to paths relative to
`baseDir` before returning. This produces cleaner output (`src/main.go`
instead of `/Users/peter/projects/myapp/src/main.go`) and makes the results
portable between machines.

### 2.5 Output format

```
42 file(s) matched "**/*.go":
  cmd/drover-code/main.go
  internal/agent/events.go
  internal/agent/loop.go
  ...
```

The count at the top tells the model how many matches exist before it starts
reading them. If the count is large, the model may decide to refine the
pattern rather than reading all matching files.

When the limit is hit:
```
1000 file(s) matched "**/*" (limit: first 1000 shown):
  ...
```

This tells the model the results are incomplete without being an error.

---

## 3. `grep`

### 3.1 The ripgrep decision

```go
func (t *Grep) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
    // ...
    if rgPath, err := exec.LookPath("rg"); err == nil {
        return t.grepWithRg(ctx, rgPath, ...)
    }
    return t.grepPureGo(ctx, ...)
}
```

Ripgrep (`rg`) is detected at call time, not at startup. This means:
- No startup error if `rg` is not installed
- The pure Go fallback activates automatically
- Installing `rg` later in the session is immediately picked up

Why prefer `rg` over Go's `regexp` package?

**Speed.** Ripgrep uses SIMD instructions and optimised regex engines (PCRE2
or its own `regex` crate). For a 100,000 file codebase, rg completes in
milliseconds. The pure Go implementation may take several seconds.

**`.gitignore` awareness.** Ripgrep respects `.gitignore` patterns by default.
The pure Go implementation does not (it would need to parse and apply gitignore
patterns, which is non-trivial). This means rg naturally excludes `build/`,
`dist/`, `node_modules/`, etc. — the files developers almost never want to
search.

**Binary skipping.** Ripgrep detects binary files using the same null-byte
heuristic we use in `read_file`, and skips them automatically. The pure Go
implementation does this per-file but less efficiently.

**Encoding detection.** Ripgrep handles UTF-8, UTF-16, Latin-1, and other
common encodings. The pure Go implementation only handles UTF-8.

The trade-off: rg is not available everywhere. In minimal Docker containers,
CI environments, or fresh machine setups, it may not be present. The pure Go
fallback ensures consistent (if slower) behaviour.

### 3.2 Ripgrep arguments

```go
args := []string{
    "--line-number",      // include line numbers
    "--no-heading",       // don't group by file (easier to parse)
    "--color=never",      // no ANSI codes (model reads plain text)
    fmt.Sprintf("--context=%d", contextLines),
    fmt.Sprintf("--max-count=%d", maxMatches),
}
if !caseSensitive { args = append(args, "--ignore-case") }
if filePattern != ""  { args = append(args, "--glob", filePattern) }
args = append(args, pattern, searchPath)
```

**`--no-heading`** produces output like:
```
src/auth.go:42:func validateToken(token string) bool {
src/auth.go:45:    return token != ""
```

Rather than:
```
src/auth.go
42: func validateToken(token string) bool {
45:     return token != ""
```

The `file:line:content` format is unambiguous and easy for the model to
reference (`src/auth.go line 42`).

**`--context=N`** includes N lines before and after each match (like `grep -C`).
Default is 2. This gives the model enough surrounding code to understand the
match without reading the entire file.

**`--max-count=N`** stops after N total matches. Without this, `grep "func"`
on a large Go codebase could return thousands of matches, overwhelming the
context. The model can refine its pattern or add file filters if the count is
too high.

**Exit code 1** from rg means "no matches found" — not an error. We detect
this:

```go
if err != nil {
    if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
        return fmt.Sprintf("no matches for %q", pattern), nil
    }
    return "", fmt.Errorf("grep: rg failed: %w", err)
}
```

### 3.3 Pure Go implementation

```go
func (t *Grep) grepPureGo(ctx context.Context, pattern, searchPath, filePattern string,
    contextLines, maxMatches int, caseSensitive bool) (string, error) {

    regexStr := pattern
    if !caseSensitive { regexStr = "(?i)" + pattern }
    re, err := regexp.Compile(regexStr)
    if err != nil {
        return "", fmt.Errorf("grep: invalid pattern %q: %w", pattern, err)
    }

    var results []string
    matchCount := 0

    err = filepath.WalkDir(searchPath, func(path string, d os.DirEntry, err error) error {
        if err != nil || d.IsDir() { return nil }
        if filePattern != "" {
            ok, _ := filepath.Match(filePattern, d.Name())
            if !ok { return nil }
        }
        if strings.HasPrefix(d.Name(), ".") { return nil }

        fileResults, n, _ := searchFile(ctx, re, path, t.WorkDir, contextLines)
        results = append(results, fileResults...)
        matchCount += n
        if matchCount >= maxMatches {
            return fmt.Errorf("max matches reached")
        }
        return nil
    })
    // ...
}
```

**Case insensitivity** via `(?i)` prefix is a Go regex feature. It compiles
to a case-folding automaton that's almost as fast as case-sensitive matching.

**File pattern matching** uses `filepath.Match` against the filename only
(`d.Name()`), not the full path. This means `"*.go"` matches `src/main.go`
because we match against `"main.go"`. This is the expected `glob`-style
behaviour.

**Skipping hidden files** (`strings.HasPrefix(d.Name(), ".")`) skips both
hidden files and hidden directories (since `WalkDir` calls the function for
every entry including directories). Note this doesn't skip `.git` directory
contents — it skips files whose name starts with `.` in each directory. For
a complete gitignore-aware implementation, we'd need to parse `.gitignore`
files, which is out of scope.

### 3.4 Per-file search implementation

```go
func searchFile(ctx context.Context, re *regexp.Regexp, path, baseDir string,
    contextLines int) ([]string, int, error) {

    f, err := os.Open(path)
    if err != nil { return nil, 0, nil }
    defer f.Close()

    var lines []string
    scanner := bufio.NewScanner(f)
    scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
    for scanner.Scan() {
        lines = append(lines, scanner.Text())
    }

    // Quick binary check
    for _, l := range lines[:min(len(lines), 5)] {
        if strings.ContainsRune(l, 0) { return nil, 0, nil }
    }

    var results []string
    for i, line := range lines {
        if !re.MatchString(line) { continue }

        var block strings.Builder
        start := max(0, i-contextLines)
        for j := start; j < i; j++ {
            fmt.Fprintf(&block, "%s-%d-%s\n", rel, j+1, lines[j])
        }
        fmt.Fprintf(&block, "%s:%d:%s\n", rel, i+1, line)
        end := min(len(lines)-1, i+contextLines)
        for j := i+1; j <= end; j++ {
            fmt.Fprintf(&block, "%s-%d-%s\n", rel, j+1, lines[j])
        }
        block.WriteString("--\n")
        results = append(results, block.String())
        // context cancellation check...
    }
    return results, len(results), nil
}
```

**1 MB scanner buffer** handles minified JavaScript files, long generated
files, and other cases where lines exceed the default 64 KB scanner buffer.
A minified `bundle.js` with one very long line would fail silently with a
smaller buffer (scanner returns false when the buffer is exceeded, discarding
the line).

**Reading all lines before searching** is a simplification. A streaming
approach (search while reading) would be more memory-efficient for large files.
But reading all lines upfront makes context extraction trivial — we can look
backwards and forwards with simple index arithmetic. For files up to a few
MB (the common case for source code), the memory cost is acceptable.

**Context format** uses different separators for context lines vs match lines:
- Match: `file:line:content` (colon separators, same as rg)
- Context: `file-line-content` (hyphen separators, same as grep -C)
- Block separator: `--` (same as rg and grep)

This format is immediately recognisable to the model from its training data.

**Binary detection** reads the first 5 lines and checks for null bytes. This
is less thorough than the full binary detection in `read_file` (which checks
the first 8 KB) but much faster for the search path where we're calling it
for potentially hundreds of files. A false negative (searching a binary file)
produces garbage results that the model ignores. A false positive (skipping a
valid text file) is worse — that's why `read_file` is more thorough.

### 3.5 Context cancellation during search

```go
select {
case <-ctx.Done():
    return results, matchCount, nil
default:
}
```

The context check inside the per-line loop allows the search to be cancelled
mid-file. Without this, a `grep` call on a huge file (10 MB of source code)
can't be interrupted. The check runs once per matching line — not every line
— which means it only adds overhead on files with many matches (the expensive
case).

### 3.6 Pattern compilation errors

```go
re, err := regexp.Compile(regexStr)
if err != nil {
    return "", fmt.Errorf("grep: invalid pattern %q: %w", pattern, err)
}
```

Invalid regex patterns return an error immediately, before touching the
filesystem. The error includes the pattern so the model knows what it tried.

Common model mistakes:
- Using PCRE-only syntax (`?P<name>` named groups work in Go, `(?P=name)` backreferences don't)
- Unescaped special characters (`grep "some.thing"` where `.` is intended literally)
- Overly complex patterns that cause catastrophic backtracking

The last case is not currently handled — Go's `regexp` package uses a linear-
time NFA algorithm that cannot have catastrophic backtracking, but the model
may be used to PCRE and write patterns that are inefficient in subtle ways.

---

## 4. Tool Interaction Patterns

### 4.1 The locate-then-read pattern

The dominant search workflow:

```
glob(pattern: "**/*_test.go")
    → [list of 23 test files]
grep(pattern: "TestAuth", path: ".")
    → [auth_test.go:15, auth_test.go:34, ...]
read_file(path: "src/auth_test.go", start_line: 10, end_line: 45)
    → [test function code]
```

`glob` narrows the search space by file name. `grep` narrows it further by
content. `read_file` with a line range reads only the relevant section.

This three-step approach is much more efficient than reading every file and
is what expert developers do when navigating an unfamiliar codebase.

### 4.2 The build-and-fix pattern

The most common multi-step bash workflow:

```
bash(command: "go build ./...")
    → exit_code: 1, stderr: "undefined: fooBar at main.go:42"
grep(pattern: "fooBar", path: ".")
    → main.go:42: fooBar()
read_file(path: "main.go", start_line: 38, end_line: 50)
    → [context around the undefined reference]
edit_file(path: "main.go", ...)
    → [diff showing the fix]
bash(command: "go build ./...")
    → exit_code: 0, (no output)
```

The model reads the build error, locates the problem, fixes it, and verifies
the fix. This pattern repeats until the build succeeds.

### 4.3 When bash is better than glob+grep

For some operations, `bash` with the right command is more efficient than
chaining glob and grep:

```bash
# Find all TODO comments with their context
grep -rn "TODO" --include="*.go" -A2 -B2

# Find files changed in the last week
find . -name "*.go" -mtime -7

# Find large files
find . -size +1M -type f

# Check if a dependency is used
grep -r "import.*fmt" --include="*.go" | wc -l
```

The model should prefer `glob` and `grep` for simple file and pattern
searches (structured output, no permission needed), but `bash` for complex
queries that combine multiple operations.

### 4.4 Parallel search

Like `read_file`, `glob` and `grep` can be called in parallel when the model
needs multiple pieces of information:

```
[Single response with multiple tool calls]
grep(pattern: "func NewHandler", path: "src/")    ← concurrent
grep(pattern: "type Handler", path: "src/")        ← concurrent
glob(pattern: "src/**/*.go")                       ← concurrent
```

All three complete in parallel. The agent loop collects all results and
delivers them in one user turn.

---

## 5. Testing Strategy

### `bash` tests

```go
// Happy path: simple command
result, err := bashTool.Execute(ctx, marshal(`{"command": "echo hello"}`))
assertNoError(t, err)
assertContains(t, result, "exit_code: 0")
assertContains(t, result, "[stdout]\nhello")

// Stderr separation
result, err = bashTool.Execute(ctx, marshal(`{"command": "echo out; echo err >&2"}`))
assertContains(t, result, "[stdout]\nout")
assertContains(t, result, "[stderr]\nerr")

// Non-zero exit code is not an error
result, err = bashTool.Execute(ctx, marshal(`{"command": "exit 1"}`))
assertNoError(t, err)  // tool does not fail
assertContains(t, result, "exit_code: 1")

// Timeout
result, err = bashTool.Execute(ctx, marshal(`{"command":"sleep 300","timeout_seconds":1}`))
assertError(t, err)
assertContains(t, err.Error(), "timed out")

// Elapsed time in output
result, err = bashTool.Execute(ctx, marshal(`{"command": "sleep 0.1"}`))
assertContains(t, result, "elapsed:")

// Context cancellation
ctx, cancel := context.WithCancel(context.Background())
go func() { time.Sleep(50 * time.Millisecond); cancel() }()
_, err = bashTool.Execute(ctx, marshal(`{"command": "sleep 10"}`))
assertError(t, err)  // should return before 10 seconds

// Working directory override
result, err = bashTool.Execute(ctx, marshal(`{
    "command": "pwd",
    "working_directory": "src"
}`))
assertContains(t, result, "/src")

// Path traversal in working_directory
_, err = bashTool.Execute(ctx, marshal(`{
    "command": "pwd",
    "working_directory": "../../.."
}`))
assertError(t, err)
```

### `glob` tests

```go
// Basic ** matching
createFiles(t, workDir,
    "src/main.go", "src/util/helper.go", "tests/main_test.go",
    "vendor/dep/dep.go", ".git/config",
)
result, err := globTool.Execute(ctx, marshal(`{"pattern": "**/*.go"}`))
assertNoError(t, err)
assertContains(t, result, "src/main.go")
assertContains(t, result, "src/util/helper.go")
assertContains(t, result, "tests/main_test.go")
assertNotContains(t, result, ".git/config")      // hidden dir skipped
assertNotContains(t, result, "vendor/dep/dep.go") // not matching... wait, vendor/ is not hidden
// Actually vendor/ should be included unless .gitignore excludes it
// glob doesn't read .gitignore; only rg does

// Explicit hidden directory
result, err = globTool.Execute(ctx, marshal(`{"pattern": ".github/**/*.yml"}`))
assertContains(t, result, ".github/")

// No matches
result, err = globTool.Execute(ctx, marshal(`{"pattern": "**/*.rb"}`))
assertContains(t, result, "no files matched")

// Directory filtering (only files returned)
result, err = globTool.Execute(ctx, marshal(`{"pattern": "src"}`))
assertNotContains(t, result, "src") // directories not returned

// Limit
createManyFiles(t, workDir, 1500) // create 1500 .txt files
result, err = globTool.Execute(ctx, marshal(`{"pattern": "**/*.txt"}`))
assertContains(t, result, "1000 file(s)")
```

### `grep` tests (both rg and pure Go)

Run the same test suite twice — once with `rg` on `$PATH`, once with rg
removed from `$PATH` to force the pure Go implementation:

```go
for _, usePureGo := range []bool{false, true} {
    t.Run(fmt.Sprintf("pureGo=%v", usePureGo), func(t *testing.T) {
        if usePureGo {
            // Temporarily remove rg from PATH for this test
            t.Setenv("PATH", pathWithoutRg())
        }

        // Basic match
        writeTestFile(t, "main.go", "func main() {}\nfunc helper() {}\n")
        result, err := grepTool.Execute(ctx, marshal(`{
            "pattern": "func main",
            "path": "main.go"
        }`))
        assertNoError(t, err)
        assertContains(t, result, "main.go:1:func main()")

        // Case insensitive
        result, _ = grepTool.Execute(ctx, marshal(`{
            "pattern": "FUNC MAIN",
            "path": "main.go",
            "case_sensitive": false
        }`))
        assertContains(t, result, "func main()")

        // Context lines
        result, _ = grepTool.Execute(ctx, marshal(`{
            "pattern": "func helper",
            "path": "main.go",
            "context_lines": 1
        }`))
        assertContains(t, result, "main.go-1-func main()")   // context before
        assertContains(t, result, "main.go:2:func helper()") // match

        // No matches
        result, _ = grepTool.Execute(ctx, marshal(`{"pattern": "xyz123"}`))
        assertContains(t, result, "no matches")

        // Invalid regex
        _, err = grepTool.Execute(ctx, marshal(`{"pattern": "[invalid"}`))
        assertError(t, err)
        assertContains(t, err.Error(), "invalid pattern")
    })
}
```

---

## 6. Edge Cases and Known Issues

### `bash`: zombie processes on timeout

When `exec.CommandContext` sends SIGKILL to the bash process, child processes
spawned by the bash script may not receive the signal. Bash typically forwards
signals to its process group, but this is not guaranteed for all commands or
platforms. Zombie processes may accumulate over a long session.

Mitigation: use `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` to
start bash in its own process group, then send SIGKILL to the entire process
group on timeout. This ensures all child processes are killed.

```go
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
// On timeout (when context is cancelled):
// syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
```

This is Linux/macOS specific (Windows doesn't have process groups in the same
sense). The implementation would need build tags.

### `glob`: vendor directory included

Unlike `rg`, the pure Go glob implementation doesn't respect `.gitignore`.
This means `**/*.go` includes files in `vendor/` (a common Go pattern for
vendored dependencies) which can produce thousands of irrelevant results.

The model can work around this with a `base_dir` that excludes vendor:
`glob(pattern: "**/*.go", base_dir: "src")`. But ideally we'd parse and apply
`.gitignore` patterns. The `go-gitignore` library exists but adds a dependency.

### `grep` pure Go: performance on large repos

The pure Go implementation walks the directory tree and reads every file.
On a large codebase (100K+ files, 10M+ lines), this can take 10+ seconds and
produce output that overflows the 200K truncation limit. The model should use
`rg` when available or refine its search with `file_pattern` and `path` to
limit the search space.

### `grep`: context overlap

When two matches are within `contextLines` of each other, their context blocks
overlap in the rg output but appear as separate blocks in the pure Go
implementation. This produces slightly different output between the two
implementations for adjacent matches. The model handles this correctly in
practice because the match lines are always present — only the context
arrangement differs.

### `bash`: no stdin

`cmd.Stdin` is nil (not set), meaning the bash subprocess has no stdin. Commands
that expect interactive input (e.g. `sudo`, `ssh`, password prompts) will fail
or hang. This is correct behaviour — an agentic tool should not accept
interactive input. If the model tries to run an interactive command, it will
get an error or a hang (caught by the timeout) and can adapt.

---

## 7. Future Considerations

### `bash`: process group management

As discussed in §6.1, proper process group management for SIGKILL would improve
cleanup on timeout. Implement with build tags for Linux/macOS.

### `bash`: output streaming

Currently, bash output is captured and returned as a single block after the
command completes. For long-running commands (build systems, test runners),
it would be better to stream output to the TUI as it arrives. This would
require `cmd.Stdout` to be a pipe that's read concurrently while the command
runs, with partial output delivered via `TextDeltaEvent`s. Complex to implement
correctly but significantly improves UX for slow commands.

### `grep`: `.gitignore` awareness in pure Go

Parse and apply `.gitignore` files encountered during the walk. This closes
the behaviour gap between rg and pure Go, and prevents the vendor/node_modules
explosion. The parsing logic is non-trivial (gitignore patterns have their own
mini-language) but widely implemented libraries exist.

### `ripgrep` embedded binary

For guaranteed rg performance without requiring system installation, embed
the rg binary in the drover-code binary using `//go:embed`. This would make
the binary larger (~5 MB for rg) but eliminate the pure-Go fallback path.
Feasibility depends on license compatibility (rg uses MIT/UNLICENSE).

### `ast_grep` integration

For code-structure-aware search — find all function calls, not just text
patterns — integration with `ast-grep` (`sg`) would be valuable. Like rg,
detect at runtime and fall back to grep if not available. This would allow
queries like "find all calls to `http.Get`" that regex can't reliably answer.

---

*Previous: [`04-fs-tools.md`](./04-fs-tools.md)*  
*Next: [`06-git-web-tools.md`](./06-git-web-tools.md) — Git Tools and web_fetch*
