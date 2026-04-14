# 06 — Git Tools and web_fetch

**Package:** `internal/tools/git`, `internal/tools/web`  
**Files:** `git/git.go`, `web/fetch.go`  
**Tools:** `git_status`, `git_diff`, `git_log`, `git_add`, `git_commit`,
`git_push`, `git_create_branch`, `web_fetch`  
**Depends on:** `internal/tools/toolutil`

---

## Purpose

Git tools give the model a first-class interface to version control, covering
the full cycle from checking state through committing and pushing. The `web_fetch`
tool reaches the wider world — documentation, APIs, GitHub issues, dependency
registries. Together they handle the two most common "reach outside the local
filesystem" needs in a coding session.

Both packages share a design philosophy: prefer correctness over clever
abstraction. Git tools shell out to the real `git` binary rather than using
a Go library. `web_fetch` makes plain HTTP requests and does minimal HTML
processing. There are no clever wrappers; the tools do exactly what they say
and produce output that mirrors what a developer would see in their terminal.

---

## 1. Why Shell Out to `git`?

This is the most consequential design decision in the git package and deserves
a thorough explanation.

### 1.1 The `go-git` alternative

`github.com/go-git/go-git` is a pure Go implementation of git. It would
eliminate subprocess overhead, work without `git` installed, and be easier to
test (no subprocess mocking needed). So why not use it?

**Behavioural divergence.** `go-git` aims for compatibility but diverges from
native git in ways that matter for real-world use:

- Sparse checkouts and partial clones are not fully supported
- Worktrees have known limitations
- Some credential helpers don't work
- Certain merge strategies produce different results
- Hook execution (pre-commit, commit-msg) is handled differently
- `.gitconfig` options are partially supported

Any of these divergences can produce confusing results when the model runs a
command and the behaviour doesn't match what `git` would do in the terminal.
Debugging "why does my commit have wrong authorship" when the model used `go-git`
internally but the user sees `git` externally is painful.

**Configuration.** Users invest significant effort in their git configuration:
signing commits with GPG, custom merge tools, per-directory `.gitconfig` files,
credential managers, LFS filters. Native `git` respects all of this. `go-git`
may or may not.

**Credential handling.** `git push` needs to authenticate to remote repositories.
Credential helpers (macOS Keychain, Windows Credential Manager, GNOME Keyring,
SSH agents) are configured in the user's git config and activated by native
git automatically. Replicating this in Go would require implementing the git
credential helper protocol — a significant undertaking.

**Version features.** `git` adds new options in each release. The subprocess
approach automatically supports new options without code changes. A `go-git`
wrapper would need manual updates to expose new capabilities.

**Conclusion:** The subprocess approach trades a small per-call overhead
(~10–50ms to spawn a process) for behavioural correctness. For a tool that
runs at human timescales, this is the right trade.

### 1.2 The `runGit` helper

```go
func runGit(ctx context.Context, workDir string, args ...string) (string, error) {
    cmd := exec.CommandContext(ctx, "git", args...)
    cmd.Dir = workDir
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    if err := cmd.Run(); err != nil {
        msg := strings.TrimSpace(stderr.String())
        if msg == "" {
            msg = err.Error()
        }
        return "", fmt.Errorf("git %s: %s", args[0], msg)
    }
    return toolutil.Truncate(stdout.String()), nil
}
```

All git tools flow through this helper. Key decisions:

**Separate stdout/stderr capture.** Git writes errors to stderr and output to
stdout. For user-facing results we want stdout. For error messages we want
stderr. Capturing them separately means `runGit` can present clean output on
success and a meaningful error message on failure.

**Error message formatting.** `fmt.Errorf("git %s: %s", args[0], msg)` produces
errors like `git push: error: failed to push some refs to 'origin'`. The `args[0]`
(the subcommand) anchors the error. The model sees this and understands which
git operation failed.

**Truncation on stdout.** `toolutil.Truncate(stdout.String())` caps the output
at 200,000 bytes. `git log` on a large repository or `git diff` on a huge
changeset could produce megabytes. Without truncation this would overflow the
context window.

**No stderr in the success path.** Git sometimes writes warnings to stderr even
on success (e.g. `warning: LF will be replaced by CRLF`). We intentionally
discard stderr on success. These warnings are noise for the model — they don't
affect the operation's outcome and would dilute the actual output. If the model
needs to see warnings, it can run the equivalent `bash` command.

---

## 2. `git_status`

```go
func (t *Status) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
    return runGit(ctx, t.WorkDir, "status", "--short", "--branch")
}
```

### 2.1 Output format

`--short --branch` produces:

```
## main...origin/main
M  src/auth.go
?? src/new_file.go
D  src/old_file.go
```

- `##` line: current branch and tracking status
- `M` modified (staged), ` M` modified (unstaged)
- `??` untracked
- `D` deleted (staged), ` D` deleted (unstaged)
- `A` added (staged)
- `R` renamed

This is the most information-dense status format git offers. The model can
immediately see what's staged, what's not, what branch it's on, and how far
ahead/behind the remote it is.

**Why not `git status` (long form)?** The long form includes explanatory text
(`"Changes not staged for commit:"`, `"Use 'git add' to update..."`) that the
model doesn't need and that wastes tokens. The short form gives the same
information in roughly 1/5 the tokens.

### 2.2 Permission

`git_status` is read-only and `NeedsPermission: false`. It should be callable
without any user interaction — the model uses it as a starting point for almost
every git workflow to understand the current state.

---

## 3. `git_diff`

```go
type diffInput struct {
    Staged bool   `json:"staged"`
    Path   string `json:"path"`
    Base   string `json:"base"`
}

func (t *Diff) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
    var inp diffInput
    json.Unmarshal(rawInput, &inp)

    args := []string{"diff", "--unified=3"}
    if inp.Staged { args = append(args, "--cached") }
    if inp.Base != "" { args = append(args, inp.Base) }
    if inp.Path != "" { args = append(args, "--", inp.Path) }

    out, err := runGit(ctx, t.WorkDir, args...)
    if err != nil { return "", err }
    if out == "" { return "no changes", nil }
    return out, nil
}
```

### 3.1 The `--unified=3` flag

Shows 3 lines of context around each change — enough to understand what
changed without showing the entire file. The model can always increase context
by passing a larger value via `bash` if needed.

### 3.2 Staged vs unstaged

`staged: true` adds `--cached`, showing the diff between the index (staging
area) and the last commit. `staged: false` (default) shows the diff between
the working tree and the index.

Typical usage pattern:
```
git_status
    → M  src/auth.go (unstaged), A  tests/new_test.go (staged)
git_diff
    → shows unstaged changes to auth.go
git_diff(staged: true)
    → shows staged changes to new_test.go
```

### 3.3 Base ref comparison

`base: "HEAD~1"` shows changes since the last commit. `base: "main"` shows
changes since branching from main. `base: "abc1234"` compares against a
specific commit.

This is useful for reviewing what a branch has changed overall:
```
git_diff(base: "main")
    → all changes on the current branch compared to main
```

### 3.4 Empty diff

If the working tree matches the comparison point, git produces no output.
We return `"no changes"` rather than an empty string — the model needs to
distinguish "the tool produced no output" from "the tool was not called".

---

## 4. `git_log`

```go
type logInput struct {
    MaxCount int    `json:"max_count"`
    Path     string `json:"path"`
    OneLine  bool   `json:"one_line"`
}

func (t *Log) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
    n := inp.MaxCount
    if n <= 0 { n = 20 }

    args := []string{"log", fmt.Sprintf("-n%d", n)}
    if inp.OneLine {
        args = append(args, "--oneline")
    } else {
        args = append(args, "--pretty=format:%C(auto)%h %as %<(20,trunc)%an  %s")
    }
    if inp.Path != "" {
        args = append(args, "--", inp.Path)
    }
    return runGit(ctx, t.WorkDir, args...)
}
```

### 4.1 Custom format

The default format `%h %as %<(20,trunc)%an  %s` produces:

```
abc1234 2024-01-15 Peter Hanssens       fix: auth token validation
def5678 2024-01-14 Jane Smith           feat: add user profile endpoint
ghi9012 2024-01-13 Peter Hanssens       refactor: extract middleware
```

Fields:
- `%h` — abbreviated commit hash (7 chars)
- `%as` — author date (YYYY-MM-DD)
- `%<(20,trunc)%an` — author name, left-padded to 20 chars, truncated
- `%s` — commit subject

This is denser than `--oneline` (which omits date and author) but much more
compact than the default multi-line format. It gives the model enough context
to identify relevant commits for further inspection.

**`%C(auto)`** enables automatic colour output only when writing to a terminal.
Since we capture stdout, colour is disabled automatically. We don't need
`--no-color`.

### 4.2 Path filtering

`git log -- path` limits history to commits that touched `path`. This is
invaluable for understanding why a file looks the way it does:

```
git_log(path: "src/auth.go", one_line: true)
    → last 20 commits that modified src/auth.go
```

The `--` separator before the path is important: it tells git that what
follows is a path, not a branch name. Without `--`, a file named `main`
would be interpreted as a branch name.

---

## 5. `git_add`

```go
type addInput struct {
    Paths []string `json:"paths"`
}

func (t *Add) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
    var inp addInput
    json.Unmarshal(rawInput, &inp)
    args := []string{"add"}
    if len(inp.Paths) == 0 {
        args = append(args, "-A")
    } else {
        args = append(args, inp.Paths...)
    }
    return runGit(ctx, t.WorkDir, args...)
}
```

### 5.1 Empty paths → `git add -A`

An empty `paths` array means "stage everything". `git add -A` stages all
changes: new files, modifications, and deletions. This is the most common
case — when the model has made changes to multiple files, it typically wants
to stage them all.

If the model passes specific paths, only those are staged. This matters when:
- Some changes should go in a separate commit
- Some files (e.g. `.env`, debug logs) should not be committed
- Partial staging is needed for a cleaner history

### 5.2 Output

`git add` produces no output on success (same as the shell command). We return
whatever `runGit` produces, which may be empty. The empty string is fine here —
the model expects no output from a successful `git add`. If there's an error
(file doesn't exist, permissions issue), `runGit` returns a non-nil error with
the git error message.

### 5.3 Permission

`NeedsPermission: true` because staging changes modifies the git index, which
is part of the repository state. While staging is not irreversible (you can
`git reset HEAD` to unstage), it's a write operation that the user should
be aware of.

---

## 6. `git_commit`

```go
type commitInput struct {
    Message    string `json:"message"`
    AllowEmpty bool   `json:"allow_empty"`
}

func (t *Commit) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
    var inp commitInput
    json.Unmarshal(rawInput, &inp)
    if inp.Message == "" {
        return "", fmt.Errorf("git_commit: message cannot be empty")
    }
    args := []string{"commit", "-m", inp.Message}
    if inp.AllowEmpty { args = append(args, "--allow-empty") }
    return runGit(ctx, t.WorkDir, args...)
}
```

### 6.1 Message validation

An empty commit message is an error caught before calling git. `git commit -m ""`
would also fail, but with a less clear error message. Validating upfront gives
the model an immediate, actionable error.

### 6.2 Conventional commits

The description steers the model toward conventional commit format:

```
"Use conventional commit format: type(scope): description"
```

Examples: `feat(auth): add JWT token refresh`, `fix(api): handle nil pointer
in handler`, `refactor(db): extract query builder`.

This isn't enforced — the model can write any message. But mentioning the
convention in the description increases the likelihood of consistent,
machine-readable commit history.

### 6.3 `allow_empty`

`git commit` fails if there are no staged changes. The `allow_empty` flag is
useful for creating checkpoint commits (e.g. `chore: checkpoint before
refactoring`) or for testing CI pipelines without code changes. It defaults
to false.

### 6.4 Successful output

`git commit` on success produces output like:

```
[main abc1234] feat: add user authentication
 2 files changed, 45 insertions(+), 3 deletions(-)
 create mode 100644 src/auth.go
```

This is returned directly to the model, confirming the commit hash, branch,
message, and change statistics. The model can use the hash in subsequent
operations.

### 6.5 Pre-commit hooks

Native `git commit` runs pre-commit hooks configured in `.git/hooks/pre-commit`.
This is correct behaviour — if the user has a linter or formatter hook, it
should run on model-generated commits too. If the hook fails, `runGit` returns
the hook's stderr as the error message, which the model can interpret and act on
(e.g. run the formatter and retry).

This is one of the strongest arguments for using native `git` over `go-git` —
hooks are a fundamental part of many development workflows, and silently
skipping them would produce commits that fail the CI pipeline.

---

## 7. `git_push`

```go
type pushInput struct {
    Remote string `json:"remote"`
    Branch string `json:"branch"`
    Force  bool   `json:"force"`
}

func (t *Push) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
    remote := inp.Remote
    if remote == "" { remote = "origin" }

    args := []string{"push", remote}
    if inp.Branch != "" { args = append(args, inp.Branch) }
    if inp.Force { args = append(args, "--force-with-lease") }
    return runGit(ctx, t.WorkDir, args...)
}
```

### 7.1 `--force-with-lease` not `--force`

When the model requests `force: true`, we use `--force-with-lease` instead
of `--force`. This is one of the most important safety decisions in the git
package.

`git push --force` unconditionally overwrites the remote branch, even if
someone else pushed commits since your last fetch. It can silently discard
other people's work.

`git push --force-with-lease` adds a safety check: it only force-pushes if
the remote branch is in the state you last saw it (i.e. you haven't missed
any new commits). If someone else pushed in the meantime, it refuses with:

```
error: failed to push some refs
hint: Updates were rejected because the tip of your current branch is behind
```

This preserves the model's ability to rewrite history (amend commits, rebase,
squash) while preventing accidental overwrites of collaborators' work. There
is no situation where the model should use `--force` instead of
`--force-with-lease` — we simply don't expose the more dangerous option.

### 7.2 Push output

`git push` produces progress output and a summary:

```
Enumerating objects: 5, done.
Counting objects: 100% (5/5), done.
Delta compression using up to 10 threads
Compressing objects: 100% (3/3), done.
Writing objects: 100% (3/3), 789 bytes | 789.00 KiB/s, done.
Total 3 (delta 1), reused 0 (delta 0), pack-reused 0
To github.com:cloudshuttle/drover-code.git
   abc1234..def5678  main -> main
```

The last line (`abc1234..def5678  main -> main`) is what the model cares about
— the old commit, new commit, and branch. We return all output from `runGit`,
which includes this summary.

Note: git push progress goes to stderr (the counting/compressing lines),
while the final summary goes to both. Our `runGit` discards stderr on success.
The model sees only the clean summary line.

### 7.3 Authentication

Push requires authentication to the remote. `runGit` inherits the process
environment and `git`'s configured credential helpers, so SSH key
authentication, HTTPS credential helpers, and token-based auth all work
transparently.

For the GitHub webhook runner, the clone URL includes the token embedded:
`https://x-access-token:<token>@github.com/owner/repo.git`. Push operations
from within the cloned repo use this embedded token automatically. No special
handling needed.

---

## 8. `git_create_branch`

```go
type createBranchInput struct {
    Name    string `json:"name"`
    Checkout bool  `json:"checkout"`
    FromRef  string `json:"from_ref"`
}

func (t *CreateBranch) Execute(ctx context.Context, rawInput json.RawMessage) (string, error) {
    var inp createBranchInput
    json.Unmarshal(rawInput, &inp)
    if inp.Name == "" {
        return "", fmt.Errorf("git_create_branch: name cannot be empty")
    }

    checkout := true  // default to true
    if bytes.Contains(rawInput, []byte(`"checkout"`)) {
        checkout = inp.Checkout
    }

    var args []string
    if checkout {
        args = []string{"checkout", "-b", inp.Name}
    } else {
        args = []string{"branch", inp.Name}
    }
    if inp.FromRef != "" {
        args = append(args, inp.FromRef)
    }
    return runGit(ctx, t.WorkDir, args...)
}
```

### 8.1 Default checkout: true

When creating a branch, the most common intent is to switch to it immediately.
`checkout: true` (the default) uses `git checkout -b` which creates and switches
in one command. `checkout: false` uses `git branch` which creates without
switching.

The default detection `bytes.Contains(rawInput, []byte('"checkout"'))` checks
whether the model explicitly included `checkout` in the input. If it didn't,
we default to true. This is a slightly awkward way to distinguish "model
omitted the field" from "model explicitly said false" — a cleaner approach
would use a `*bool` pointer in the input struct.

### 8.2 `from_ref`

Creating branches from a specific ref is important for:
- Feature branches from the latest remote main: `from_ref: "origin/main"`
- Bug fix branches from a release tag: `from_ref: "v1.2.3"`
- Experimental branches from a specific commit: `from_ref: "abc1234"`

Without `from_ref`, the branch is created from `HEAD` (the current commit).

---

## 9. Typical Git Workflow

The model should follow this workflow for any change involving version control:

```
1. git_status
       → understand current state; ensure clean working tree

2. [make changes via edit_file/write_file]

3. bash(command: "go test ./...")   OR   bash(command: "npm test")
       → verify changes before committing

4. git_diff
       → review what changed

5. git_add(paths: ["src/auth.go", "tests/auth_test.go"])
       → stage specific files

6. git_status
       → verify only intended files are staged

7. git_commit(message: "feat(auth): add token refresh endpoint")

8. git_push
       → if requested
```

Steps 1 and 6 (double status check) are a guard pattern: check the state
before touching anything, then check again before committing to make sure
staging did what you intended. The model should include these even when they
seem redundant.

---

## 10. `web_fetch`

### 10.1 Use cases

- Reading documentation (API docs, framework guides)
- Checking package versions (PyPI, npm, crates.io)
- Reading GitHub issues and PRs for context
- Fetching configuration file examples
- Looking up error messages in issue trackers

`web_fetch` is intentionally not a browser — it doesn't execute JavaScript,
doesn't render CSS, and doesn't follow client-side routing. It fetches the raw
HTTP response and optionally strips HTML tags. For JavaScript-heavy SPAs, it
returns whatever the server sends before JavaScript execution — often a nearly
empty skeleton. This is a known limitation.

### 10.2 HTTP client configuration

```go
client: &http.Client{
    Timeout: 30 * time.Second,
    CheckRedirect: func(req *http.Request, via []*http.Request) error {
        if len(via) >= 10 {
            return fmt.Errorf("too many redirects")
        }
        return nil
    },
}
```

**30-second timeout** covers most web requests including slow documentation
sites, while preventing the tool from blocking indefinitely on unreachable hosts
or slow CDNs.

**Redirect following** is enabled by default (Go's `http.Client` follows
redirects automatically). The 10-redirect limit prevents redirect loops.
Limits to 10 rather than Go's default 10 — same value, but explicit.

**No TLS configuration customisation.** We use the default TLS settings, which
means standard certificate validation. Self-signed certificates will fail.
This is correct for an internet-facing fetch tool — the model should not be
fetching from hosts with invalid certificates in normal usage.

### 10.3 Request headers

```go
req.Header.Set("User-Agent", "drover-code/1.0 (...)")
req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")
```

**User-Agent** identifies the client. This is good practice: server
administrators can see what's making requests to their servers. Some servers
block requests with no user agent or with obvious bot user agents. A descriptive
user agent that identifies itself as an AI assistant is transparent and allows
server admins to make informed blocking decisions.

**Accept header** declares that we prefer HTML, then XHTML, then plain text,
then anything. This matches what a browser would send and increases the chance
of getting the human-readable page rather than a machine-readable API response.

### 10.4 Response size cap

```go
limited := io.LimitReader(resp.Body, maxFetchBytes)  // 2 MB
body, err := io.ReadAll(limited)
```

2 MB is enough to capture most documentation pages, README files, and API
responses. Pages larger than 2 MB are typically not useful for the model
anyway — a 10 MB page is usually a generated page or an unusually large resource
that isn't what the model is looking for.

Note: we do not tell the server about this cap (no `Range` header). The server
sends the full response; we simply stop reading after 2 MB. This wastes bandwidth
for large pages, but implementing Range requests would require checking that
the server supports them first.

### 10.5 HTML stripping

```go
func htmlToText(html string) string {
    var b strings.Builder
    inTag := false
    inScript := false
    inStyle := false

    // ... character-by-character state machine ...

    // Post-processing
    result = strings.ReplaceAll(result, "&amp;",  "&")
    result = strings.ReplaceAll(result, "&lt;",   "<")
    result = strings.ReplaceAll(result, "&gt;",   ">")
    result = strings.ReplaceAll(result, "&quot;", `"`)
    result = strings.ReplaceAll(result, "&#39;",  "'")
    result = strings.ReplaceAll(result, "&nbsp;", " ")

    // Collapse blank lines
    for strings.Contains(result, "\n\n\n") {
        result = strings.ReplaceAll(result, "\n\n\n", "\n\n")
    }
    return strings.TrimSpace(result)
}
```

The HTML stripper is intentionally minimal — a state machine with three states
(in tag, in script, in style). It:

- Removes all HTML tags
- Suppresses content inside `<script>` and `<style>` blocks
- Converts block-level elements (`<p>`, `<br>`, `<div>`, `<li>`, `<tr>`,
  `<h1>`–`<h6>`) to newlines
- Decodes the six most common HTML entities
- Collapses excessive blank lines

**What it doesn't do:**
- Parse tables into structured output
- Handle nested elements specially (e.g. `<pre>` code blocks)
- Decode all HTML entities (there are hundreds)
- Handle malformed HTML gracefully (it does handle it — malformed tags are
  just ignored by the state machine, but the output may be garbled)

For most documentation pages, this produces readable output. The model can
extract the information it needs even from imperfect text extraction. A full
HTML-to-text library would produce better results but adds a dependency —
reserved for Phase 3.

### 10.6 Output format

```
URL: https://pkg.go.dev/net/http
Status: 200
Content-Type: text/html; charset=utf-8

<stripped text content>
```

The header block tells the model:
- Which URL was actually fetched (after any redirects)
- The HTTP status (in case it's 200 OK but the page content indicates an error)
- The content type (helps interpret the body)

### 10.7 Non-HTML content

```go
if !inp.Raw && strings.Contains(contentType, "text/html") {
    result = htmlToText(result)
}
```

HTML stripping only applies to `text/html` responses. JSON, plain text, and
other content types are returned as-is. This means `web_fetch` works correctly
for:

- REST APIs returning JSON
- Raw text files hosted on GitHub (`raw.githubusercontent.com`)
- Plain text documentation
- RSS/Atom feeds (XML)

Setting `raw: true` bypasses HTML stripping even for HTML responses — useful
when the model needs to inspect the HTML structure itself.

### 10.8 Error handling

```go
if resp.StatusCode < 200 || resp.StatusCode >= 400 {
    return "", fmt.Errorf("web_fetch: HTTP %d for %s", resp.StatusCode, inp.URL)
}
```

HTTP error responses (4xx, 5xx) are returned as tool errors, not as content.
This prevents the model from interpreting an error page as valid content (e.g.
a 404 page that says "Not Found" might look like a valid response if we didn't
check the status code).

3xx responses are followed automatically by the HTTP client — we never see
them unless the redirect limit is exceeded.

---

## 11. Testing Strategy

### Git tools: the test repository pattern

Git tests require an actual git repository with a known state. Create it as a
test fixture:

```go
func setupTestRepo(t *testing.T) (string, func()) {
    t.Helper()
    dir := t.TempDir()

    run := func(args ...string) {
        cmd := exec.Command("git", args...)
        cmd.Dir = dir
        cmd.Env = append(os.Environ(),
            "GIT_AUTHOR_NAME=Test",
            "GIT_AUTHOR_EMAIL=test@test.com",
            "GIT_COMMITTER_NAME=Test",
            "GIT_COMMITTER_EMAIL=test@test.com",
        )
        require.NoError(t, cmd.Run())
    }

    run("init", "-b", "main")
    run("config", "user.email", "test@test.com")
    run("config", "user.name", "Test User")

    // Create initial commit
    os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test"), 0o644)
    run("add", ".")
    run("commit", "-m", "initial commit")

    cleanup := func() { os.RemoveAll(dir) }
    return dir, cleanup
}
```

Setting `GIT_AUTHOR_NAME` etc. via environment ensures test commits have
deterministic authorship, independent of the tester's git config.

### `git_status` tests

```go
repoDir, cleanup := setupTestRepo(t)
defer cleanup()

statusTool := &git.Status{WorkDir: repoDir}

// Clean working tree
result, err := statusTool.Execute(ctx, nil)
assertNoError(t, err)
assertContains(t, result, "## main")
assertNotContains(t, result, "M ")  // no modifications

// Unstaged modification
os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Modified"), 0o644)
result, err = statusTool.Execute(ctx, nil)
assertContains(t, result, " M README.md")

// Staged modification
exec.Command("git", "-C", repoDir, "add", "README.md").Run()
result, err = statusTool.Execute(ctx, nil)
assertContains(t, result, "M  README.md")  // staged (M in first column)

// Untracked file
os.WriteFile(filepath.Join(repoDir, "newfile.go"), []byte("package main"), 0o644)
result, err = statusTool.Execute(ctx, nil)
assertContains(t, result, "?? newfile.go")
```

### `git_diff` tests

```go
// Unstaged diff
os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Modified\n"), 0o644)
result, err := diffTool.Execute(ctx, marshal(`{}`))
assertContains(t, result, "-# Test")
assertContains(t, result, "+# Modified")

// Staged diff
exec.Command("git", "-C", repoDir, "add", "README.md").Run()
result, err = diffTool.Execute(ctx, marshal(`{"staged": true}`))
assertContains(t, result, "+# Modified")

// No changes
result, err = diffTool.Execute(ctx, marshal(`{}`))  // after staging, no unstaged
assertContains(t, result, "no changes")
```

### `git_commit` tests

```go
// Stage a change
os.WriteFile(filepath.Join(repoDir, "file.go"), []byte("package main"), 0o644)
exec.Command("git", "-C", repoDir, "add", ".").Run()

// Commit
result, err := commitTool.Execute(ctx, marshal(`{"message": "feat: add file"}`))
assertNoError(t, err)
assertContains(t, result, "feat: add file")

// Empty message fails
_, err = commitTool.Execute(ctx, marshal(`{"message": ""}`))
assertError(t, err)
assertContains(t, err.Error(), "message cannot be empty")

// Nothing staged
_, err = commitTool.Execute(ctx, marshal(`{"message": "empty"}`))
assertError(t, err)
assertContains(t, err.Error(), "nothing to commit")  // git's message
```

### `web_fetch` tests

```go
// Use httptest.Server for controlled responses
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    switch r.URL.Path {
    case "/html":
        w.Header().Set("Content-Type", "text/html")
        fmt.Fprint(w, `<html><body><h1>Hello</h1><p>World</p></body></html>`)
    case "/json":
        w.Header().Set("Content-Type", "application/json")
        fmt.Fprint(w, `{"key": "value"}`)
    case "/large":
        w.Header().Set("Content-Type", "text/plain")
        // Write 3 MB of content
        for i := 0; i < 3*1024; i++ {
            w.Write(make([]byte, 1024))
        }
    case "/404":
        w.WriteHeader(404)
        fmt.Fprint(w, "Not Found")
    case "/redirect":
        http.Redirect(w, r, "/html", 302)
    }
}))
defer srv.Close()

fetchTool := web.NewFetch()

// HTML stripping
result, err := fetchTool.Execute(ctx, marshal(fmt.Sprintf(`{"url":"%s/html"}`, srv.URL)))
assertNoError(t, err)
assertContains(t, result, "Hello")
assertContains(t, result, "World")
assertNotContains(t, result, "<h1>")
assertNotContains(t, result, "<p>")

// JSON passthrough
result, err = fetchTool.Execute(ctx, marshal(fmt.Sprintf(`{"url":"%s/json"}`, srv.URL)))
assertContains(t, result, `"key": "value"`)
assertContains(t, result, "application/json")

// Size cap
result, err = fetchTool.Execute(ctx, marshal(fmt.Sprintf(`{"url":"%s/large"}`, srv.URL)))
assertNoError(t, err)
assert(t, len(result) < 3*1024*1024)  // capped at 2 MB

// HTTP error
_, err = fetchTool.Execute(ctx, marshal(fmt.Sprintf(`{"url":"%s/404"}`, srv.URL)))
assertError(t, err)
assertContains(t, err.Error(), "HTTP 404")

// Redirect following
result, err = fetchTool.Execute(ctx, marshal(fmt.Sprintf(`{"url":"%s/redirect"}`, srv.URL)))
assertNoError(t, err)
assertContains(t, result, "Hello")  // followed redirect to /html

// Invalid URL (not http/https)
_, err = fetchTool.Execute(ctx, marshal(`{"url": "ftp://example.com"}`))
assertError(t, err)
assertContains(t, err.Error(), "must start with http")
```

---

## 12. Edge Cases and Known Issues

### `git_push`: authentication failure messaging

When push fails due to authentication (expired token, SSH key not authorised),
git's error message varies by remote type and git version. The model may not
immediately understand "Repository not found" (GitHub's auth failure message
for private repos with insufficient credentials). The description should note
that push failures often indicate credential issues.

### `git_diff`: binary files

`git diff` on binary files produces output like:
```
Binary files a/image.png and b/image.png differ
```

This is handled correctly — it's returned as text and the model understands it.
We don't need special handling for binary diffs.

### `git_log`: detached HEAD

On a detached HEAD (e.g. after `git checkout <commit>`), `git log` works
correctly but the `## main` branch indicator in `git_status` is replaced with
`## HEAD (no branch)`. The model should handle this but it's worth testing
explicitly.

### `git_commit`: GPG signing

If the user has `commit.gpgsign = true` in their git config and their GPG key
requires a passphrase, `git commit` will block waiting for the passphrase. This
hangs the tool indefinitely (until the timeout). No fix in the current design
— if GPG signing is configured, the user needs to ensure their key is unlocked
before using drover-code.

### `web_fetch`: relative URLs in redirects

Some servers send `Location: /path` (relative) in redirect responses. Go's
`http.Client` handles this correctly — it resolves relative redirect URLs
against the original request URL. No special handling needed.

### `web_fetch`: JavaScript-rendered content

Many modern documentation sites (React-based SPAs) render content client-side.
`web_fetch` sees the initial HTML skeleton, not the rendered content. For
sites like this, the model should use the raw API endpoints (often available
at different paths) rather than the human-facing URL.

---

## 13. Future Considerations

### `git_stash`

```go
// git stash / git stash pop
// Useful for the model to "save" work before trying a different approach
```

Stash management would be valuable for exploratory workflows where the model
wants to try something and revert if it doesn't work.

### `git_rebase`

Interactive rebase (`git rebase -i`) would allow the model to help clean up
commit history before pushing. High-risk, complex to implement safely.

### `git_blame`

Showing which commit introduced each line of a file — useful for understanding
why code looks the way it does. Output is verbose; would need careful
truncation.

### `web_fetch`: JavaScript execution via `chromedp`

For JavaScript-rendered pages, integrate with a headless Chrome instance using
`github.com/chromedp/chromedp`. This would allow fetching fully-rendered pages
but adds significant complexity and a non-trivial binary dependency.
Appropriate as an optional enhancement, not a default.

### `web_fetch`: robots.txt compliance

Parse and respect `robots.txt` before fetching a URL. This is good practice
for an AI assistant that may make many requests to the same domain. Adds a
second HTTP request per fetch (to get robots.txt). Could be cached.

### Authentication for `web_fetch`

Some URLs require authentication: private GitHub repos, documentation behind
login walls, internal company wikis. A `headers` field in the input would
allow the model to pass auth tokens:

```json
{
    "url": "https://api.internal.company.com/docs",
    "headers": {"Authorization": "Bearer <token>"}
}
```

Security concern: the model might be manipulated into passing sensitive tokens
to untrusted URLs. Requires careful thought about what URLs are allowed to
receive auth headers.

---

*Previous: [`05-shell-search-tools.md`](./05-shell-search-tools.md)*  
*Next: [`07-tui.md`](./07-tui.md) — BubbleTea Terminal UI*
