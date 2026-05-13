# Available Tools

This document lists all the tools implemented in drover-code and their purposes.

## File System Tools

| Tool | Description |
|------|-------------|
| `read_file` | Read the contents of a file, optionally sliced to a line range, with safety checks for binary files. |
| `write_file` | Write content to a file, creating it if it does not exist or completely replacing it if it does. |
| `edit_file` | Make targeted string replacements in a file with fuzzy whitespace normalisation and unified diff output. |
| `list_directory` | List the files and directories in a given directory with metadata (size, type, modification time). |
| `file_info` | Return metadata for a file or directory including size, permissions, and modification time. |

## Shell Tools

| Tool | Description |
|------|-------------|
| `bash` | Execute a bash command and return stdout, stderr, and exit code with configurable timeout. |

## Search Tools

| Tool | Description |
|------|-------------|
| `glob` | Find files matching a glob pattern with support for `**` recursive matching (up to 1000 results). |
| `grep` | Search for a regex pattern in files with context lines, using ripgrep for speed or pure Go fallback. |

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

## Unikraft Cloud Tools (Optional)

These tools require `UKC_TOKEN` environment variable to be set.

| Tool | Description |
|------|-------------|
| `ukc_create` | Create a Unikraft Cloud instance running the drover HTTP agent with configurable image and memory. |
| `ukc_exec` | Run a shell command on a Unikraft Cloud instance via the in-VM agent with SSE streaming. |
| `ukc_delete` | Delete a Unikraft Cloud instance by ID and remove it from the local registry. |
| `ukc_delete_all` | Delete all Unikraft Cloud instances recorded in the local registry and clear the file. |
| `ukc_list` | List Unikraft Cloud instances stored in the local registry with metadata (id, name, url, age). |

## Tool Categories

### Read-Only Tools (No Permission Required)
- `read_file`, `list_directory`, `file_info`
- `glob`, `grep`
- `bash` (note: executes commands, but no permission flag)
- `git_status`, `git_diff`, `git_log`
- `web_fetch`
- `ukc_list`

### Write/Modify Tools (Permission Required)
- `write_file`, `edit_file`
- `git_add`, `git_commit`, `git_push`, `git_create_branch`
- `ukc_create`, `ukc_exec`, `ukc_delete`, `ukc_delete_all`

### System Tools (Permission Required)
- `bash`
