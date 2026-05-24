# 15. UKC Instance Tool

## Overview

A new sub-package `internal/tools/ukc/` adds five tools that give the model full Unikraft Cloud instance lifecycle management. A companion Go binary (`cmd/ukc-agent/`) runs inside each instance and accepts commands over HTTP.

---

### Part 1 — In-Instance Agent (`cmd/ukc-agent/`)

A minimal static Go binary (`CGO_ENABLED=0`) baked into an OCI image that drover-code launches on Unikraft Cloud.

**Endpoints:**

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/health` | Returns `200 OK` when ready (used by `ukc_create` to poll before returning) |
| `POST` | `/exec` | Spawns command, returns `{"job_id":"..."}` immediately |
| `GET` | `/exec/{job_id}/stream` | SSE log stream + done event |

**SSE event shape:**
```json
data: {"stream":"stdout","line":"hello world"}
data: {"stream":"stderr","line":"warning: ..."}
data: {"done":true,"exit_code":0}
```

**Auth:** every request must carry `Authorization: Bearer <token>`. The token is injected at instance creation time as an env var (`AGENT_TOKEN`). drover-code generates a random 32-byte hex token per instance and passes it both to the UKC API (as an env var) and stores it in the local instance registry.

---

### Part 2 — Instance Registry (persistence)

A JSON file at `~/.drover-code/ukc-instances.json` tracks all instances created in any session. Schema:

```json
{
  "abc123": {
    "id": "abc123",
    "name": "my-worker",
    "url": "https://abc123.kraft.cloud",
    "token": "deadbeef...",
    "created_at": "2026-04-21T10:00:00Z"
  }
}
```

The registry is loaded at tool construction time. On any create/delete operation it is atomically written back to disk (write to temp file, rename). This means if drover-code restarts, all previously created instances are still tracked and can be deleted.

---

### Part 3 — Tools in `internal/tools/ukc/`

Five tools share a single `*Manager` struct (holds the registry, the UKC API credentials, and a mutex):

#### `ukc_create`
- Input: `name` (string), `image` (string, default: drover's published agent image), `memory_mb` (int, optional)
- Creates a Unikraft Cloud instance via the raw Unikraft Cloud REST API directly (thin HTTP wrapper) to avoid a heavy dependency.
- Passes `AGENT_TOKEN=<random>` as an env var to the instance
- Polls `GET /health` (with backoff, up to 60s) until ready
- Saves entry to registry
- Returns instance ID + URL to the model

#### `ukc_exec`
- Input: `instance_id` (string), `command` (string), `timeout_seconds` (int)
- Looks up URL + token from registry
- `POST /exec` → gets `job_id`
- Opens `GET /exec/{job_id}/stream` SSE connection with `context.WithTimeout`
- Reads all events, buffers lines; terminates on `done` event or context cancellation
- Returns buffered log + exit code as a single string to the model (Option A)

#### `ukc_delete`
- Input: `instance_id` (string)
- Calls Unikraft Cloud API to terminate the instance
- Removes from registry
- Returns confirmation

#### `ukc_delete_all`
- No input
- Iterates registry, calls delete on each instance (concurrently with `errgroup`)
- Clears registry entirely
- Returns a summary of what was deleted / any errors
- **This is the cleanup escape hatch** — the model can call it at end of task, and drover-code could also call it as a shutdown hook

#### `ukc_list`
- No input
- Returns all instances from the registry (ID, name, URL, age)
- Lets the model recover context if it has forgotten which instance IDs it created

---

### Part 4 — Who Manages Instance IDs?

The **model is responsible** for remembering instance IDs within a conversation — `ukc_create` returns an ID and the model stores it in its context window like any other tool output. This is intentional and consistent with how tools like `bash` work.

However, the model can always recover via `ukc_list`. The persistent registry also means a human operator can run `ukc_delete_all` after a crashed session to avoid orphaned instances.

The model should be prompted (via the tool description for `ukc_create`) to always call `ukc_delete` when done with an instance, and to prefer `ukc_delete_all` at task end for safety.

---

### Part 5 — File Layout

```
cmd/ukc-agent/          ← in-instance HTTP agent binary
  main.go
  exec.go               ← subprocess + SSE streaming

internal/tools/ukc/
  manager.go            ← *Manager, registry load/save, shared UKC API client
  registry.go           ← JSON persistence
  tool_create.go
  tool_exec.go
  tool_delete.go
  tool_delete_all.go
  tool_list.go
  ukc.go                ← RegisterAll(*Registry, cfg)
```

register.go gains a call to `ukc.RegisterAll(...)` when UKC credentials are present in config.

---

### Open Questions (Resolved)

1. **KraftCloud SDK vs raw REST**: Using the raw Unikraft Cloud REST API directly (thin HTTP wrapper) is preferred to avoid a heavy dependency.
2. **Image distribution**: The `cmd/ukc-agent` binary is bundled inside a standard Dockerfile which also includes `cmd/drover-code` to allow remote agents to execute `drover-code -headless`.
3. **Credentials config**: `UKC_TOKEN` (and `UKC_METRO`) are fetched directly from the environment inside `NewManagerFromEnv()`.
