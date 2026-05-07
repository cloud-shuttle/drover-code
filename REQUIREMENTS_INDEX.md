# Drover-Code Requirements & Integration Guide — Master Index

## Overview

This index guides you through four comprehensive documents that completely specify all required modules, APIs, and integration points for the drover-code project.

**Total Documentation:** ~2,500 lines across 4 documents  
**Status:** ✅ Implementation complete (79 Go files)  
**Test Coverage:** Unit, integration, fuzz, and property tests included

---

## 📋 Document Guide

### 1. **REQUIREMENTS_SUMMARY.txt** (Quick Start — 490 lines)

**Purpose:** One-page executive summary and quick reference  
**Read this first for:** High-level overview, key decisions, environment variables  
**Best for:** Onboarding, quick lookups, status at a glance

**Sections:**
- Executive summary (what's built, what's partial)
- Core modules at-a-glance (18 tools, 10+ subsystems)
- Key architectural decisions (8 major patterns)
- Critical integration points (10 junctions)
- Data flow from user input to response
- Quick environment variable reference
- Test coverage snapshot

**Time to read:** 15 minutes

---

### 2. **IMPLEMENTATION_REQUIREMENTS.md** (Complete Reference — 880 lines)

**Purpose:** Exhaustive module-by-module breakdown  
**Read this for:** Implementation status, dependencies, contracts, testing strategy  
**Best for:** Understanding what exists, implementation specs, contract validation

**Sections:**
- Detailed module descriptions (15+ packages)
  - Status indicators (✅ implemented, ⚠️  partial, ❌ missing)
  - Responsibilities and key exports
  - Dependencies and integrations
  - Example code where relevant

**Modules covered:**
- Foundation: `internal/api` (types, client, stream)
- Tools: registry, utilities, 8 tool categories (18 tools total)
- Agent: loop, events, errors, conversation manager
- Configuration: loader, runtime apply, markdown injection
- Permissions: engine, presets
- Dream memory: worker, JSON/SQLite stores, retention
- Coordinator: task decomposition, parallelism
- Bridge: JSON-RPC/LSP for IDEs
- GitHub: webhook, parser, runner, client
- TUI: Bubble Tea model, views, styling
- Telemetry & undercover utilities

**Also includes:**
- Integration points matrix
- Critical implementation details
- Missing/incomplete components
- Build targets
- Environment variables
- Testing strategy (unit, fuzz, property, integration, evals)

**Time to read:** 30 minutes

---

### 3. **ARCHITECTURE_OVERVIEW.md** (Visual Guide — 585 lines)

**Purpose:** Visual representation of how everything fits together  
**Read this for:** Data flows, system interactions, architectural patterns  
**Best for:** Understanding system design, debugging, extension planning

**Sections:**

**Visual Diagrams:**
- Module dependency graph (tree structure with all relationships)
- Data flow: user input → API → stream → response (9-step pipeline)
- Tool execution lifecycle (permission checks, execution, result injection)
- Coordinator mode: task parallelism (decompose → workers → merge)
- Dream memory: consolidation cycle (trigger → summarise → store → inject)
- Bridge: IDE request-response loop (stdin → framing → dispatch → response)
- GitHub webhook: event to agent (webhook → parser → clone → run → post)
- UKC agent: remote execution (upload → execute → download)

**Conceptual Flows:**
- Configuration cascade (home → project → local → env)
- Permissions model: 3-layer checks (preset → allowance → input-specific)
- Testing strategy: layers from unit to live eval tests

**Time to read:** 25 minutes

---

### 4. **INTEGRATION_CHECKLIST.md** (Developer Guide — 565 lines)

**Purpose:** Practical, step-by-step guide for extending the system  
**Read this for:** How to add features, implementation patterns, best practices  
**Best for:** Adding tools, extending features, maintaining quality

**Step-by-Step Guides:**
1. **Adding a new tool** (6 steps with code example)
2. **Modifying the agent loop** (invariants, testing pattern)
3. **Adding configuration options** (cascade, env vars)
4. **Extending the TUI** (state machine, Bubble Tea patterns)
5. **Adding permission presets** (definition, testing)
6. **Implementing new operation modes** (detection, event handling)
7. **Dream/memory extensions** (store interface, backends)
8. **GitHub integration points** (parser, runner, new actions)
9. **Bridge RPC methods** (handler registration, framing)
10. **Coordinator extensions** (decomposition strategies, merging)

**Reference Tables:**
- Testing integration points (unit test template, integration template)
- Configuration troubleshooting
- Environment variable checklist
- Build and test checklist
- Performance considerations
- Release checklist

**Time to read:** 20 minutes (or refer to relevant sections as needed)

---

## 🎯 How to Use These Documents

### For Onboarding
1. Start: **REQUIREMENTS_SUMMARY.txt** (15 min)
2. Then: **ARCHITECTURE_OVERVIEW.md** (25 min)
3. Deep dive: Relevant sections of **IMPLEMENTATION_REQUIREMENTS.md** (10-30 min)

### For Adding a New Tool
1. Read: **INTEGRATION_CHECKLIST.md** → "Adding a New Tool" section
2. Reference: **REQUIREMENTS_SUMMARY.txt** → 18 Production Tools section
3. Example: Look at existing tool in `internal/tools/git/` or `internal/tools/fs/`

### For Understanding Agent Loop
1. Read: **ARCHITECTURE_OVERVIEW.md** → "Data Flow" section
2. Detailed: **IMPLEMENTATION_REQUIREMENTS.md** → Agent Loop section
3. Verify: `internal/agent/loop.go` in actual code

### For System Architecture
1. Diagram: **ARCHITECTURE_OVERVIEW.md** → Module Dependency Graph
2. Details: **IMPLEMENTATION_REQUIREMENTS.md** → Integration Points Matrix
3. Implementation: Check actual code files

### For Extending Functionality
1. Pattern: **INTEGRATION_CHECKLIST.md** → relevant feature section
2. Reference: Related sections in **IMPLEMENTATION_REQUIREMENTS.md**
3. Context: **ARCHITECTURE_OVERVIEW.md** → relevant data flow
4. Code: Examine similar implementation in codebase

### For Troubleshooting
1. Environment: **REQUIREMENTS_SUMMARY.txt** → Environment Variables section
2. Issues: **INTEGRATION_CHECKLIST.md** → Configuration Troubleshooting
3. Details: **IMPLEMENTATION_REQUIREMENTS.md** → specific module section

---

## 🏗️ Document Cross-References

### REQUIREMENTS_SUMMARY.txt Links to:
- 18 Production Tools → Detailed specs in IMPLEMENTATION_REQUIREMENTS.md
- Core Modules → Full module specs in IMPLEMENTATION_REQUIREMENTS.md
- Data Flow → Detailed diagrams in ARCHITECTURE_OVERVIEW.md
- Critical Points → Implementation checklist items in INTEGRATION_CHECKLIST.md

### IMPLEMENTATION_REQUIREMENTS.md Links to:
- Testing Strategy → Test types explained in architecture doc
- Integration Points Matrix → Visual representations in ARCHITECTURE_OVERVIEW.md
- Module responsibilities → Data flows in ARCHITECTURE_OVERVIEW.md
- Implementation details → Step-by-step guides in INTEGRATION_CHECKLIST.md

### ARCHITECTURE_OVERVIEW.md Links to:
- Visual diagrams → Textual specs in IMPLEMENTATION_REQUIREMENTS.md
- Data flows → Implementation in specific modules
- Testing layers → Test types defined in INTEGRATION_CHECKLIST.md
- Integration patterns → Step-by-step guides in INTEGRATION_CHECKLIST.md

### INTEGRATION_CHECKLIST.md Links to:
- Step-by-step guides → Architecture patterns in ARCHITECTURE_OVERVIEW.md
- Module changes → Specs in IMPLEMENTATION_REQUIREMENTS.md
- Code examples → Existing implementations in `internal/`
- Build steps → Build targets in REQUIREMENTS_SUMMARY.txt

---

## 📊 Key Numbers at a Glance

| Metric | Count |
|--------|-------|
| **Go Source Files** | 79 (+ 49 test files) |
| **Production Tools** | 18 |
| **Internal Packages** | 13 |
| **API Endpoints** (modes) | 5 (TUI, headless, bridge, webhook, coordinator) |
| **Permission Presets** | 4 |
| **Message Types** | 6+ (Text, ToolCall, ToolResult, Usage, Error, Complete) |
| **Total Implementation Lines** | ~2,500 in 4 docs |
| **Code + Tests** | ~14,000 lines of Go |

---

## ✅ Implementation Status Summary

### ✅ COMPLETE (79 files)
- Foundation layer (API client, SSE stream, types)
- 18 production tools
- Agent loop with streaming
- Conversation manager
- TUI with Bubble Tea
- Headless mode
- GitHub webhook integration
- IDE bridge (JSON-RPC)
- Dream memory (JSON + SQLite)
- Coordinator mode
- Permissions engine
- Configuration system
- Comprehensive tests

### ⚠️ PARTIAL / FUTURE
- UKC agent HTTP endpoints (needs workspace sync completion)
- Langfuse telemetry (integration scaffolding in place)
- IDE extensions (protocol defined, clients to implement)
- Batch API support (designed, not implemented)
- Real tokenizer (currently heuristic-based)

---

## 🔍 Key Design Patterns

All documents explain these critical decisions:

1. **Raw HTTP Client** — vs official SDK for streaming control
2. **Discriminated Unions** — unexported marker methods for type safety
3. **Snapshot Copies** — thread-safe conversation manager
4. **Iterator Pattern** — streaming with backpressure
5. **Non-Blocking Memory** — dream consolidation won't block agent
6. **Per-Index Accumulators** — handle interleaved stream blocks
7. **Tool-Specific Parsing** — deferred JSON unmarshalling
8. **Three-Layer Permissions** — preset → tool → input-specific

---

## 📚 Document Purposes

| Document | Primary Purpose | Read Time | Best For |
|----------|---|---|---|
| REQUIREMENTS_SUMMARY.txt | Quick reference & overview | 15 min | Onboarding, quick lookups |
| IMPLEMENTATION_REQUIREMENTS.md | Complete specification | 30 min | Implementation details, contracts |
| ARCHITECTURE_OVERVIEW.md | Visual system design | 25 min | Understanding interactions |
| INTEGRATION_CHECKLIST.md | Developer guide & patterns | 20 min | Adding features, maintaining |

**Total reading time for complete understanding:** ~90 minutes  
**Total reading time for quick start:** ~15 minutes (summary only)

---

## 🚀 Getting Started

### Path 1: Quick Start (15 minutes)
1. Read **REQUIREMENTS_SUMMARY.txt**
2. Review **ARCHITECTURE_OVERVIEW.md** data flow diagrams
3. You're ready to explore the code

### Path 2: Deep Dive (90 minutes)
1. Read **REQUIREMENTS_SUMMARY.txt** (15 min)
2. Study **IMPLEMENTATION_REQUIREMENTS.md** (30 min)
3. Review **ARCHITECTURE_OVERVIEW.md** (25 min)
4. Browse **INTEGRATION_CHECKLIST.md** (20 min)
5. Explore code with understanding

### Path 3: Implementation-Focused (40 minutes)
1. Read **REQUIREMENTS_SUMMARY.txt** (15 min)
2. Jump to **INTEGRATION_CHECKLIST.md** relevant section (15 min)
3. Reference **ARCHITECTURE_OVERVIEW.md** as needed (10 min)
4. Start implementing

---

## 📍 Document Locations

All documents are in the repository root:

```
/workspace/
├── REQUIREMENTS_SUMMARY.txt          (executive summary)
├── IMPLEMENTATION_REQUIREMENTS.md    (detailed specs)
├── ARCHITECTURE_OVERVIEW.md          (visual guide)
├── INTEGRATION_CHECKLIST.md          (developer guide)
├── REQUIREMENTS_INDEX.md             (this file)
├── internal/                         (implementation)
│   ├── api/
│   ├── agent/
│   ├── tools/
│   ├── config/
│   ├── convo/
│   ├── permissions/
│   ├── dream/
│   ├── coordinator/
│   ├── bridge/
│   ├── github/
│   ├── tui/
│   ├── telemetry/
│   └── undercover/
├── cmd/
│   ├── drover-code/
│   └── ukc-agent/
├── design/                          (original specs)
└── tests/                           (comprehensive)
```

---

## 💡 Tips for Using These Documents

### As a Developer
- **Bookmark INTEGRATION_CHECKLIST.md** for quick access when adding features
- **Keep ARCHITECTURE_OVERVIEW.md open** while reading code to understand flow
- **Reference IMPLEMENTATION_REQUIREMENTS.md** for API contracts

### As a Reviewer
- **Check against REQUIREMENTS_SUMMARY.txt** for consistency with design
- **Verify integration points** using ARCHITECTURE_OVERVIEW.md
- **Ensure tests match** INTEGRATION_CHECKLIST.md patterns

### As a Maintainer
- **Use IMPLEMENTATION_REQUIREMENTS.md** to track implementation status
- **Consult INTEGRATION_CHECKLIST.md** before approving new features
- **Keep these docs in sync** with actual code changes

### As an Evaluator
- **Start with REQUIREMENTS_SUMMARY.txt** for high-level view
- **Check status indicators** in IMPLEMENTATION_REQUIREMENTS.md
- **Review test coverage** in testing strategy sections

---

## 🔄 Maintaining These Documents

These documents should be updated when:
1. A new tool is added (update all 4 docs)
2. A new module is created (update specs + checklist)
3. An integration point changes (update architecture diagram)
4. Implementation status changes (update requirements doc)
5. New design patterns emerge (update overview + checklist)

**Sync checklist:**
- [ ] Update status indicators in IMPLEMENTATION_REQUIREMENTS.md
- [ ] Update module count in REQUIREMENTS_SUMMARY.txt
- [ ] Update diagrams in ARCHITECTURE_OVERVIEW.md if topology changes
- [ ] Add new patterns to INTEGRATION_CHECKLIST.md
- [ ] Run tests: `CGO_ENABLED=0 go test ./...`
- [ ] Verify all files build: `CGO_ENABLED=0 go build ./cmd/...`

---

## ❓ FAQ

**Q: Where do I start if I want to add a new tool?**  
A: Go to INTEGRATION_CHECKLIST.md → "Adding a New Tool" section. It's step-by-step with code examples.

**Q: How do I understand how everything connects?**  
A: Read ARCHITECTURE_OVERVIEW.md, especially the module dependency graph and data flow sections.

**Q: What's the status of feature X?**  
A: Check REQUIREMENTS_SUMMARY.txt first (quick), then IMPLEMENTATION_REQUIREMENTS.md (detailed).

**Q: What tests should I write for my change?**  
A: See INTEGRATION_CHECKLIST.md → "Testing Integration Points" for templates and patterns.

**Q: Where's the TUI implementation?**  
A: See IMPLEMENTATION_REQUIREMENTS.md → section 11, then look in `internal/tui/`.

**Q: How do I add a new permission preset?**  
A: INTEGRATION_CHECKLIST.md → "Adding a Permission Preset" section.

**Q: What environment variables are available?**  
A: REQUIREMENTS_SUMMARY.txt → "Environment Variables Quick Reference" section.

---

## 📖 Document Statistics

```
Document                      Lines  Words  Size
────────────────────────────────────────────────
REQUIREMENTS_SUMMARY.txt       489   3,200  18.7K
IMPLEMENTATION_REQUIREMENTS    878   5,800  24.9K
ARCHITECTURE_OVERVIEW          585   4,100  20.5K
INTEGRATION_CHECKLIST          565   3,900  13.4K
────────────────────────────────────────────────
TOTAL                        2,517  17,000  77.5K
```

---

## 🎯 Next Steps

1. **If onboarding:** Start with REQUIREMENTS_SUMMARY.txt
2. **If extending:** Go to INTEGRATION_CHECKLIST.md
3. **If learning:** Read ARCHITECTURE_OVERVIEW.md
4. **If implementing:** Use IMPLEMENTATION_REQUIREMENTS.md
5. **If maintaining:** Check all 4 periodically

---

*Last updated: 2025*  
*Part of drover-code project: github.com/cloudshuttle/drover-code*  
*These documents describe the complete system architecture and integration requirements.*
