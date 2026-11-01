# Buddy

Single-session Rex terminal orchestrator and agent skill for managing as many concurrent project sessions and feature tabs as you want without cognitive overload or context drift.

---

## The Vision

Instead of manually navigating dozens of terminal tabs across projects to see which agent is blocked or waiting for input, you talk to **one** terminal agent session: **Buddy**.

- **Dispatch work**: *"Hey Buddy, add inline comment shortcuts to Docnotate"* -> Buddy finds or creates the `docnotate` Rex session, creates a dedicated tab for the feature, launches `agy` (or `pi`/`opencode` if requested), starts planning, and relays questions back to you.
- **Check progress**: *"Hey, how is the Docnotate task going?"* -> Buddy checks the pane in O(1) via its local state cache (`~/.buddy/state.json`), summarizes progress or clarifying questions, and relays your answers back to the worker.
- **Review / Demo**: *"Show me a demo"* -> Buddy splits a dev server pane beside the worker and focuses the tab in Rex.

---

## Architecture & Memory Layers

Buddy solves the 3 core multi-project orchestration challenges:

```
+-------------------------------------------------------------+
|                      User talks to Buddy                    |
+-------------------------------------------------------------+
                               |
                               v
               +-------------------------------+
               |   Buddy Control Tower         |
               |   Rex Session: 'Buddy'        |
               |   CWD: ~/.buddy/              |
               +-------------------------------+
                 | Layer 1: AGENTS.md (Always-on system prompt)
                 | Layer 2: .agents/hooks.json (PreInvocation ephemeral state)
                 | Layer 3: state.json (O(1) cached IDs: project -> session -> task)
                               |
                               v
               +-------------------------------+
               | buddy-ctl (Python 3 CLI tool) |
               +-------------------------------+
                               |
                               v
                     rex CLI & Rex Server
                    /         |         \
                   /          |          \
        Rex Session         Rex Session   ...
        'docnotate'         'daily-catch'
        - Tab 1: feat-auth   - Tab 1: fix-sync
        - Tab 2: feat-ui
```

1. **Always-On System Prompt Identity**: `~/.buddy/AGENTS.md` is injected on every turn and survives `/compact`. Because it lives in `~/.buddy/`, worker agents in project repositories never inherit it.
2. **Ephemeral Turn Injections (Zero History Bloat)**: `~/.buddy/.agents/hooks.json` runs `buddy-ctl context-inject` on `PreInvocation`, emitting a transient `ephemeralMessage` with active project statuses that is discarded immediately after the turn.
3. **O(1) Local State Cache**: `~/.buddy/state.json` maps projects and tasks to exact Rex session and block IDs, eliminating slow scans across Rex sessions.

---

## Hybrid Execution Model

```
                  ┌─────────────────────────────────────────────────────────┐
                  │                   User Interaction                      │
                  └──────────────────────────┬──────────────────────────────┘
                                             │
                                   ┌─────────▼────────┐
                                   │ Buddy Control    │
                                   │ Tower (Main)     │
                                   └────┬────────┬────┘
           Fast Interactive Operations  │        │  Heavy / Background Workloads
                     (<50ms O(1) Cache) │        │  (Isolated Subagents)
                                        │        │
      ┌─────────────────────────────────┘        └───────────────────────────────┐
      ▼                                                                          ▼
┌─────────────────────────────────┐                    ┌──────────────────────────────────────────────────┐
│ Main Thread CLI:                │                    │ Subagent Tasks (Zero Main Token Bloat):          │
│ • buddy-ctl status              │                    │ • Watchdog: buddy-ctl watch (sleeps in Python,   │
│ • buddy-ctl send                │                    │   wakes Buddy via send_message)                  │
│ • buddy-ctl focus / demo        │                    │ • Diagnostician: buddy-ctl diagnose              │
│ • buddy-ctl capture             │                    │ • Batch Dispatch: buddy-ctl batch                │
└──────────────┬──────────────────┘                    └────────────────────────┬─────────────────────────┘
               │                                                                │
               └───────────────────────────┬────────────────────────────────────┘
                                           ▼
                         ┌───────────────────────────────────┐
                         │      Rex Terminal Worker Panes    │
                         │ (docnotate, daily-catch, etc.)    │
                         └───────────────────────────────────┘
```

| Workflow | Execution Mode | Tools Used | Why |
| :--- | :--- | :--- | :--- |
| **Status, Quick Capture, Q&A Relay, Demo** | **Main Thread** | `buddy-ctl status`, `capture`, `send`, `demo` | O(1) cache lookup (<50ms). Instant interactive replies. |
| **Background Watchdog** | **Subagent** | `invoke_subagent` (`Role: 'Buddy Watchdog'`) + `buddy-ctl watch` | Waits asynchronously for questions/completion; alerts main Buddy without blocking user chat. |
| **Deep Pane Diagnosis** | **Subagent** | `invoke_subagent` (`Role: 'Buddy Diagnostician'`) + `buddy-ctl diagnose` | Analyzes large scrollback/stack traces in isolation without cluttering main transcript. |
| **Batch Multi-Repo Dispatch** | **Subagents / Batch CLI** | `buddy-ctl batch` or parallel `invoke_subagent` | Dispatches tasks across 3+ repos concurrently. |

---

## Installation

Clone and run the installer:

```sh
cd /Users/sunday/Dev/Personal/Projects/buddy
./install.sh
```

This:
1. Links `bin/buddy` and `buddy-ctl` -> `~/.local/bin/`
2. Links `skills/buddy` into:
   - `~/.agents/skills/buddy`
   - `~/.gemini/config/skills/buddy`
   - `~/.gemini/antigravity-cli/skills/buddy`
   - `~/.pi/agent/skills/buddy`
   - `~/.config/opencode/skills/buddy`
3. Initializes `~/.buddy/` (`config.json`, `state.json`, `AGENTS.md`, `.agents/hooks.json`).

---

## Quickstart

### Launch Buddy
```sh
buddy
```
Switches to or creates the `Buddy` Rex session and launches your orchestrator agent (`agy`).

### CLI Usage (`buddy-ctl`)

```sh
# View tracked projects and tasks from cache (O(1))
buddy-ctl status

# Reconcile cache against live Rex sessions
buddy-ctl status --sync

# List available projects under ~/Dev/Personal/Projects
buddy-ctl projects

# Fast-path atomic dispatch (auto-slug, spawn, prompt, verify in 1 shot)
buddy-ctl dispatch --project docnotate --prompt "Add shortcut to resolve comments" [--task inline-comments]

# Start a new task tab with worker agent (agy by default)
buddy-ctl task start --project docnotate --task inline-comments --plan-first --prompt "Add shortcut to resolve comments"

# Start with a specific worker agent (pi or opencode)
buddy-ctl task start --project daily-catch --task sync-engine --agent pi --prompt "Fix sync timeout"

# Inspect worker terminal output (O(1))
buddy-ctl capture --project docnotate --task inline-comments

# Relay user input or answer questions to worker agent
buddy-ctl send --project docnotate --task inline-comments --text "Use Cmd+Shift+E"

# Focus task in Rex and launch dev server split
buddy-ctl demo --project docnotate --task inline-comments --cmd "npm run dev"

# Watchdog: Wait asynchronously until worker asks a question or finishes
buddy-ctl watch --project docnotate --task inline-comments --interval 5 --timeout 600

# Diagnostician: Extract error snippets and failure analysis from worker pane
buddy-ctl diagnose --project docnotate --task inline-comments --lines 300

# Batch dispatch tasks across multiple projects
buddy-ctl batch --json '[{"project":"docnotate","task":"feat-a","prompt":"..."},{"project":"daily-catch","task":"feat-b","prompt":"..."}]'

# Clean up task tab when finished
buddy-ctl task close --project docnotate --task inline-comments
```

---

## Planning & Implementation Docs

- [Implementation Handoff & Context Notes](plans/buddy-handoff.md)
- [Full Architecture Plan](plans/buddy-terminal-orchestrator.md)
