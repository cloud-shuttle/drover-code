---
title: Available tools
description: Reference of built-in agent tools exposed to Drover Code during a run.
product: drover-code
audience:
  - platform-operator
  - agent
doc_type: reference
topics:
  - agent-jobs
surface: repo-docs
---

# Available Tools

This document lists all the tools implemented in drover-code and their purposes.

## File System Tools

| Tool | Description |
|------|-------------|
| `read_file` | Read the contents of a file, optionally sliced to a line range, with safety checks for binary files. |
| `write_file` | Write content to a file, creating it if it does not exist or completely replacing it if it does. A local session appends gopls errors and warnings when the file is Go. |
| `edit_file` | Make targeted string replacements in a file with fuzzy whitespace normalisation and unified diff output. A local session appends gopls errors and warnings when the file is Go. |
| `list_directory` | List the files and directories in a given directory with metadata (size, type, modification time). |
| `file_info` | Return metadata for a file or directory including size, permissions, and modification time. |

## Shell Tools

| Tool | Description |
|------|-------------|
| `bash` | Execute a bash command and return stdout, stderr, and exit code with configurable timeout. `background: true` returns a job id immediately in a local session; the output arrives as a later message. A local session on macOS or Linux runs the command in an OS sandbox. |

A local macOS or Linux session runs `bash` in an OS sandbox. The command can read files. It can write only inside the working tree and a private temp directory. Network is blocked. The unikernel preset, coordinator, IDE bridge, and `--cloud` do not set this sandbox.

## Search Tools

| Tool | Description |
|------|-------------|
| `glob` | Find files matching a glob pattern with support for `**` recursive matching (up to 1000 results). |
| `grep` | Search for a regex pattern in files with context lines, using ripgrep for speed or pure Go fallback. |

## Explore

| Tool | Description |
|------|-------------|
| `explore` | Search the working tree in a separate read-only pass and return a short file map. The search stays out of the conversation. Local sessions only. It cannot edit files or run commands. |
| `delegate` | Do one coding task in a separate loop and return a short result. The child can read, edit, and run bash. Its tool calls stay out of the conversation. It cannot ask the user or start another delegate. Local sessions only. |
| `browser` | Open a page in a local headless Chrome. Actions are open, snapshot, click, type, press, screenshot, and close. Pages are http or https. Local sessions only. |

## Git Tools

| Tool | Description |
|------|-------------|
| `git_status` | Show the working tree status including modified, staged, untracked, and conflicted files. |
| `git_diff` | Show changes in the working tree or staged area as unified diff, with support for comparing against commits. |
| `git_log` | Show recent commit history with optional one-line format and filtering by file or directory. |
| `git_add` | Stage files for the next commit, with support for staging all changes via empty paths list. |
| `git_commit` | Create a commit from currently staged changes with optional support for empty commits. |
| `git_push` | Push commits to a remote repository with optional force-with-lease support. |
| `git_create_branch` | Create a new git branch with optional checkout and support for creating from a specific ref. |

## Web Tools

| Tool | Description |
|------|-------------|
| `web_fetch` | Fetch the content of a URL and return it as plain text, with automatic HTML-to-text conversion. |
| `web_search` | Search the web and return up to eight titles, URLs, and snippets. Local sessions only. |
| `read_skill` | Load one `SKILL.md` body by name from the workspace or `~/.drover/skills`. Local sessions only. |
| `todo` | Replace the session checklist shown above the input. Local sessions only. |
| `ask_user` | Ask one question and wait for the typed answer. Local TUI only. |

## Unikraft Cloud Tools (Optional)

These tools require `UKC_TOKEN` environment variable to be set.

| Tool | Description |
|------|-------------|
| `ukc_create` | Create a Unikraft Cloud instance running the drover HTTP agent with configurable image and memory. |
| `ukc_exec` | Run a shell command on a Unikraft Cloud instance via the in-VM agent with SSE streaming. |
| `ukc_delete` | Delete a Unikraft Cloud instance by ID and remove it from the local registry. |
| `ukc_delete_all` | Delete all Unikraft Cloud instances recorded in the local registry and clear the file. |
| `ukc_list` | List Unikraft Cloud instances stored in the local registry with metadata (id, name, url, age). |

## Plan mode

Shift+Tab or `/planmode` turns plan mode on for this process. Tools that ask for permission are refused, including `write_file`, `edit_file`, `bash`, and git commands that change the repository. `read_file`, `list_directory`, `file_info`, `glob`, `grep`, `explore`, `write_plan`, and `ask_user` still run. Shift+Tab again restores the previous permission mode. `permissionMode: plan` in settings starts the TUI in plan mode. Headless runs stay on their own permission preset.

## Tool Categories

### Read-Only Tools (No Permission Required)
- `read_file`, `list_directory`, `file_info`
- `glob`, `grep`, `explore`
- `bash` (note: executes commands, but no permission flag)
- `git_status`, `git_diff`, `git_log`
- `web_fetch`, `web_search`, `read_skill`, `todo`, `ask_user`
- `ukc_list`

### Write/Modify Tools (Permission Required)
- `write_file`, `edit_file`
- `git_add`, `git_commit`, `git_push`, `git_create_branch`
- `ukc_create`, `ukc_exec`, `ukc_delete`, `ukc_delete_all`

### System Tools (Permission Required)
- `bash`
