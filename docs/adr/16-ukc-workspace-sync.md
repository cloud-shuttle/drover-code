# 16 — UKC Remote Workspace Synchronization

**Entrypoint:** `cmd/drover-code` (Coordinator Mode)  
**Agent:** `cmd/ukc-agent` (Unikraft Instance)  
**Status:** Proposed

---

## Architecture Overview

This document proposes a **Stateless Workspace Synchronization** architecture for executing `drover-code` tasks on ephemeral Unikraft Cloud instances. 

Instead of relying on remote Git push workflows or persistent cloud volumes, this architecture allows a local coordinator (running on a developer's laptop) to synchronize their local filesystem with an ephemeral cloud worker, execute the heavy AI generation in the cloud, and seamlessly merge the resulting code back into the developer's local IDE.

### The End-to-End Flow

1. **The Upload**: The local coordinator synchronizes the local Git repository to the freshly booted Unikraft instance (e.g., via an initial compressed payload or `rsync`).
2. **The Execution (`POST /exec`)**: The `ukc-agent` prepares the isolated `/workspace` directory, sets it as the working directory, and launches `drover-code --headless`.
3. **The SSE Stream (`GET /exec/:id/stream`)**: The coordinator streams real-time logs and AI generation progress back to the local terminal.
4. **The Download**: Upon successful exit (Code 0), the coordinator synchronizes the modified `/workspace` back from the instance, leveraging `rsync` to compute a diff and only pull changed files, greatly reducing network traffic.
5. **The Merge**: The coordinator safely unpacks the modified files into the local repository.
6. **The Cleanup**: The Unikraft instance is destroyed.

---

## Deep Dive: Potential Issues & Mitigations

While conceptually simple, network-bound workspace synchronization introduces several critical edge cases that must be mitigated to prevent data loss and ensure a premium developer experience.

### 1. Large Workspace Uploads & Efficient Syncing
**The Issue:**
Modern repositories often exceed several gigabytes due to `node_modules`, `venv`, `.git/objects`, or large build binaries. Attempting to compress and upload a 2GB workspace on every run will consume immense local CPU and saturate outbound bandwidth.

**The Mitigation:**
While Unikraft Cloud instances can comfortably be given gigabytes of memory to handle large operations without OOMing, we still want to minimize network traffic. 

1. **Incremental Syncing (`rsync`)**: For cases requiring multiple syncs between the sandbox and the host, we should use `rsync` over SSH (or equivalent diffing logic). `rsync` computes a diff of the workspace directory and only syncs the changed files, drastically reducing network overhead compared to full `.tar.gz` payloads.
2. **Exclusion Rules**: The coordinator **must** respect `.gitignore` and `.dockerignore` rules when syncing. 
    - Ignore all build directories (`target/`, `dist/`, `node_modules/`).
    - Exclude the heavy `.git/objects` database unless git history is explicitly required.

### 2. The Concurrency Problem (Parallel Workers Wiping Each Other)
**The Issue:**
The coordinator currently supports spawning parallel workers (e.g. `[worker 1]`, `[worker 2]`, `[worker 3]`). If all three workers finish their subtasks and attempt to unpack their modified files directly over the local directory, the last worker to finish will blindly overwrite the changes made by the others!

**The Mitigation:**
The coordinator must leverage **Git Branches**. 
When the coordinator downloads a modified workspace from `[worker 1]`, it does *not* extract it directly into the working tree. Instead:
1. It unzips the payload into a temporary `.drover/tmp/worker-1` directory.
2. It creates a new local git branch: `git checkout -b drover/task-1`.
3. It syncs the files, commits the changes automatically (`git commit -m "AI Gen: Task 1"`), and then returns to the main branch.
The user is left with three independent, cleanly committed branches that they can review and merge manually, completely eliminating race conditions.

### 3. Local State Overwrites (The Dirty Working Tree)
**The Issue:**
If a cloud worker takes 3 minutes to finish a task, the developer might continue typing and editing files locally. If the coordinator downloads the finished workspace and unpacks it into the working tree, it will destroy the developer's uncommitted work.

**The Mitigation:**
Never overwrite uncommitted local changes. Before applying a downloaded workspace, the coordinator must check `git status --porcelain`. 
- If the working tree is dirty, the coordinator aborts the direct merge, stashes the downloaded changes in a temporary branch, and alerts the user: *"Worker finished, but you have uncommitted changes. Run `git merge drover/task-1` to review."*

### 4. Security & Secret Leakage
**The Issue:**
Uploading the entire local directory means highly sensitive files (like `.env`, `aws_credentials`, or local config files) are transmitted to the cloud instance. If the instance egresses data or is compromised, secrets are exposed.

**The Mitigation:**
The coordinator must implement a strict **Secret Filter**. By default, it aggressively strips out `.env`, `*.pem`, `*.key`, and any files ignored by `.gitignore`. The coordinator should display a preview of the files to be uploaded before transmission: *"Uploading 42 files (1.2MB) to cloud worker. Press Enter to confirm."*

### 5. Missing Toolchains in the Cloud Worker
**The Issue:**
The AI agent (`drover-code`) often needs to run language-specific tools (e.g., `npm run test`, `cargo build`, `pytest`) to validate its code. The base `ukc-agent` Alpine image only has `bash` and `git` installed. If the uploaded workspace is a Node.js project, the agent will fail because `npm` does not exist.

**The Mitigation:**
The user must be able to define the runtime environment for the worker. 
The coordinator should look for a `drover-worker.Dockerfile` or a `.devcontainer` in the root of the local workspace. If found, it instructs Unikraft Cloud to build the instance using that image instead of the default `ukc-agent` image, ensuring all necessary language toolchains are present before the code is uploaded.

### 6. Streaming SSE Connection Drops
**The Issue:**
The `GET /exec/:id/stream` Server-Sent Events connection may drop due to network instability or load balancer idle timeouts, causing the coordinator to falsely assume the task failed.

**The Mitigation:**
The coordinator must implement automatic reconnection logic. It should send the last received `Event-ID` when reconnecting, and the `ukc-agent` must buffer the last 100 events in memory to seamlessly replay any missed telemetry.
