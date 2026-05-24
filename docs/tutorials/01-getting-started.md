---
title: Getting started with Drover Code
description: Install the static Go binary, configure API access, and run your first agentic coding session.
product: drover-code
audience:
  - evaluator
  - platform-operator
doc_type: tutorial
topics:
  - agent-jobs
surface: repo-docs
---

# Getting Started with Drover Code

Welcome to Drover Code! This tutorial will guide you through setting up and running your first autonomous agentic coding assistant using the Anthropic API. 

By the end of this tutorial, you will have:
1. Built the `drover-code` binary from source.
2. Configured your environment with an Anthropic API key.
3. Started the interactive Terminal User Interface (TUI).
4. Run your first task with the agent.

## Prerequisites

Before you begin, ensure you have the following installed on your system:
- **Go 1.22+**: Required to build the binary.
- **Git**: Required for the agent to use git tools.
- **Anthropic API Key**: You'll need a valid API key (`sk-ant-...`) to communicate with the Claude models.

## Step 1: Build the Binary

Drover Code is a single, static Go binary. Open your terminal, navigate to the root of the `drover-code` repository, and run the following command to build it:

```bash
CGO_ENABLED=0 go build -o drover-code ./cmd/drover-code
```

This will produce a `drover-code` executable in your current directory.

## Step 2: Configure Your Environment

The agent requires an API key to communicate with the model. You can set this via the `ANTHROPIC_API_KEY` environment variable.

```bash
export ANTHROPIC_API_KEY="your-api-key-here"
```

> [!TIP]
> If you are using an Anthropic-compatible provider (like Moonshot or GLM), refer to our [Custom LLM Providers guide](../how-to/configure-custom-providers.md) for setup instructions.

## Step 3: Launch the TUI

Now you are ready to start the interactive Terminal User Interface (TUI). Simply run the binary you built in Step 1:

```bash
./drover-code
```

You should see the Drover Code TUI initialize, presenting you with a prompt where you can type your requests.

## Step 4: Run Your First Task

Let's test the agent by asking it to summarize the README file. In the TUI prompt, type:

```text
Please summarize the README.md file in this directory.
```

Press `Enter` or `Ctrl+J` to submit your request. 

The agent will read the `README.md` using its filesystem tools, stream its thought process, and present a concise summary on your screen.

## Next Steps

Congratulations! You've successfully built and interacted with Drover Code. 

- To learn how to integrate this into automated workflows, see our [Integration Checklist](../how-to/integration-checklist.md).
- To understand how the agent loop works under the hood, read the [Architecture Overview](../explanation/architecture-overview.md).
- To see what tools the agent has available, check out the [Available Tools](../reference/available-tools.md) reference.
