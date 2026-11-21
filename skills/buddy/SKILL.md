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
| **Tier 1: High Confidence (`>= 90%`)** | Same project, same feature/bug, clearly the same intention (e.g. answering a worker's question, requesting a follow-up tweak on the exact feature). | **Reuse Tab**: Run `buddy-ctl send -p <proj> -t <slug> --text "<message>"` (untracked tabs are Tier 2 unless the user explicitly named that tab, in which case run `buddy-ctl task adopt -p <proj> -w <window>` first). |
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

### 2. Watch Is Subagent-Only (Main Thread Only As Fallback)
- **Background Watchdog Subagent (Required)**: ALWAYS run `buddy-ctl watch` in a background subagent after `dispatch`, `batch`, or `send` — never on the main thread, and never as a shell background job (`&`, `nohup`, detached pane).
- **Only fallback**: if subagent tooling is unavailable or spawn fails, run `watch` on the main thread and state the reason in one line.
  ```python
  # T3 Code (delegate_task) — or Antigravity: invoke_subagent(TypeName="self", ...)
  delegate_task(
      role="research",
      title="Buddy Watchdog: <P>/<T>",
      task=(
          "Run '<recommended_watch_command>'. "
          "When it returns an event (waiting_for_input, ready_for_demo, or error), "
          "summarize the event and what the worker needs."
      ),
  )
  ```
- **Main-Thread Reads (Non-Watch Only)**: Run `buddy-ctl status`, `capture`, or `diagnose` directly on the main thread for immediate checks. **Never** use this path for `watch`.

---

## Playbook 4: Dispatch Pickup Verification, Q&A Relay, Diagnosis, Demos & Cleanup

### 1. Dispatch & Pickup Verification (`picked_up` & `pickup_reason`)
```sh
buddy-ctl dispatch --project <proj> --prompt "<prompt>" [--task <slug>] [--agent agy|pi|opencode] [--model <model>] [--engine rex|t3|opencode] [--force]
```
- Omitting `--model` (and `BUDDY_MODEL`) leaves worker commands untouched so each CLI/engine uses its own configured or last-used default model.
- Use `buddy-ctl models [--agent <name>] [--engine <eng>] [--query <q>]` to inspect available models before passing `--model`.
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

### 4. Presenting a Demo (`demo`) & Cleanup (`task close` -> `task clean`)
- **Focus tab & open dev server split**:
  ```sh
  buddy-ctl demo --project <proj> --task <slug> [--cmd "npm run dev"] [--focus-only]
  ```
- **Close and clean completed task** (never hand-edit `~/.buddy/state/<engine>.json`):
  ```sh
  buddy-ctl task close --project <proj> --task <slug>
  buddy-ctl task clean --project <proj> --task <slug> --reason "merged and verified"
  # Or in one step:
  buddy-ctl task clean --project <proj> --task <slug> --close --reason "merged and verified"
  ```
- **`--force` Consent Rule**: **NEVER** pass `--force` on your own initiative. `--force` is reserved for orphaned/stale entries (e.g., unreachable backend or already-gone tab/thread) and **requires explicit user agreement in the current conversation**. Always ask the user before passing `--force`.
- **Inspect past cleaned tasks**:
  ```sh
  buddy-ctl task history [--project <proj>] [--task <slug>] [--since 7d] [--limit 20] [--json]
  ```

---

## Playbook 5: Research & Dispatch Scout (`Buddy Scout`)

Delegate investigatory dispatches (requiring repo reads, `git` checks, or multi-tab disambiguation) to a background **`Buddy Scout`** subagent so the main thread stays responsive and free of codebase read noise.

> [!NOTE]
> **Verified Phase 1 CLI Surface**: `--prompt-file`, `--text-file`, `--plan-first`, `--worktree`, and `buddy-ctl triage` do **not** exist yet. Always pass briefs via `-m "$(cat '<brief_path>')"` on `dispatch` and `--text "$(cat '<brief_path>')"` on `send`.

### 1. Trigger, Concurrency & Invocation
- **Trigger**: See the Execution Routing Rule in `templates/orchestrator-AGENTS.md` (anything needing repo reading, `git` inspection, or multi-tab disambiguation before a brief).
- **Concurrency**: **Max 1 active Scout per project** (cross-project parallel allowed). If a second investigatory request arrives for the same project while a Scout is running, reply in 1 line (*"Already scouting `<project>`; I'll handle this right after it reports (or reply 'cancel' to redirect it)."*) and queue it. `buddy-ctl dispatch`/`task start` enforce `findCandidateCollisions` and `.state.lock` as a secondary net.
- **Subagent Capability & Prompt Template**: Spawn a **write + shell-capable** subagent using your harness's subagent tool with role `"Buddy Scout: <project>"` (e.g., `TypeName="self"` or `define_subagent` with `EnableWriteTools=true`, `EnableSubagentTools=false` in Antigravity; T3 `delegate_task` write/shell role is `UNVERIFIED`—never use a read-only `research` role):
  ```text
  You are Buddy Scout for project '<project>' (proposed slug: '<slug>').
  User request: <user_request>

  Contract (return ONLY the YAML report: 11 required + up to 3 optional lines: pickup_reason, candidates, open_questions):
  1. Run `buddy-ctl status --sync` (and `buddy-ctl projects` / `buddy-ctl projects roots` if project is unknown).
  2. Inspect repo read-only (`git status --short`, `git branch --show-current`, relevant files, target AGENTS.md). NEVER run `git checkout`/`stash`/`reset` or edit/write/delete any repo file.
  3. Apply 3-Tier triage (untracked_windows are always Tier 2 unless user explicitly named that tab). NEVER pass `--force` to `dispatch`, `task start`, or `task clean`.
  4. Write brief to `~/.buddy/briefs/<YYYYMMDD-HHMMSS>-<project>-<slug>.md` (even on Tier 2 needs_decision).
  5. Tier 1 (>=90% tracked): `buddy-ctl send -p "<project>" -t "<slug>" --text "$(cat '<brief_path>')"`.
     Tier 2 (30-89%, untracked, dirty conflict, or collision): STOP and return `status: needs_decision`.
     Tier 3 (<30%): `buddy-ctl dispatch -p "<project>" -t "<slug>" -m "$(cat '<brief_path>')" [--model <chosen>]` (pass `--model` only if the user explicitly requested one; otherwise omit to use the worker's default model). If `picked_up: false`, run modal recovery (max 2 attempts).
  6. NEVER run `buddy-ctl watch` or spawn a Watchdog; copy `recommended_watch` verbatim from CLI JSON.
  7. Return ONLY the structured YAML report (no file contents, diffs, or prose).
  ```
- Acknowledge in 1 line (`"Scouting <project>..."`) and **yield the turn** without pre-arming a Watchdog.

### 2. Scout Execution Rules (Read-Only Repo, 3-Tier Triage & Briefs)
- **Read-Only Repo**: Run `buddy-ctl status --sync`, `git status --short`, and `git branch --show-current`; read relevant files and target `AGENTS.md`/`CLAUDE.md`. Never mutate git state or repo files.
- **3-Tier Triage (No `--force`)**:
  - **Tier 1 (`>=90%`, unambiguous tracked task)**: Write brief, run `buddy-ctl send -p "<p>" -t "<slug>" --text "$(cat '<brief>')"`, return `status: reused` + verbatim `recommended_watch`.
  - **Tier 2 (`30-89%`, any `untracked_windows` unless user named that tab, dirty conflict, or CLI collision)**: Still write the brief, **stop without `--force` or `send`**, and return `status: needs_decision` with `candidates` (`{slug, kind, window_id, status, summary, why_uncertain}`).
  - **Tier 3 (`<30%`, distinct)**: Write brief and run `buddy-ctl dispatch -p "<p>" -t "<slug>" -m "$(cat '<brief>')"`.
- **Brief Path, Retention & Quality**: Save to `~/.buddy/briefs/<YYYYMMDD-HHMMSS>-<project>-<slug>.md` (the **only** authorized write path; retained for audit/Tier 2 reuse, may be pruned by age/count later; never include secrets/tokens). Every brief must specify: **(1)** 1–2 sentence objective + non-goals, **(2)** verified file paths/patterns, **(3)** target `AGENTS.md` constraints, **(4)** explicit planning instructions if plan-first is wanted (no `--plan-first`), **(5)** exact verification commands + concise summary/question reporting.

### 3. Pickup Verification & Modal Recovery
If `buddy-ctl dispatch` returns `"picked_up": false`, the Scout runs up to **2 recovery attempts**:
1. `buddy-ctl capture -p "<proj>" -t "<slug>" --lines 40` to inspect the blocker.
2. Dismiss modal (`buddy-ctl send -p "<proj>" -t "<slug>" --key esc` or `--key enter`), wait ~1s, verify idle prompt (`capture --lines 20`).
3. Re-send (`buddy-ctl send -p "<proj>" -t "<slug>" --text "$(cat '<brief_path>')"`) and verify echo/spinner (`capture --lines 30`).
- **Truthfulness Gate**: Report `status: dispatched` + `picked_up: true` **only** if `dispatch` returned `picked_up: true` or post-recovery `capture` proves pickup. Otherwise leave the tab intact and return `status: pickup_failed`, `picked_up: false`, `window_id`, and `pickup_reason`.

### 4. Compact Report Schema (11 Required + Up to 3 Optional Lines)
Return **only** this YAML block (zero file contents, diffs, or prose):
```yaml
status: dispatched | reused | needs_decision | pickup_failed | failed
project: <project-name>
task: <task-slug>
window_id: <window_id or none>
picked_up: true | false | n/a
pickup_reason: "<only on pickup_failed: exact CLI pickup_reason or capture summary>"
tier: "<Tier 1 (>=90%) | Tier 2 (30-89%) | Tier 3 (<30%)>"
tier_evidence: "<1-line summary of open/untracked tabs checked>"
inspected: ["<path1>", "<path2>", "<path3>"]
brief_path: "<~/.buddy/briefs/YYYYMMDD-HHMMSS-proj-slug.md>"
recommended_watch: "<exact command from CLI JSON, or none>"
candidates: "<only on needs_decision: slug [kind, status]: summary — why_uncertain>"
open_questions: "<none, or 1-line blocker/dirty-state note>"
next_action: "<1-line instruction for parent/user>"
```

### 5. Failure Matrix & Parent Wakeup

| Failure Mode | Scout Action | Parent Buddy Action |
| :--- | :--- | :--- |
| **1. `dispatch` fails** | Never pass `--force`. Save `brief_path`; return `status: failed` + CLI error. | Report error in 1–2 lines, keep `brief_path` for retry; arm nothing. |
| **2. `picked_up: false` after recovery** | Leave tab open; return `status: pickup_failed`, `picked_up: false`, `window_id`, `pickup_reason`. | Report honestly (no pickup, not running); arm nothing; ask to retry `send`, `focus`, or `task close`. |
| **3. Immediate `waiting_for_input`** | Note question in `open_questions` if captured. | Surface question immediately, or newly armed Watchdog catches it within 5s. |
| **4. Dirty repo / wrong branch** | Read-only `git` check only. Warn worker if disjoint, else `needs_decision`. | Surface `git` state from `open_questions`; ask to proceed, switch branch, or isolate. |
| **5. Unknown project** | Check `buddy-ctl projects` and `projects roots`; never guess. Return `failed`. | Report missing project + close matches; ask for path or `projects add-root <path>`. |
| **6. User cancels mid-Scout** | N/A (runs in background). | Cancel Scout via harness subagent manager (e.g. `manage_subagents` kill in `agy`, if available), or `send`/`task close` if already dispatched. |

- **On `dispatched` / `reused`**: Reply in 1–3 lines (no raw YAML) and **arm `Buddy Watchdog` in the same turn** using verbatim `recommended_watch` (Playbook 3).
- **On `needs_decision`**: Ask the user with the candidate list and execute their choice inline using `brief_path` (`send` — after `task adopt -p "<proj>" -w "<win>"` if untracked — or `dispatch ... --force`).
- **Lost-Report Recovery**: Per `templates/orchestrator-AGENTS.md`, if interrupted before the Scout reports, run `buddy-ctl status` at the start of the next turn and arm a Watchdog for any active task that appeared.

---

## Complete `buddy-ctl` CLI Reference & JSON Schemas

| Command | Flags & Purpose |
| :--- | :--- |
| `buddy-ctl dispatch` | `-p <proj> -m "<prompt>" [-t <slug>] [-a <agent>] [-M/--model <model>] [--engine <eng>] [--force]` — Atomic spawn, readiness probe, modal dismissal, prompt delivery, and pickup verification. |
| `buddy-ctl task adopt` | `-p <proj> -w <window-id-or-label> [-t <slug>] [-a <agent>] [--engine <eng>]` — Adopt an untracked user-opened window into Buddy state (`origin: "user"`). |
| `buddy-ctl task start` | `-p <proj> -t <slug> [--prompt "<prompt>"] [-a <agent>] [-M/--model <model>] [--engine <eng>] [--force]` — Start a task tab with explicit slug. |
| `buddy-ctl models` | `[-a/--agent <name>] [--engine <eng>] [-q/--query <q>] [-f/--format json\|table\|brief]` — List available models for the target agent or engine. |
| `buddy-ctl task update` | `-p <proj> -t <slug> --status <status> [--summary "<text>"]` — Manually update cached status/summary. |
| `buddy-ctl task close` | `-p <proj> -t <slug>` — Close task window in multiplexer and mark closed. |
| `buddy-ctl task clean` | `-p <proj> -t <slug> [--reason "..."] [--close] [--force] [--all-closed] [--prune-empty-project] [--dry-run]` — Remove closed task from active state and append audit record to `~/.buddy/history/tasks.jsonl` (`--force` requires explicit user consent). |
| `buddy-ctl task history` | `[-p <proj>] [-t <slug>] [--engine <eng>] [--since 7d] [--limit 20] [--json]` — Query append-only task cleanup history, newest first. |
| `buddy-ctl status` | `[--sync] [--no-sync] [--format json\|table\|brief] [--engine <eng>]` — View tracked projects, tasks, and `untracked_windows` (auto-syncs if Rex cache >45s old unless `--no-sync`). |
| `buddy-ctl watch` | `[--project <P> --task <T>] \| [--project <P>] \| [--all] [--interval 5] [--timeout 600] [--debounce 2] [--notify]` — Block until a watched task hits `waiting_for_input`, `ready_for_demo`, or `error`. |
| `buddy-ctl send` | `-p <proj> -t <slug> (--text "<msg>" \| --key <key>)` — Send text (+ Enter) or special key (`enter`, `esc`, `ctrl+c`), transitioning task back to `running`. |
| `buddy-ctl capture` | `-p <proj> -t <slug> [--lines 120]` — Capture recent terminal output lines from worker block. |
| `buddy-ctl diagnose` | `-p <proj> -t <slug> [--lines 300]` — Extract structured error blocks, stack traces, and recent commands. |
| `buddy-ctl demo` | `-p <proj> -t <slug> [--cmd "<dev-cmd>"] [--focus-only]` — Focus task window and optionally open a dev split pane. |
| `buddy-ctl focus` | `-p <proj> [-t <slug>] [-w <win>]` — Focus project/task window and bring terminal app to front. |
| `buddy-ctl batch` | `--json '[...]' \| --file <path>` — Dispatch multiple tasks across projects in one call (each item supports `project`, `task`, `prompt`, optional `agent`, `model`, `engine`). |
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

### Custom Model Selection (`--model` / `-M`)
- Pass `--model <id>` (or `-M <id>`) on `buddy-ctl dispatch` or `buddy-ctl task start` (or `"model"` in `buddy-ctl batch` items) across `rex`, `t3`, and `opencode`. Omitting `--model` preserves each CLI/engine's default model.
- Terminal agents configure injection via `agents.<name>.model_args` (default `["--model", "{{model}}"]`) or `agents.<name>.model_command` (e.g. `opencode` defaults to `["opencode", "mini", "--model", "{{model}}"]`), and discovery via `agents.<name>.models_cmd` and `models`.
- To pin a model variant permanently as a named harness, add an entry to `agents` in `~/.buddy/config.json` with the model args in its `command` array (e.g. `"agy-flash": {"command": ["agy", "--model", "gemini-3.8-flash-medium"]}`) and select it via `--agent agy-flash` (passing `--model` explicitly still replaces the pinned `--model` flag cleanly without duplication).
