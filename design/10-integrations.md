# 10 — Integrations: IDE Bridge and GitHub @drover-code Webhook

**Packages:** `internal/bridge`, `internal/github`  
**Files:** `bridge/bridge.go`, `github/types.go`, `github/client.go`,
`github/parser.go`, `github/runner.go`, `github/server.go`  
**Depends on:** `internal/agent`, `internal/api`, `internal/tools`  
**Depended on by:** `cmd/drover-code`

---

## Purpose

These two packages extend drover-code beyond the terminal session. The IDE bridge
lets VS Code and JetBrains users interact with drover-code without leaving their
editor. The GitHub webhook integration lets any repository member invoke the
agent by mentioning `@drover-code` in a comment, with no local tooling required.

Both are protocol adapters — they translate external interfaces (JSON-RPC over
stdio, HTTP webhooks) into the same agent loop and tool system the TUI uses.
From the agent loop's perspective, an IDE session and a GitHub session look
identical.

---

## 1. IDE Bridge (`bridge/bridge.go`)

### 1.1 Protocol: LSP wire format

The bridge uses the Language Server Protocol (LSP) wire format:

```
Content-Length: <N>\r\n
\r\n
<N bytes of JSON-RPC 2.0>
```

This is not LSP (we don't implement any LSP methods), but the wire format is
borrowed because:
- VS Code's extension API has built-in support for length-prefixed JSON
- Every language server uses it, so every editor extension developer knows it
- It handles binary-safe framing without a custom parser
- The original Claude Code CLI uses the same format

The JSON-RPC 2.0 envelope:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "drover/execute",
    "params": {"input": "refactor the auth module"}
}
```

Requests have an `id` field and expect a response. Notifications (no `id`)
are fire-and-forget.

### 1.2 The `Bridge` struct

```go
type Bridge struct {
    r        *bufio.Reader      // reads from stdin
    w        io.Writer          // writes to stdout
    wMu      sync.Mutex         // serialises writes (multiple goroutines may respond)
    handlers map[string]Handler // method → handler function
    nextID   atomic.Int64       // for outgoing requests
    pendingMu sync.Mutex
    pending  map[int64]chan Message  // in-flight outgoing requests
}
```

**Why `sync.Mutex` on writes?** Multiple goroutines may be handling concurrent
requests and sending responses simultaneously. Without serialisation, two
goroutines could interleave their length-prefix headers and bodies, producing
malformed output. The write mutex ensures each message is written atomically.

**`pending` map for outgoing requests.** When the bridge makes a request to
the extension (e.g. asking for a file's content), it needs to match the
response to the request. The `pending` map stores a per-request channel keyed
by the request ID. When a response arrives on the read loop, it's matched by
ID and sent on the channel.

### 1.3 The read loop

```go
func (b *Bridge) Run(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        msg, err := b.readMessage()
        if err != nil {
            if err == io.EOF { return nil }  // extension disconnected
            return fmt.Errorf("bridge: read: %w", err)
        }

        // Route response messages to pending callers
        if msg.ID != nil && msg.Method == "" {
            b.pendingMu.Lock()
            ch, ok := b.pending[*msg.ID]
            b.pendingMu.Unlock()
            if ok {
                ch <- msg
                continue
            }
        }

        // Dispatch to registered handler
        h, ok := b.handlers[msg.Method]
        if !ok {
            if msg.ID != nil {
                b.SendError(*msg.ID, -32601, "method not found: "+msg.Method)
            }
            continue
        }

        go h(ctx, msg, b.Send)
    }
}
```

**Handlers run on goroutines.** Each incoming request is dispatched to a new
goroutine. This means multiple requests can be handled concurrently — if the
extension sends `ping` while `drover/execute` is running, `ping` responds
immediately without waiting for the agent to finish.

**Response routing.** A message with `ID != nil` and `Method == ""` is a
response to an outgoing request. It's routed to the waiting goroutine via the
`pending` map. This handles the case where the bridge makes a request to the
extension (bidirectional communication).

### 1.4 Message framing: `readMessage`

```go
func (b *Bridge) readMessage() (Message, error) {
    var contentLength int

    // Read headers until blank line
    for {
        line, err := b.r.ReadString('\n')
        if err != nil { return Message{}, err }
        line = strings.TrimRight(line, "\r\n")
        if line == "" { break }  // blank line = end of headers
        if strings.HasPrefix(line, "Content-Length:") {
            n, _ := strconv.Atoi(strings.TrimSpace(
                strings.TrimPrefix(line, "Content-Length:")))
            contentLength = n
        }
    }

    if contentLength == 0 {
        return Message{}, fmt.Errorf("missing Content-Length")
    }

    body := make([]byte, contentLength)
    if _, err := io.ReadFull(b.r, body); err != nil {
        return Message{}, fmt.Errorf("read body: %w", err)
    }

    var msg Message
    return msg, json.Unmarshal(body, &msg)
}
```

**`io.ReadFull`** reads exactly `contentLength` bytes. Without this, a partial
read (e.g. due to network buffering) would silently produce a truncated message.
`io.ReadFull` blocks until all bytes arrive or returns an error.

**The `bufio.Reader` (1 MB buffer).** JSON messages from the IDE can be large —
if the extension sends file contents or diff data as part of a request, a
64 KB buffer would cause multiple `ReadString` calls per message. The 1 MB
buffer handles most messages in a single read.

**`ReadString('\n')` for header lines.** LSP headers always end in `\r\n`, but
`ReadString('\n')` captures both by scanning to `\n` and including it. The
`strings.TrimRight(line, "\r\n")` strips both carriage return and newline,
handling platforms that use bare `\n` in addition to `\r\n`.

### 1.5 Standard handlers

```go
func RegisterStandardHandlers(b *Bridge, agentFn func(ctx context.Context, input string) (string, error)) {
    b.Handle("initialize", func(ctx context.Context, msg Message, send func(Message)) {
        if msg.ID == nil { return }
        send(Message{
            ID: msg.ID,
            Result: mustMarshal(map[string]any{
                "capabilities": map[string]any{
                    "execute":      true,
                    "streamTokens": true,
                },
                "serverInfo": map[string]any{
                    "name":    "drover-code",
                    "version": "0.1.0",
                },
            }),
        })
    })

    b.Handle("drover/execute", func(ctx context.Context, msg Message, send func(Message)) {
        var params struct { Input string `json:"input"` }
        json.Unmarshal(msg.Params, &params)
        result, err := agentFn(ctx, params.Input)
        if err != nil {
            b.SendError(*msg.ID, -32603, err.Error())
            return
        }
        send(Message{ID: msg.ID, Result: mustMarshal(map[string]any{"output": result})})
    })

    b.Handle("ping", func(ctx context.Context, msg Message, send func(Message)) {
        if msg.ID != nil {
            send(Message{ID: msg.ID, Result: mustMarshal("pong")})
        }
    })
}
```

**`initialize`** is the handshake. The extension calls it on connection to
discover what the server supports. `capabilities.streamTokens: true` indicates
that the server can stream token-by-token updates (via notifications) in
addition to returning complete responses. The VS Code extension uses this to
show streaming output.

**`drover/execute`** is the primary method. The extension sends the user's
input and receives the complete response after the agent loop finishes. For
streaming behaviour, the extension also subscribes to `drover/textDelta`
notifications that the bridge sends as tokens arrive.

**Token streaming in bridge mode.** In TUI mode, text deltas go to the
viewport. In bridge mode, they should go to the IDE. The current implementation
collects the complete output and returns it on `drover/execute` completion —
streaming is not yet wired up. A complete implementation would:

```go
// In the agentFn closure:
for ev := range eventCh {
    if td, ok := ev.(agent.TextDeltaEvent); ok {
        b.Notify("drover/textDelta", map[string]string{"text": td.Text})
    }
}
```

### 1.6 Bridge mode vs TUI mode

When `DROVER_CODE_IDE_BRIDGE=1`:
- The TUI is not started
- stdin/stdout carry the bridge protocol
- The agent loop runs headlessly
- Events are collected but not displayed
- Responses are returned via `drover/execute` responses

This means two instances of drover-code can't easily share a terminal — one
would use the TUI, the other the bridge protocol. In practice, the extension
launches its own drover-code subprocess and communicates with it exclusively.

### 1.7 Testing strategy

```go
// Use io.Pipe to simulate stdin/stdout
clientR, serverW := io.Pipe()
serverR, clientW := io.Pipe()

bridge := bridge.NewBridge(serverR, serverW)
client := bridge.NewBridge(clientR, clientW)

// Register a test handler
bridge.Handle("test/echo", func(ctx context.Context, msg bridge.Message, send func(bridge.Message)) {
    send(bridge.Message{ID: msg.ID, Result: msg.Params})
})

ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

go bridge.Run(ctx)

// Send a request from the client side
result, err := client.Request(ctx, "test/echo", map[string]string{"hello": "world"})
assertNoError(t, err)
assert(t, string(result) == `{"hello":"world"}`)

// Unknown method returns error
_, err = client.Request(ctx, "nonexistent/method", nil)
assertError(t, err)
assert(t, strings.Contains(err.Error(), "method not found"))

// Concurrent requests are handled independently
var wg sync.WaitGroup
for i := 0; i < 10; i++ {
    wg.Add(1)
    go func(n int) {
        defer wg.Done()
        result, err := client.Request(ctx, "test/echo",
            map[string]int{"n": n})
        assertNoError(t, err)
        // verify result contains n
    }(i)
}
wg.Wait()

// Initialize handshake
result, err = client.Request(ctx, "initialize", nil)
assertNoError(t, err)
var capabilities map[string]any
json.Unmarshal(result, &capabilities)
assert(t, capabilities["capabilities"] != nil)

// Message framing: large payload
largePayload := strings.Repeat("x", 100_000)
result, err = client.Request(ctx, "test/echo",
    map[string]string{"data": largePayload})
assertNoError(t, err)
// verify large payload round-tripped correctly
```

---

## 2. GitHub @drover-code Webhook (`internal/github/`)

### 2.1 Overview

The GitHub integration enables this workflow:

```
Developer opens a PR
Developer comments: "@drover-code please review this for security issues"
GitHub sends webhook to drover-code server
drover-code:
    1. Posts "Processing..." placeholder comment
    2. Clones the repository at the PR branch
    3. Runs the agent against the cloned repo
    4. Updates the placeholder with the agent's response
```

No local tooling required. The developer never leaves GitHub.

### 2.2 Package structure

| File | Responsibility |
|---|---|
| `types.go` | GitHub payload shapes, Trigger, ReplyTarget |
| `client.go` | REST API client, HMAC signature verification |
| `parser.go` | @drover-code mention extraction from webhook payloads |
| `runner.go` | Placeholder → clone → agent → update comment |
| `server.go` | HTTP server, signature verification, deduplication, rate limiting |

The separation allows testing each layer independently. The parser can be
tested with static JSON without an HTTP server. The runner can be tested with
a mock GitHub client without a real repository.

### 2.3 Trigger extraction (`parser.go`)

```go
var mentionRe = regexp.MustCompile(`(?i)@drover-code\s+(.+)`)

func extractMention(s string) string {
    lines := strings.Split(s, "\n")
    var inRequest bool
    var parts []string
    for _, line := range lines {
        if mentionRe.MatchString(line) {
            inRequest = true
            parts = append(parts, mentionRe.FindStringSubmatch(line)[1])
            continue
        }
        if inRequest {
            trimmed := strings.TrimSpace(line)
            if trimmed == "" { break }           // blank line ends request
            if !strings.HasPrefix(trimmed, "@") { // continuation line
                parts = append(parts, trimmed)
            }
        }
    }
    if len(parts) == 0 { return "" }
    return strings.TrimSpace(strings.Join(parts, " "))
}
```

**Multi-line request support.** A developer can write:

```
@drover-code please review this authentication function
for timing attack vulnerabilities and suggest
the correct approach using constant-time comparison
```

The parser collects continuation lines until a blank line or a line starting
with `@`. The three lines above produce the single request:
`"please review this authentication function for timing attack vulnerabilities
and suggest the correct approach using constant-time comparison"`.

This is important: splitting the request at a newline would give the model an
incomplete instruction ("please review this authentication function") that
misses the developer's actual concern (timing attacks).

**Case-insensitive `(?i)`.** `@Drover-code`, `@DROVER-CODE`, and `@drover-code` all trigger.
GitHub usernames are case-insensitive; the mention should be too.

**Only fires on `action: "created"`.** Edited and deleted comments do not
re-trigger the agent. If a developer edits their `@drover-code` comment to clarify
the request, nothing happens. This is the correct behaviour — otherwise a
single comment could trigger multiple agent runs as it's edited. A future
enhancement: react to edits by posting a new response (not updating the
existing one).

### 2.4 Webhook signature verification (`client.go`)

```go
func VerifySignature(body []byte, signatureHeader, secret string) error {
    if signatureHeader == "" {
        return fmt.Errorf("missing X-Hub-Signature-256 header")
    }
    if !strings.HasPrefix(signatureHeader, "sha256=") {
        return fmt.Errorf("unexpected signature format: %s", signatureHeader)
    }
    gotHex := strings.TrimPrefix(signatureHeader, "sha256=")
    got, err := hex.DecodeString(gotHex)
    if err != nil {
        return fmt.Errorf("decode signature: %w", err)
    }
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(body)
    expected := mac.Sum(nil)
    if !hmac.Equal(got, expected) {
        return fmt.Errorf("signature mismatch")
    }
    return nil
}
```

**`hmac.Equal` for constant-time comparison.** This is critical. Comparing
HMAC values with `bytes.Equal` or `==` is vulnerable to timing attacks: an
attacker can measure the response time to determine how many bytes of their
forged signature are correct. `hmac.Equal` uses a constant-time comparison
that always takes the same time regardless of where the first mismatch occurs.

**Why verify the signature?** Without verification, anyone who knows your
webhook URL can send arbitrary events to your drover-code server — potentially
causing it to run agent sessions against repositories, post comments, make API
calls. The secret shared between GitHub and your server ensures only GitHub
can send valid events.

**If no secret is configured (`secret == ""`),** verification is skipped. This
is intentional for development environments where the webhook is exposed only
locally via `ngrok` or similar. Production deployments must set a secret.

### 2.5 GitHub API client

```go
type Client struct {
    token      string
    httpClient *http.Client
}
```

All API calls share a single `http.Client` with a 30-second timeout and
connection pooling. For concurrent webhook handler goroutines, the shared
client avoids creating a new connection per request.

**Key methods and their design:**

`PostIssueComment` — creates the placeholder comment and returns its ID. The
ID is used by `UpdateComment` to replace the placeholder with the real
response. This two-step (placeholder → update) pattern is essential: GitHub
webhooks must respond within 10 seconds, but agent runs can take minutes. The
placeholder gives the developer immediate feedback that the request was received.

`UpdateComment` — uses `PATCH /repos/{owner}/{repo}/issues/comments/{id}`.
Updating in-place rather than posting a new comment keeps the conversation
thread clean — the response appears right where the request was made.

`PostReviewComment` — posts an inline comment on a specific diff line. Always
sets `confirmed: true`, which in the Claude Code protocol means the comment
should actually be posted (not held as a draft). This prevents partial review
comments from appearing.

`CreateReviewWithComments` — posts multiple inline comments in a single API
call. More efficient than individual `PostReviewComment` calls when the agent
has multiple inline suggestions. Uses `event: "COMMENT"` (not "REQUEST_CHANGES"
or "APPROVE") to avoid unexpectedly blocking or approving PRs.

`GetPRDiff` — fetches the unified diff for a PR by setting
`Accept: application/vnd.github.diff`. This gives the model the actual changes
being reviewed without reading every modified file individually.

**Authentication:** `Authorization: Bearer <token>` in all requests. The token
can be a personal access token, a GitHub App installation token, or a fine-
grained token. Required scopes: `repo` (for private repos) or `public_repo`
(for public repos), plus `pull_requests` for review comments.

### 2.6 The runner: placeholder → clone → agent → update

```go
func (r *Runner) Handle(ctx context.Context, trigger *Trigger) error {
    // 1. Post placeholder immediately
    commentID, err := r.ghClient.PostIssueComment(
        ctx, target.Owner, target.Repo, target.Number,
        fmt.Sprintf("_Processing: %s…_", truncate(trigger.Request, 80)),
    )

    // 2. Run the agent (may take minutes)
    response, runErr := r.run(ctx, trigger)
    if runErr != nil {
        response = fmt.Sprintf("❌ An error occurred:\n\n```\n%s\n```", runErr.Error())
    }

    // 3. Update placeholder with real response (always runs, even on error)
    return r.ghClient.UpdateComment(ctx, target.Owner, target.Repo, commentID, response)
}
```

The three-step structure has a critical invariant: step 3 (update comment)
**always** runs, even if step 2 (agent) fails. Without this, a failed agent
run would leave the placeholder comment ("Processing...") visible forever,
giving no indication of the failure.

This is implemented with the `defer`-free approach above rather than
`defer` because `defer` would execute only on function return, which could
be after `runErr != nil` has modified `response`. The explicit call ensures
`response` has the correct value (success or error message) before the update.

### 2.7 Repository cloning

```go
func (r *Runner) cloneRepo(ctx context.Context, trigger *Trigger) (string, func(), error) {
    ref := trigger.Context.PRHead
    if ref == "" { ref = trigger.Context.Repo.DefaultBranch }
    if ref == "" { ref = "main" }

    jobDir := filepath.Join(r.workBase,
        fmt.Sprintf("job-%s-%d",
            strings.NewReplacer("/", "-", ".", "-").Replace(trigger.Context.Repo.FullName),
            trigger.ReplyTarget.Number,
        ),
    )
    cleanup := func() { os.RemoveAll(jobDir) }

    cloneURL := trigger.Context.Repo.CloneURL
    // Embed token for HTTPS authentication
    if r.ghClient.token != "" {
        cloneURL = strings.Replace(cloneURL,
            "https://github.com/",
            fmt.Sprintf("https://x-access-token:%s@github.com/", r.ghClient.token),
            1,
        )
    }

    if err := execGit(ctx, "", "clone", "--depth=1",
        "--branch="+ref, cloneURL, jobDir); err != nil {
        cleanup()
        return "", nil, err
    }
    return jobDir, cleanup, nil
}
```

**`--depth=1` shallow clone.** Only the latest commit on the branch is fetched.
This is dramatically faster and lighter than a full clone for large repositories.
The agent doesn't need git history — it needs the current code. `git log` in
the agent would only show the shallow history, but this is acceptable for PR
review tasks.

**Unique job directory per (repo, PR).** The directory name includes the full
repository name and PR/issue number. This ensures concurrent jobs for the same
PR don't conflict. The `cleanup()` function removes the directory after the job
completes. If the server crashes before cleanup, stale directories accumulate
in `workBase` — a periodic cleanup job would address this.

**Token embedded in clone URL.** `x-access-token:<token>@github.com` is the
standard GitHub pattern for HTTPS authentication with a token. The token is
embedded in the URL for the clone operation. Once cloned, the `.git/config`
contains the URL with the token, which git uses for subsequent operations
(like `git push` if the agent tries to push changes).

**Security note:** The clone URL with embedded token is visible in `git log`
output and in the `.git/config` file. Within the ephemeral job directory this
is acceptable, but care should be taken that the directory is not world-readable
and is cleaned up promptly.

### 2.8 System prompt for GitHub context

```go
func buildGitHubSystemPrompt(trigger *Trigger, repoDir string) string {
    var b strings.Builder

    fmt.Fprintf(&b, "You are an AI assistant helping with a GitHub repository.\n")
    fmt.Fprintf(&b, "Repository: %s\nWorking directory: %s\n\n",
        ctx.Repo.FullName, repoDir)

    if ctx.PRNumber > 0 {
        fmt.Fprintf(&b, "Pull Request #%d: %s\n", ctx.PRNumber, ctx.IssuTitle)
        if ctx.PRHead != "" {
            fmt.Fprintf(&b, "Branch: %s → %s\n", ctx.PRHead, ctx.PRBase)
        }
    }
    if ctx.IssueBody != "" {
        fmt.Fprintf(&b, "Description:\n%s\n\n", truncate(ctx.IssueBody, 1000))
    }
    if ctx.DiffContext != "" {
        fmt.Fprintf(&b, "Comment on %s (line %d):\n```diff\n%s\n```\n\n",
            ctx.FilePath, ctx.DiffLine, ctx.DiffContext)
    }

    b.WriteString("Available tools: read files, search code, run bash, git operations.\n")
    b.WriteString("Do not commit or push unless explicitly asked.\n")
    b.WriteString("Respond in GitHub Flavored Markdown. Be concise.\n")
    b.WriteString("Do not mention that you are an AI or include AI attribution footers.\n")
    return b.String()
}
```

**Diff context injection.** For review comments on specific diff lines, the
`DiffHunk` (surrounding diff context) is included in the system prompt. This
gives the model the exact code being reviewed without the agent needing to
find and read the file — saving a round-trip.

**"Do not commit or push unless explicitly asked."** This is the most important
safety instruction in the webhook context. Without it, an agent reviewing a PR
might decide to "helpfully" fix the issues it finds and push a commit. This
would be extremely surprising to the developer and potentially harmful.

**"Be concise."** GitHub comments are read in a PR review thread. Long,
verbose responses waste the reviewer's time. The model should aim for clear,
actionable feedback, not exhaustive analysis.

**Undercover mode in the system prompt.** The note "Do not mention that you
are an AI" is a simplified version of the undercover mode instruction. In the
webhook context, the bot's attribution footer (`_via drover-code_`) provides
transparency at the infrastructure level, so the model's response body can
read as natural human code review.

### 2.9 HTTP server and safety mechanisms

```go
type Server struct {
    runner     *Runner
    secret     string
    sem        chan struct{}         // semaphore: max 5 concurrent jobs
    mu         sync.Mutex
    activeJobs map[string]bool       // deduplication: one job per issue/PR
}
```

**Deduplication.** The `activeJobs` map prevents two concurrent agent runs
for the same issue or PR. If a PR receives multiple `@drover-code` mentions in
quick succession (e.g. the developer posts a comment, realises they forgot
something, posts a follow-up), only the first triggers an agent run. The
subsequent triggers are silently dropped while the first is in progress.

This avoids:
- Two agents posting conflicting responses to the same PR
- Double API and compute costs
- Race conditions in the shared job directory

After a job completes (success or error), the entry is removed from
`activeJobs`. A subsequent `@drover-code` mention then triggers a new run.

**Semaphore (max 5 concurrent).** Even with deduplication, multiple PRs across
different repositories can trigger simultaneous jobs. The semaphore caps total
concurrent agent sessions to 5. Additional jobs queue at the HTTP handler
(not blocking the HTTP response — they queue at the goroutine scheduler).

**Respond immediately to GitHub.** The HTTP handler returns `202 Accepted`
within the 10-second webhook delivery window, then dispatches the job
asynchronously:

```go
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
    // ... parse, verify, deduplicate ...

    // Respond to GitHub BEFORE starting the job
    w.WriteHeader(http.StatusAccepted)
    json.NewEncoder(w).Encode(map[string]string{
        "status": "accepted",
        "delivery": deliveryID,
    })

    // Dispatch asynchronously
    go func() {
        s.sem <- struct{}{}
        defer func() { <-s.sem }()
        ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
        defer cancel()
        s.runner.Handle(ctx, trigger)
    }()
}
```

GitHub retries webhook delivery if the server doesn't respond within 10 seconds.
Retries cause duplicate jobs. By responding immediately and tracking
`activeJobs`, we handle this correctly: the retry is received, deduplicated
(the job is still active), and the retry is silently dropped.

**10-minute job timeout.** Each job gets a fresh `context.WithTimeout` of 10
minutes. This prevents runaway jobs from consuming resources indefinitely.
If the agent is stuck in an infinite tool loop or the API is unresponsive,
the job times out, the error is captured, and the placeholder comment is
updated with an error message.

### 2.10 Response formatting

```go
func (r *Runner) run(ctx context.Context, trigger *Trigger) (string, error) {
    // ... clone, agent, collect output ...
    resp := strings.TrimSpace(output.String())
    if resp == "" { resp = "_No response generated._" }
    return resp + "\n\n---\n_via [drover-code](https://github.com/cloudshuttle/drover-code)_", nil
}
```

**Attribution footer.** Every response ends with `_via [drover-code]_`. This
serves two purposes:

1. **Transparency.** PR reviewers know the comment came from an automated tool,
   not a human teammate.
2. **Auditability.** If a bot comment causes a problem (wrong advice, error),
   the attribution makes it easy to identify and filter bot comments.

The footer uses italic markdown formatting and the repository link. It's subtle
enough not to distract from the response content but visible enough to be found.

Operators who prefer no footer can remove the string concatenation in `run()`.
A `WebhookAttribution: false` config option is a natural future addition.

### 2.11 Full deployment walkthrough

```bash
# 1. Build the binary
CGO_ENABLED=0 go build -o drover-code ./cmd/drover-code/

# 2. Set environment variables
export ANTHROPIC_API_KEY="sk-ant-..."
export GITHUB_TOKEN="ghp_..."                # PAT or App installation token
export GITHUB_WEBHOOK_SECRET="my-secret-123"  # set same value in GitHub webhook config
export WEBHOOK_ADDR=":8080"
export WEBHOOK_WORK_DIR="/tmp/drover-code-jobs"

# 3. Start the webhook server
./drover-code webhook

# 4. Configure GitHub webhook
# Payload URL: https://your-server.com:8080/webhooks/github
# Content type: application/json
# Secret: my-secret-123
# Events: Issue comments, Pull request review comments

# 5. For local development with ngrok:
ngrok http 8080
# Use the ngrok HTTPS URL as the payload URL (GitHub requires HTTPS)
```

### 2.12 GitHub App vs Personal Access Token

The webhook runner works with both authentication types:

**Personal Access Token (PAT)** is simpler to set up. Create a token with
`repo` and `pull_requests` scopes. Works immediately, no app registration
required. Disadvantage: the token is tied to a specific user account; if
that user loses access to the repository, the bot stops working.

**GitHub App** is the production-grade approach. Create an App, install it on
repositories, generate installation tokens. Advantages: tokens auto-rotate,
access can be scoped to specific repositories, the App appears as its own
actor in the GitHub UI (not as a user account). The App installation token
is generated programmatically and passed as `GITHUB_TOKEN`. The token
generation logic (calling `/app/installations/{id}/access_tokens`) is not
implemented in the current codebase — it requires the App's private key and
app ID.

### 2.13 Testing strategy

```go
// Parser: basic mention
comment := "Looking at this PR.\n\n@drover-code please review this for SQL injection"
trigger := github.ParseWebhookTest(github.EventIssueComment,
    buildCommentEvent("created", comment))
assert(t, trigger != nil)
assert(t, trigger.Request == "please review this for SQL injection")

// Parser: multi-line request
comment = "@drover-code please check this function\nfor timing attack vulnerabilities\n\nOther text"
trigger = parse(comment)
assert(t, strings.Contains(trigger.Request, "timing attack"))
assert(t, !strings.Contains(trigger.Request, "Other text"))

// Parser: edited comment doesn't trigger
trigger = parse(buildCommentEvent("edited", "@drover-code do something"))
assert(t, trigger == nil)

// Parser: no mention returns nil
trigger = parse(buildCommentEvent("created", "This looks good to me"))
assert(t, trigger == nil)

// Signature verification
body := []byte(`{"action":"created"}`)
secret := "test-secret"
mac := hmac.New(sha256.New, []byte(secret))
mac.Write(body)
sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

err := github.VerifySignature(body, sig, secret)
assertNoError(t, err)

// Wrong signature fails
err = github.VerifySignature(body, "sha256=deadbeef", secret)
assertError(t, err)

// Missing header fails
err = github.VerifySignature(body, "", secret)
assertError(t, err)

// Server: deduplication
srv := github.NewServer(mockRunner, "")
rec := httptest.NewRecorder()

// First request triggers job
req := buildWebhookRequest("@drover-code review this")
srv.Handler().ServeHTTP(rec, req)
assert(t, rec.Code == http.StatusAccepted)

// Second request for same PR deduplication
rec2 := httptest.NewRecorder()
srv.Handler().ServeHTTP(rec2, req)  // same PR
// Job is deduplicated — runner was only called once
assert(t, mockRunner.CallCount == 1)

// Server: concurrent jobs for different PRs
// Both should proceed concurrently
req1 := buildWebhookRequestForPR(1, "@drover-code review")
req2 := buildWebhookRequestForPR(2, "@drover-code review")
go srv.Handler().ServeHTTP(httptest.NewRecorder(), req1)
go srv.Handler().ServeHTTP(httptest.NewRecorder(), req2)
time.Sleep(100 * time.Millisecond)
assert(t, mockRunner.CallCount == 2)
```

---

## 3. Comparing the Two Integration Modes

| Aspect | IDE Bridge | GitHub Webhook |
|---|---|---|
| Transport | stdio (JSON-RPC) | HTTP (webhooks) |
| Initiator | IDE extension | GitHub comment |
| Session scope | Interactive, multi-turn | Single request-response |
| Repo access | User's local checkout | Ephemeral clone |
| Permissions | User's permission prompts | AllowAll (no interactive user) |
| Output | Streamed to IDE | Posted as GitHub comment |
| Authentication | API key (user's) | API key + GitHub token |
| Undercover mode | Optional | Always active |
| Cleanup | Stateless | Delete clone dir |

Both modes use the same agent loop and tool system. The only differences are
how input arrives, how output is delivered, and how the repository is accessed.

---

## 4. Edge Cases and Known Issues

### Bridge: streaming not wired

As noted in §1.5, the bridge currently returns complete responses rather than
streaming tokens. The VS Code extension may show a spinner until the full
response arrives. Wiring up `drover/textDelta` notifications requires
forwarding `TextDeltaEvent`s from the event channel to the bridge's `Notify`
method during the agent run.

### Bridge: reconnection handling

If the IDE extension disconnects and reconnects (e.g. VS Code reloads the
window), the bridge's read loop receives `io.EOF` and returns. The bridge
process exits. A more robust implementation would restart the bridge's
read loop or keep the agent loop alive and reconnect the bridge.

### Webhook: no rate limiting per user

The deduplication prevents two jobs for the same PR, but there is no per-user
rate limiting. A user could mention `@drover-code` on hundreds of different issues
in rapid succession, spawning hundreds of agent jobs (bounded by the semaphore
to 5 concurrent, but queuing arbitrarily many). A token bucket per GitHub user
would address this.

### Webhook: clone failure on private repos

If the GitHub token doesn't have access to the repository (expired token,
wrong scopes, repository access revoked), the `git clone` command fails.
The runner captures this as an error and updates the placeholder comment with
the error message. The error message from git may include the clone URL with
the embedded token — the error message should be sanitised before posting to
the public comment.

```go
// In run(), sanitise error messages before posting to GitHub
if err != nil {
    safeErr := sanitiseGitError(err.Error(), r.ghClient.token)
    response = fmt.Sprintf("❌ Error: %s", safeErr)
}

func sanitiseGitError(msg, token string) string {
    if token != "" {
        return strings.ReplaceAll(msg, token, "***")
    }
    return msg
}
```

### Webhook: large repositories slow to clone

Even with `--depth=1`, cloning a repository with hundreds of MB of large
binary files (assets, packages) is slow. The `--filter=blob:none` option
(partial clone) would fetch only the tree structure initially and blobs
on demand:

```
git clone --depth=1 --filter=blob:none --branch=<ref> <url> <dir>
```

This significantly reduces clone time for asset-heavy repositories while
allowing the agent to read files as needed.

---

## 5. Future Considerations

### Bridge: streaming tokens to IDE

Wire `TextDeltaEvent`s from the agent event channel to `b.Notify("drover/textDelta", ...)`.
Required for the VS Code extension to show streaming output.

### Bridge: tool confirmation in IDE

Currently, tools that need permission block the agent goroutine waiting for
the TUI prompt — which doesn't exist in bridge mode. Bridge mode uses `AllowAll`.
A complete implementation would route `PermissionRequestEvent` as a JSON-RPC
notification to the extension, which shows a modal to the user, and sends the
decision back via a `drover/permissionResponse` method.

### Webhook: GitHub App support

Implement installation token generation from a GitHub App private key. This
is the production-grade authentication approach and required for distributing
the webhook server as a public service.

### Webhook: PR review workflow

A complete code review workflow would:
1. Fetch the full PR diff
2. Run a tool-using agent session that reads changed files and the diff
3. Generate structured review feedback (file, line, comment triples)
4. Post inline comments via `CreateReviewWithComments`

The current implementation posts a single issue comment. The inline comment
path (`PostReviewComment`, `CreateReviewWithComments`) is implemented in the
client but not yet triggered by the runner.

### Webhook: issue assignment

If a GitHub issue is assigned to the bot's GitHub App/user, trigger an agent
session automatically (no `@drover-code` mention needed). The `PullRequestEvent`
with `action: "assigned"` or `IssueEvent` with `action: "assigned"` would
trigger the runner with the issue body as the request.

---

*Previous: [`09-advanced-systems.md`](./09-advanced-systems.md)*  
*End of design documentation series.*

---

## Document Index

| Doc | Sections | Lines |
|---|---|---|
| [01-foundation.md](./01-foundation.md) | Types, API client, SSE streaming | ~673 |
| [02-agent-loop.md](./02-agent-loop.md) | Convo manager, agent loop, events | ~848 |
| [03-tools-overview.md](./03-tools-overview.md) | Tool interface, registry, toolutil | ~824 |
| [04-fs-tools.md](./04-fs-tools.md) | read_file, write_file, edit_file, ls | ~920 |
| [05-shell-search-tools.md](./05-shell-search-tools.md) | bash, glob, grep | ~995 |
| [06-git-web-tools.md](./06-git-web-tools.md) | Git tools, web_fetch | ~1055 |
| [07-tui.md](./07-tui.md) | BubbleTea terminal UI | ~411 |
| [08-config-permissions-undercover.md](./08-config-permissions-undercover.md) | Config, permissions, undercover | ~822 |
| [09-advanced-systems.md](./09-advanced-systems.md) | Dream memory, coordinator | ~923 |
| [10-integrations.md](./10-integrations.md) | IDE bridge, GitHub webhook | ~923 |