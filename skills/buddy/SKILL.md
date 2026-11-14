---
name: buddy
description: Central multi-project Rex terminal orchestrator. Use this skill whenever the user asks Buddy to manage, dispatch, coordinate, or inspect tasks across multiple project workspaces, spawn worker agents in dedicated project tabs (agy by default, or pi / opencode on request), adopt or reconcile out-of-band user tabs, relay questions or answers to worker panes, or present finished feature demos in Rex.
---

# Buddy — Operational Manual & CLI Reference

This skill is the operational reference for **Buddy** (`~/.buddy/`). For Buddy's core identity, 5 hard invariants, and 4-step turn loop, see `~/.buddy/AGENTS.md` (`templates/orchestrator-AGENTS.md`).

> [!IMPORTANT]
> **Planning Instructions Belong in `--prompt`**: Buddy does **not** use a `--plan-first` flag. Whenever a task requires architectural planning or a plan review before coding, include those planning instructions directly inside `--prompt` (e.g., `--prompt "First write an implementation plan in plans/feat-x.md and wait for my review before coding..."`).

---

## Playbook 1: Pre-Dispatch Triage & 3-Tier Confidence Threshold

Before calling `buddy-ctl dispatch`, compare the user's request against open tracked tasks and untracked tabs in the target project (shown in `[Buddy Live State]` or `buddy-ctl status`):

| Confidence Tier | Criteria | Required Action |
| :--- | :--- | :--- |
| **Tier 1: High Confidence (`>= 90%`)** | Same project, same feature/bug, clearly the same intention (e.g. answering a worker's question, requesting a follow-up tweak on the exact feature). | **Reuse Tab**: Run `buddy-ctl send -p <proj> -t <slug> --text "<message>"` (if untracked, run `buddy-ctl task adopt -p <proj> -w <window>` first). |
| **Tier 2: Uncertain / Related (`30% – 89%`)** | Same project and overlapping domain tokens, or an open task is `waiting_for_input` / `ready_for_demo` on a related area, but you are not 100% certain whether the user wants to continue that tab or start fresh. | **STOP & Ask User**: List the candidate `slug`, `status`, and 1-line `summary`. Ask: *"Task `<proj>/<slug>` (`<status>`: `<summary>`) is already open. Should I continue in that tab or start a new tab?"* |
| **Tier 3: Distinct (`< 30%`)** | Clearly a different feature, bug, or subsystem. | **Dispatch New Tab**: Run `buddy-ctl dispatch -p <proj> -t <slug> -m "<prompt>"`. |

### Handling the CLI Candidate Duplicate Guard
`buddy-ctl dispatch` and `buddy-ctl task start` enforce this rule automatically. If a live task or untracked window in the same project matches the requested `--task` slug or shares domain tokens, the CLI exits with an error listing the candidate(s):
- **Do NOT blindly re-run with `--force`.**
- If the match is Tier 1 (unambiguous same task), use `buddy-ctl send`.
- If the match is Tier 2 (uncertain), present the candidate list to the user and ask.
- Only pass `--force` (`buddy-ctl dispatch -p <proj> -t <slug> -m "<prompt>" --force`) after the user explicitly confirms they want a separate tab.

---

## Playbook 2: Out-of-Band User Work, Deep Reconcile & Tab Adoption

Users frequently jump directly into project Rex tabs to code, answer prompts, or open manual agent windows, then return to Buddy.

### 1. Deep Reconcile (`buddy-ctl status --sync`)
- `buddy-ctl status` and `buddy-ctl context-inject` automatically trigger a deep reconcile when `~/.buddy/state/rex.json` is older than 45 seconds (`last_synced_at` TTL).
- To force an immediate live probe at any time, run:
  ```sh
  buddy-ctl status --sync
  ```
- **What Deep Reconcile Does**:
  1. Verifies Rex sessions and windows exist.
  2. Samples each live tracked task block (via Rex native OSC 7501 `program_status` and `Capture(30)` screen heuristics) and updates `status`, `last_summary`, and `last_verified_at`.
  3. Discovers unknown windows inside tracked project sessions (ignoring plain idle shells like `zsh`/`bash`) and records them under `untracked_windows`.

### 2. Contradiction Handling (Live Pane vs. Cached State)
When a reconcile or `buddy-ctl capture` reveals that the real pane contradicts the old cache:
- **Trust the live pane.** Never repeat a stale question if the pane is now `running` or `ready_for_demo`.
- Report the reconciled reality to the user (e.g., *"I synced with the live pane—it looks like you already answered the question in `docnotate/auth-flow` and all tests are now passing (`ready_for_demo`)."*).

### 3. Adopting Untracked Tabs (`buddy-ctl task adopt`)
When `status` or `[Buddy Live State]` surfaces an untracked live tab (`Untracked live tabs: <proj>/<label> [window=<id>, <status>: '<summary>']`) and the user wants Buddy to manage or message it:
```sh
buddy-ctl task adopt --project <proj> --window <window-id-or-label> [--task <slug>] [--agent agy]
```
- Promotes the window into `~/.buddy/state/rex.json` with `origin: "user"`, live `status`, `last_summary`, and `last_verified_at`, and removes it from `untracked_windows`.
- Once adopted, interact with it normally via `buddy-ctl send`, `capture`, `watch`, or `demo`.

---

## Playbook 3: Situational Monitoring & Watchdog Scope Selection

Never hard-code `watch --all` when only a single task or single project is active, and never forget to arm a watchdog after `dispatch`, `batch`, or `send`.

### 1. Selecting the Right Watch Scope
Every `dispatch`, `send`, and `batch` JSON response includes a computed `recommended_watch` field with the exact smallest scope covering all `running`/`planning` tasks:
- **Single active task**:
  ```sh
  buddy-ctl watch --project <P> --task <T> --interval 5 --timeout 600
  ```
- **Multiple active tasks in one project**:
  ```sh
  buddy-ctl watch --project <P> --interval 5 --timeout 600
  ```
- **Active tasks across 2+ projects**:
  ```sh
  buddy-ctl watch --all --interval 5 --timeout 600
  ```

### 2. Blocking Watchdog Subagent vs. Quick Main-Thread Check
- **Background Watchdog (Subagent)** — use right after `dispatch`, `batch`, or `send` when waiting asynchronously for workers to finish or ask a question:
  ```python
  invoke_subagent(
      TypeName="self",
      Role="Buddy Watchdog: <P>/<T> (or <P> / all)",
      Prompt=(
          "Monitor active worker task(s). "
          "Run '<recommended_watch_command>'. "
          "When it returns with an event (waiting_for_input, ready_for_demo, or error), "
          "message the parent Buddy agent via send_message with a concise summary of the event and what the worker needs."
      )
  )
  ```
- **Quick Status / Capture (Main Thread)** — use when the user asks *"how is X going right now?"*:
  - Run `buddy-ctl status` or `buddy-ctl capture -p <P> -t <T> --lines 80` directly on the main thread and reply immediately.

---

## Playbook 4: Dispatch Pickup Verification, Q&A Relay, Diagnosis, Demos & Cleanup

### 1. Dispatch & Pickup Verification (`picked_up` & `pickup_reason`)
```sh
buddy-ctl dispatch --project <proj> --prompt "<prompt>" [--task <slug>] [--agent agy|pi|opencode] [--force]
```
- `buddy-ctl dispatch` automatically dismisses known agent onboarding modals (`esc to dismiss` / `Auto mode: edits & safe commands auto-approved`) and verifies whether the prompt was actually received by the agent (`picked_up: true` only when the prompt text is echoed or the agent transitions to active working state).
- **If `picked_up: true`**: Report the dispatch in 1 concise line and arm the Watchdog Subagent using `recommended_watch`.
- **If `picked_up: false`**: Do **not** tell the user the worker is running! Inspect `pickup_reason` and `preview`:
  1. Run `buddy-ctl capture -p <proj> -t <slug> --lines 40` to see what blocked the prompt.
  2. Dismiss the modal/banner if needed (`buddy-ctl send -p <proj> -t <slug> --key esc` or `--key enter`).
  3. Re-send the prompt (`buddy-ctl send -p <proj> -t <slug> --text "<prompt>"`).

### 2. Relaying Questions & Answers (`send`)
When a worker is `waiting_for_input`:
1. Present the worker's question clearly to the user.
2. When the user answers, relay it immediately:
   ```sh
   buddy-ctl send --project <proj> --task <slug> --text "<user's answer>"
   ```
3. `buddy-ctl send` automatically transitions a `waiting_for_input` or `ready_for_demo` task back to `running` and returns `recommended_watch`. Re-arm the Watchdog Subagent with that `recommended_watch` command.

### 3. Deep Failure Diagnosis (`diagnose` via Subagent)
When a worker hits compiler errors, panics, or failing tests:
```python
invoke_subagent(
    TypeName="self",
    Role=f"Buddy Diagnostician: {project}/{task}",
    Prompt=(
        f"Diagnose the failure in project '{project}', task '{task}'. "
        f"Run 'buddy-ctl diagnose --project {project} --task {task} --lines 300' to inspect error snippets and recent commands. "
        "Message the parent Buddy agent with a concise root-cause explanation and recommended fix."
    )
)
```

### 4. Presenting a Demo (`demo`) & Cleanup (`task close`)
- **Focus tab & open dev server split**:
  ```sh
  buddy-ctl demo --project <proj> --task <slug> [--cmd "npm run dev"] [--focus-only]
  ```
- **Close completed tab**:
  ```sh
  buddy-ctl task close --project <proj> --task <slug>
  ```

---

## Complete `buddy-ctl` CLI Reference & JSON Schemas

| Command | Flags & Purpose |
| :--- | :--- |
| `buddy-ctl dispatch` | `-p <proj> -m "<prompt>" [-t <slug>] [-a <agent>] [--engine <eng>] [--force]` — Atomic spawn, readiness probe, modal dismissal, prompt delivery, and pickup verification. |
| `buddy-ctl task adopt` | `-p <proj> -w <window-id-or-label> [-t <slug>] [-a <agent>] [--engine <eng>]` — Adopt an untracked user-opened window into Buddy state (`origin: "user"`). |
| `buddy-ctl task start` | `-p <proj> -t <slug> [-m "<prompt>"] [-a <agent>] [--engine <eng>] [--force]` — Start a task tab with explicit slug. |
| `buddy-ctl task update` | `-p <proj> -t <slug> --status <status> [--summary "<text>"]` — Manually update cached status/summary. |
| `buddy-ctl task close` | `-p <proj> -t <slug>` — Close task window in multiplexer and mark closed. |
| `buddy-ctl status` | `[--sync] [--no-sync] [--format json\|table\|brief] [--engine <eng>]` — View tracked projects, tasks, and `untracked_windows` (auto-syncs if Rex cache >45s old unless `--no-sync`). |
| `buddy-ctl watch` | `[--project <P> --task <T>] \| [--project <P>] \| [--all] [--interval 5] [--timeout 600] [--debounce 2] [--notify]` — Block until a watched task hits `waiting_for_input`, `ready_for_demo`, or `error`. |
| `buddy-ctl send` | `-p <proj> -t <slug> (--text "<msg>" \| --key <key>)` — Send text (+ Enter) or special key (`enter`, `esc`, `ctrl+c`), transitioning task back to `running`. |
| `buddy-ctl capture` | `-p <proj> -t <slug> [--lines 120]` — Capture recent terminal output lines from worker block. |
| `buddy-ctl diagnose` | `-p <proj> -t <slug> [--lines 300]` — Extract structured error blocks, stack traces, and recent commands. |
| `buddy-ctl demo` | `-p <proj> -t <slug> [--cmd "<dev-cmd>"] [--focus-only]` — Focus task window and optionally open a dev split pane. |
| `buddy-ctl focus` | `-p <proj> [-t <slug>] [-w <win>]` — Focus project/task window and bring terminal app to front. |
| `buddy-ctl batch` | `--json '[...]' \| --file <path>` — Dispatch multiple tasks across projects in one call. |
| `buddy-ctl projects` | `[--query <q>] \| roots \| add-root <path> \| remove-root <path>` — Discover repositories and manage project roots. |
| `buddy-ctl config` | `show \| set <key> <val>` — Inspect or update `~/.buddy/config.json`. |
| `buddy-ctl notify` | `-m "<msg>" [--title "..."] [--subtitle "..."] [--sound Glass]` — Trigger macOS desktop alert. |

### Key JSON Response Payloads

#### `buddy-ctl dispatch`
```json
{
  "status": "dispatched",
  "project": "docnotate",
  "task": "inline-comments",
  "window_id": "window:01...",
  "agent_block_id": "surface:01...",
  "agent": "agy",
  "picked_up": true,
  "pickup_reason": "",
  "preview": "> Add inline comment shortcut\n⠋ Thinking...",
  "active_tasks_in_project": [
    {
      "slug": "inline-comments",
      "status": "running",
      "origin": "buddy",
      "summary": "Add inline comment shortcut"
    }
  ],
  "recommended_watch": "buddy-ctl watch --project docnotate --task inline-comments --interval 5 --timeout 600"
}
```

#### `buddy-ctl task adopt`
```json
{
  "status": "adopted",
  "project": "docnotate",
  "task": "manual-refactor",
  "window_id": "window:01...",
  "agent_block_id": "surface:01...",
  "origin": "user",
  "task_status": "ready_for_demo",
  "summary": "Completed manual refactor of router.",
  "recommended_watch": ""
}
```

### Valid Task Statuses
- `starting`: Tab created, agent CLI booting.
- `planning`: Worker agent is reading codebase and drafting a plan.
- `waiting_for_input`: Worker agent asked a question or needs confirmation.
- `running`: Worker agent is actively coding, refactoring, or running tests.
- `ready_for_demo`: Implementation finished; idle at prompt with meaningful output.
- `error`: Worker encountered a panic, fatal error, or crash.
- `done` / `closed`: Task accepted and tab closed.
- `missing`: Tab or session was closed outside Buddy.

---

## Engine & Agent Configuration

Buddy stores per-engine settings under `engines.<name>` in `~/.buddy/config.json` and isolates runtime state per engine under `~/.buddy/state/<engine>.json`:

1. **Terminal Engines (`rex`, `wezterm`)**:
   - `orchestrator_agent` (default: `"agy"`): Runs the central Buddy control session (`bin/buddy`, which runs `buddy-ctl status --sync` at startup).
   - `worker_agent` (default: `"agy"`): Default CLI harness for spawned task tabs (`buddy-ctl dispatch`, `buddy-ctl task start`).
   - `session_name` (default: `"Buddy"`), `capture_lines` (default: `120`).
2. **T3 Engine (`t3`)**:
   - `provider` and `model`: Required for `t3 thread start` (e.g. `buddy-ctl config set engines.t3.provider antigravity` and `buddy-ctl config set engines.t3.model gemini-3.8-flash-medium`).

### Pinning a Model Variant
To use a model-pinned variant in terminal engines:
1. Add an entry to `agents` in `~/.buddy/config.json` with the model args in its `command` array (e.g. `"agy-flash": {"command": ["agy", "--model", "gemini-3.8-flash-medium"]}`).
2. Select it per-command (`buddy-ctl dispatch --agent agy-flash ...`) or set it as default (`buddy-ctl config set engines.rex.worker_agent agy-flash`).
