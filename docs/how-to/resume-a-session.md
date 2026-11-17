---
title: Resume a session
description: Continue a local drover-code conversation after the process has quit.
product: drover-code
audience:
  - platform-operator
doc_type: how-to
topics:
  - agent-jobs
surface: repo-docs
---

# Resume a session

A local drover-code conversation is written to disk after each completed message. Quitting the process does not end the conversation. The next launch in the same directory can load it.

## Continue the latest session

From the project directory:

```bash
drover-code -c
```

`--continue` is the same flag. The TUI shows the previous turns, then waits for the next message. The system prompt is built again from the current `CLAUDE.md`, settings, and tools.

A headless follow-up uses the same transcript:

```bash
drover-code -c -p "Apply the review comments"
```

## Pick an older session

```bash
drover-code --resume
```

In the TUI that opens the recent sessions for this directory. Move with the arrow keys, type to filter, and press Enter to continue one. Esc quits. From a script, the same flag prints the list and exits. Pass a full id or a unique prefix to skip the picker:

```bash
drover-code --resume session-18f3
drover-code -r session-18f3
```

## Where the files live

Each working directory has its own set of files under `~/.drover/sessions/`. A session started in one checkout is not offered from another checkout.

`/clear` and `/reset` keep the file you just left and start a new id for the next messages.

`/pause` and `/resume` still only apply while that process is open. They stop and continue the live turn. `-c` is what you use after you quit.

The local TUI and a local headless run save sessions. A unikernel worker, the IDE bridge, coordinator mode, and `drover-code --cloud` leave this store alone.
