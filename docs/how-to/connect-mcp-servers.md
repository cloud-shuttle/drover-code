---
title: Connect MCP servers
description: Give the local drover-code session stdio or streamable-HTTP MCP tools.
product: drover-code
audience:
  - platform-operator
doc_type: how-to
topics:
  - agent-jobs
surface: repo-docs
---

# Connect MCP servers

The local TUI loads MCP servers from the same settings files as the rest of drover-code: `~/.claude/settings.json`, then the project's `.claude/settings.json`, then `.claude/settings.local.json`. Put tokens in `settings.local.json`. That file stays untracked.

Each server is either a local command or a streamable HTTP URL. One entry uses one of those, not both. Tools show up as `mcp_{server}_{tool}`. The process calls the server with the tool's own name.

## Stdio

```json
{
  "mcpServers": {
    "docs": {
      "command": ["npx", "-y", "some-mcp"],
      "env": { "API_KEY": "..." }
    }
  }
}
```

## HTTP

Paper and other remote servers use `url` and `headers`.

```json
{
  "mcpServers": {
    "paper": {
      "url": "https://mcp.example/mcp",
      "headers": { "Authorization": "Bearer ..." }
    }
  }
}
```

Restart drover-code after editing settings. A server that fails to start is skipped. The reason is printed on stderr and as a system line in the TUI.

Hosted worker allowlists do not gain these servers unless the job settings name them. The Sydney Gateway path for Brain is unchanged.
