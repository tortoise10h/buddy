# T3 Engine Integration: Progress & Parity Roadmap

## 1. Summary

T3 (`t3` CLI) is integrated as a first-class engine in `buddy-ctl` for **host detection, config/state isolation, dispatch, follow-up messaging (`send`), transcript inspection (`capture`), live status probing & `watch`, and state-level `task close`**, backed by `internal/backend/t3` and `internal/engine/t3.go`. `T3Engine` implements `engine.TaskStatusProvider` via `t3 thread show --json`, `watchRunner.probeTarget` queries it ahead of terminal pane scraping, direct `-b <threadID>` targeting works without `-s`, and `skills/buddy/SKILL.md` plus `templates/orchestrator-AGENTS.md` have been updated for T3.

Compared to `rex` (which uses `TerminalEngine` + `RexBackend`), **the single remaining P0 blocker** to a full autonomous T3 orchestrator loop is **state reconciliation & out-of-band thread adoption (`status --sync`, TTL auto-reconcile, and `task adopt`)**: `internal/cli/sync.go` only reconciles `rex`, so `~/.buddy/state/t3.json` never auto-updates, `status` leaves finished tasks as `"running"`, `context-inject` never fires proactive nudges, and threads started outside Buddy cannot be discovered (`t3 thread list --json` is not yet wrapped) or adopted. Secondary gaps are `focus`, `demo` (`OpenDevSplit`), and test-suite engine hermeticity when `go test ./...` runs inside a T3 session.

---

## 2. Capability Matrix

| Capability | Rex | T3 | Notes / Gap | Priority |
| :--- | :--- | :--- | :--- | :--- |
| **Host detection & engine resolution** | **DONE** (`internal/host/detect.go`: `DetectHostFromEnv`; `internal/engine/resolve.go`: `ResolveEngine`) | **DONE** (`internal/host/detect.go`: `T3SignalVars`, `DetectHostFromEnv`; `internal/engine/resolve.go`: `ResolveEngine`) | 5-tier precedence (`--engine` > `BUDDY_ENGINE`/`BUDDY_WORKER_BACKEND` > host env > `default_engine` > `"rex"`). T3 wins if both Rex & T3 env vars exist. | — |
| **Project / session registration & idempotency** | **DONE** (`internal/backend/rex/rex.go`: `EnsureSession`, `ListSessions`) | **DONE** (`internal/engine/t3.go`: `T3Engine.Dispatch`, `isProjectAlreadyExistsError`; `internal/backend/t3/runner.go`: `RunProjectAdd`) | T3 calls `t3 project add` and ignores `ProjectAlreadyExistsError` / `"already exists"`. Note: `buddy-ctl ensure-session` (`internal/cli/session.go`) is Rex-only. | P2 |
| **Task dispatch & slug mapping** | **DONE** (`internal/engine/terminal.go`: `TerminalEngine.Dispatch`; `internal/project/slug.go`: `DeriveSlug`) | **PARTIAL** (`internal/engine/t3.go`: `T3Engine.Dispatch`; `internal/cli/dispatch.go`; `internal/cli/task.go`) | Starts top-level thread via `t3 thread start --json` and maps `ThreadID` -> `WindowID` & `BlockID`, `ProjectID` -> `SessionID`. Requires `--prompt` (`task start` without `--prompt` fails on T3). | P1 |
| **Prompt delivery & pickup detection** | **DONE** (`internal/engine/terminal.go`: `waitReadyAndCapture`, `verifyPickup`, `isPromptEchoed`) | **DONE (by API contract)** (`internal/engine/t3.go`: `T3Engine.Dispatch`) | `t3 thread start` accepts prompt atomically and returns `ThreadID`/`RunID`; `T3Engine.Dispatch` sets `PickedUp: true` directly without TUI readiness probing. | — |
| **Send / follow-ups** | **DONE** (`internal/engine/terminal.go`: `TerminalEngine.Send`; `internal/cli/send.go`) | **PARTIAL** (`internal/engine/t3.go`: `T3Engine.Send`; `internal/backend/t3/runner.go`: `RunThreadSend`; `internal/cli/send.go`) | Text follow-ups work via `t3 thread send <id> <msg> --json`, and direct `-b <threadID>` targeting without `-s` is supported. Raw `--key` (e.g. `esc`, `ctrl+c`) returns an error. | P1 |
| **Capture (pane scrape vs thread show JSON)** | **DONE** (`internal/backend/rex/rex.go`: `RexBackend.Capture`) | **DONE** (`internal/engine/t3.go`: `T3Engine.Capture`, `formatThreadShowOutput`; `internal/backend/t3/runner.go`: `RunThreadShow`) | Calls `t3 thread show <id> --json` and formats `[thread: ... \| status: ...]` + `[role]` messages. Direct `-b <threadID>` targeting without `-s` works in `capture.go`, `send.go`, and `diagnose.go`. | — |
| **Close** | **DONE** (`internal/backend/rex/rex.go`: `RexBackend.Close`; `internal/cli/task.go`: `newTaskCloseCmd`) | **DONE (state-level detach)** (`internal/engine/t3.go`: `T3Engine.Close`; `internal/cli/task.go`: `newTaskCloseCmd`, commit `61b8183`) | `T3Engine.Close` returns `nil` and `task close` marks `closed` in `t3.json` without requiring `sessID`. Caveat: does not close/archive in T3 UI (`t3` CLI exposes no close/archive subcommand). | — |
| **Focus** | **DONE** (`internal/backend/rex/rex.go`: `RexBackend.Focus`; `internal/cli/focus.go`) | **PARTIAL** (`internal/engine/t3.go`: `T3Engine.Focus` is a no-op returning `nil`; `internal/backend/t3/runner.go`: `DefaultCommandRunner.App`) | `App` (`t3 app <project-path>`) exists in `runner.go`, but `T3Engine.Focus` does not call it and returns `nil` silently. | P2 |
| **Live status detection (`running`/`planning`/`ready_for_demo`/`waiting_for_input`/`error`)** | **DONE** (`internal/cli/watch.go`: `evalProgramStatus`, `paneState`; `internal/cli/sync.go`: `probeLiveBlockStatus`) | **DONE** (`internal/engine/engine.go`: `TaskStatusProvider`; `internal/engine/t3.go`: `T3Engine.ProbeStatus`, commits `d5e4111`, `8f12571`) | Maps `t3 thread show --json` (blocking pending requests, `pendingRequestCount > 0`, `plan` interaction mode, error/failed/interrupted/cancelled/rolled_back, idle/completed) into structured `TaskStatusProbe` + 1-line summary + snapshot. | — |
| **Reconcile / sync & out-of-band adoption (`status --sync`, `task adopt`)** | **DONE** (`internal/cli/sync.go`: `reconcileRexWithBackend`, `reconcileRexIfStale`; `internal/cli/task.go`: `newTaskAdoptCmd`) | **MISSING** (`internal/cli/status.go`, `dispatch.go`, `task.go`, `watch.go`, `context_inject.go` gate sync on `rex`; `newTaskAdoptCmd` hard-codes `defaultRexBackend()`) | `T3Runner` and `runner.go` have no `ThreadList` wrapper for `t3 thread list --json`, and `sync.go` has no T3 equivalent. Untracked T3 threads cannot be discovered or adopted. | **P0** |
| **Watch (event detection, timeouts, `already_reported`)** | **DONE** (`internal/cli/watch.go`: `watchRunner.run`, `probeTarget`, `pollTarget`) | **DONE** (`internal/cli/watch.go`: `probeTarget` checks `engine.TaskStatusProvider` first, commits `490dd0e`, `7b7525a`) | `watch --engine t3` detects `ready_for_demo`, `waiting_for_input`, and `error` cleanly without pane scraping. Verified live against T3. | — |
| **Dev split (`OpenDevSplit` / `demo`)** | **DONE** (`internal/backend/rex/rex.go`: `OpenDevSplit`; `internal/cli/demo.go`) | **MISSING** (`internal/engine/t3.go`: `T3Engine.OpenDevSplit` returns `*ErrNotImplemented`) | `demo.go` has no `t3` branch: `buddy-ctl demo --cmd ...` errors with `ErrNotImplemented`, and `--focus-only` silently no-ops. | P2 |
| **Program status (OSC 7501) & block liveness** | **DONE** (`internal/backend/rex/rex.go`: `GetProgramStatus`, `IsBlockAlive`; `internal/engine/terminal.go`) | **PARTIAL** (`T3Engine` implements `TaskStatusProvider` for `watch`, but has no `ThreadList` / `sync` liveness pass) | T3 has no terminal blocks or OSC 7501. `watch` uses `ProbeStatus`; `sync` / `status --sync` still lacks thread-level liveness reconciliation. | P0 (via `sync`) |
| **Multi-project / multi-session listings** | **DONE** (`internal/cli/status.go` `--all-engines`; `internal/cli/projects.go`; `RexBackend.ListSessions`) | **PARTIAL** (`internal/cli/status.go`: `--engine t3` & `--all-engines` read `t3.json`; `internal/cli/projects.go` reads active engine state) | Cached listing works (`status --engine t3`, `status --all-engines`). Live listing from T3 is missing (`t3 project` has no `list` subcommand; `t3 thread list` is per-project and unwrapped). | P1 |
| **State persistence & cache reconciliation per engine** | **DONE** (`internal/state/state.go`, `internal/state/save.go`: `LoadStateForEngine`, `MutateStateForEngine`) | **PARTIAL** (`internal/state/state.go`: `~/.buddy/state/t3.json` & `.t3_seen.json` isolated with flock; legacy migration removed in `47641a7`) | Per-engine state storage is **DONE**, but `ProjectState` fields are still named `RexSessionID` / `RexSessionLabel` and cache reconciliation (`sync.go`) only runs for `rex`. | P0 (reconcile) / P2 (field naming) |
| **Notifications** | **DONE** (`internal/notify/notify.go`: `SendDesktopNotification`; `internal/cli/watch.go`: `emitEvent`; `internal/cli/notify.go`) | **DONE** (`watch.go`: `emitEvent` fires for T3 transitions via `TaskStatusProvider`, unblocked by `490dd0e`) | Both `buddy-ctl notify` and `watch`-triggered desktop notifications work for T3. | — |
| **Docs & skill contract** | **DONE** (`skills/buddy/SKILL.md`, `templates/orchestrator-AGENTS.md`) | **DONE** (`skills/buddy/SKILL.md`, `templates/orchestrator-AGENTS.md`, commit `e969f2e`) | Updated to cover T3 engine workflows, `--agent <provider>/<model>`, and non-terminal caveats. | — |

---

## 3. What Is Already Done

### Host Detection & Engine Resolution
- `internal/host/detect.go` (`DetectHostFromEnv`, `T3SignalVars`, `RexSignalVars`): Detects `HostT3` from T3 environment variables (`T3CODE_THREAD_ID`, `T3CODE_CLI_PATH`, `T3CODE_HOME`, `T3_MCP_*`, `TERM_PROGRAM=t3code`, `__CFBundleIdentifier=com.t3tools.t3code*`, `/.t3/` in `ANTIGRAVITY_HARNESS_PATH`/`GEMINI_HOME`), with T3 winning deterministically over Rex when nested.
- `internal/engine/resolve.go` (`ResolveEngine`, `WarnUnknownHost`): Implements 5-tier adaptive engine selection (`--engine` flag > `BUDDY_ENGINE`/`BUDDY_WORKER_BACKEND` > host detection > `cfg.DefaultEngine` > `"rex"`).
- `internal/cli/engine.go` (`newEngineCmd`): Exposes `buddy-ctl engine` / `buddy-ctl host` for debugging host detection and active engine resolution.

### T3 CLI Runner, Engine Core & Live Status Probing
- `internal/backend/t3/binary.go` (`FindT3Binary`): Resolves `t3` binary via `BUDDY_T3_BIN` > `T3CODE_CLI_PATH` > `PATH` > `~/.local/bin/t3` > `/usr/local/bin/t3`.
- `internal/backend/t3/runner.go` (`DefaultCommandRunner`, `App`, `ProjectAdd`, `ThreadStart`, `ThreadShow`, `ThreadSend`): Wraps `t3 app`, `t3 project add`, `t3 thread start --json`, `t3 thread show --json`, and `t3 thread send --json` with JSON parsing and validation.
- `internal/engine/engine.go` (`TaskStatusProvider`, `TaskStatusProbe`): Defines the structured live-status capability interface so engines can report `Status`, `Working`, `Event`, `Summary`, and `Snapshot` without terminal pane scraping.
- `internal/engine/t3.go` (`T3Engine`, `Dispatch`, `Send`, `Capture`, `ProbeStatus`, `Close`, `formatThreadShowOutput`): Implements `engine.Engine` and `TaskStatusProvider`:
  - `Dispatch`: idempotent `ProjectAdd` + `ThreadStart`.
  - `Send`: `ThreadSend` (rejects raw `--key` input).
  - `Capture`: `ThreadShow` formatted into `[thread: ... | status: ...]` + `[role]` blocks.
  - `ProbeStatus` (commits `d5e4111`, `8f12571`): maps `t3 thread show --json` (blocking `PendingRuntimeRequest`, `PendingRequestCount > 0`, `plan` interaction mode, `error`/`failed`/`interrupted`/`cancelled`/`rolled_back`, `idle`/`completed`) to Buddy statuses with a 1-line summary and snapshot.
  - `Close` (commit `61b8183`): returns `nil` as a state-level detach so Buddy stops tracking the thread cleanly.
- `internal/cli/watch.go` (`watchRunner.probeTarget`, commits `490dd0e`, `7b7525a`): Checks `engine.TaskStatusProvider` first before falling back to `backend.ProgramStatusProvider` and terminal pane scraping. Verified live with `buddy-ctl watch --engine t3` detecting `ready_for_demo` and `error`.
- `internal/cli/capture.go`, `internal/cli/send.go`, `internal/cli/diagnose.go`, `internal/cli/task.go`: Relaxed `sessID` requirement (`if blkID == "" || (engName != "t3" && sessID == "")` / `winID` equivalent in `newTaskCloseCmd`) so direct `-b <threadID>` / `-w <threadID>` targeting works on T3 without `-s`.
- `internal/engine/registry.go` & `internal/cli/common.go` (`getRegistry`, `resolveWorkerForEngine`): Registers `"t3"` alongside `"rex"` and resolves `<provider>/<model>` from `--agent`, `BUDDY_T3_MODEL`, or `cfg.Engines["t3"]` (commits `0f94012`, `e8ce813`).

### Per-Engine Config, State Isolation & Docs
- `internal/config/config.go` (`EngineConfig`, `GetT3ProviderModel`, `GetEffectiveWorkerAgent`, `GetEffectiveOrchestratorAgent`): Organizes `engines.t3.{provider,model}` separately from terminal engines (commit `0f94012`).
- `internal/state/state.go` & `internal/state/save.go` (`LoadStateForEngine`, `MutateStateForEngine`, `GetStatePathForEngine`, `GetStateSeenPathForEngine`, `ListStateEngines`): Isolates state in `~/.buddy/state/t3.json` and `.t3_seen.json` with per-engine `flock` locking (commit `5432dd5`; legacy `state.json` migration removed in commit `47641a7`).
- `internal/cli/context_inject.go` (`newContextInjectCmd`): Loads `t3.json` / `.t3_seen.json` when resolved to `t3`, reports `Orchestrator: t3 (direct)` and configured `<provider>/<model>`, and emits proactive nudges if state transitions are recorded.
- `skills/buddy/SKILL.md` & `templates/orchestrator-AGENTS.md` (commit `e969f2e`): Rewritten to document T3 alongside Rex, including `--agent <provider>/<model>` and non-terminal caveats.

### Existing Test Coverage
- `internal/backend/t3/binary_test.go` (6 tests): Precedence of `BUDDY_T3_BIN`, `T3CODE_CLI_PATH`, `PATH`, `~/.local/bin/t3`, `/usr/local/bin/t3`, and `ErrBinaryNotFound`.
- `internal/backend/t3/runner_test.go` (17 tests): Arg builders, JSON parsing (`ThreadStartOutput`, `ThreadShowOutput`), mock runner execution for `App`/`ProjectAdd`/`ThreadStart`/`ThreadShow`/`ThreadSend`, and validation/error cases.
- `internal/engine/t3_test.go`: Compile-time check `var _ TaskStatusProvider = (*T3Engine)(nil)`, `TestT3Engine_Dispatch_AddsProjectAndStartsThread`, `TestT3Engine_Dispatch_MissingProviderModelFailsFast`, `TestT3Engine_Dispatch_ValidationAndErrors`, `TestT3Engine_Dispatch_UsesEnvProviderModelFallback`, `TestT3Engine_Send`, `TestT3Engine_Capture`, `TestT3Engine_ProbeStatus`.
- `internal/cli/watch_test.go`: `TestWatch_T3_TransitionsAndProbeErrors`.
- `internal/cli/dispatch_test.go`: `TestDispatchAndSend_T3Routing_UsesT3Engine`, `TestDispatch_T3_MissingModelFailsFast`.
- `internal/cli/engine_test.go`: `TestEngineCmd_T3Detection_JSON`, `TestEngineCmd_FlagOverride`.
- `internal/cli/status_test.go`: `TestStatusCmd_EngineStateIsolation`.
- `internal/state/save_test.go`: `TestState_SaveAndMutateWithLock`, `TestState_PerEngineIsolation`, `TestState_PerEnginePathsAndFreshStart`.
- `internal/host/detect_test.go`: `TestDetectHostFromEnv` (T3 vs Rex signal precedence).

*(Note: The T3-specific tests above pass when T3 is resolved, but `go test ./...` is not currently hermetic across engines—see Gap #5.)*

---

## 4. Gaps and Why They Matter

1. **No T3 state reconciliation (`status --sync` / TTL auto-sync) (`internal/cli/sync.go`, `status.go`, `dispatch.go`, `task.go`, `context_inject.go`, `watch.go`)**
   - **What breaks**: `internal/cli/sync.go` contains only `reconcileRexIfStale` and `reconcileRexWithBackend`. Every call site (`status.go`, `dispatch.go`, `task.go`, `context_inject.go`, `watch.go`) gates live reconciliation on `rex`. On T3, `~/.buddy/state/t3.json` never auto-updates from live T3 thread state: tasks stay `"running"` in `status` and `[Buddy Live State]`, `last_summary` never refreshes outside `watch`, and `context-inject` never fires `[🚨 BUDDY PROACTIVE NUDGE]` for completed/blocked T3 threads.

2. **No out-of-band T3 thread discovery or adoption (`internal/cli/task.go`: `newTaskAdoptCmd`, `internal/engine/t3.go`, `internal/backend/t3/runner.go`)**
   - **What breaks**: `T3Runner` (`internal/engine/t3.go`) and `DefaultCommandRunner` (`internal/backend/t3/runner.go`) have no `ThreadList` wrapper for `t3 thread list --json`, and `newTaskAdoptCmd` (`internal/cli/task.go`) hard-codes `tb := defaultRexBackend()` and only reconciles when `engName == "rex"`. Threads started or replied to directly in the T3 Code desktop app/UI cannot be discovered into `UntrackedWindows` or adopted via `buddy-ctl task adopt --engine t3`.

3. **`task close` silent-success / UI-drift caveat (`internal/engine/t3.go`: `T3Engine.Close`, `internal/cli/task.go`: `newTaskCloseCmd`)**
   - **What breaks**: `buddy-ctl task close -p <proj> -t <slug> --engine t3` prints `{"status":"closed"}` and marks the task `closed` in `t3.json`, but `T3Engine.Close` is a state-only detach that does **not** touch T3—the thread remains open and visible in the T3 UI. While T3's MCP API supports `settle`/`archive` (`t3_thread_organize`), the `t3` CLI exposes no close or archive subcommand, so `buddy-ctl` cannot close the thread server-side today.

4. **`demo` and `focus` gaps (`internal/engine/t3.go`: `Focus`, `OpenDevSplit`; `internal/cli/demo.go`)**
   - **What breaks**: `buddy-ctl focus` succeeds silently without actually focusing T3 (`T3Engine.Focus` returns `nil` without calling `runner.App`). `internal/cli/demo.go` has no `t3` branch, so `buddy-ctl demo --cmd "..."` fails with `ErrNotImplemented` and `--focus-only` is a silent no-op.

5. **Test suite is not hermetic w.r.t. engine selection (`internal/cli`, `internal/engine`)**
   - **What breaks**: Running `go test ./...` from inside a T3 session fails in `internal/cli` (multiple rex-oriented tests fail with `"no projects tracked in state"` because they never pin `BUDDY_ENGINE`, so `resolveActiveEngine("")` host-detects `t3` from `T3CODE_*` / `__CFBundleIdentifier` and reads an empty `t3.json`). Conversely, running `BUDDY_ENGINE=rex go test ./...` breaks the T3 tests (`TestDispatchAndSend_T3Routing_UsesT3Engine`, `TestDispatch_T3_MissingModelFailsFast`, and `TestNilEnvAndConfig` in `internal/engine`). There is no single environment variable setting that makes the full suite green; each test must pin its own engine via `t.Setenv("BUDDY_ENGINE", ...)` and/or explicit `--engine` flags.

---

## 5. Proposed Roadmap

### Phase 1: T3 Live Status Probing & Working `watch` Loop — **DONE** (commits `d5e4111`, `8f12571`, `490dd0e`, `7b7525a`)
- **Completed**:
  - `internal/engine/engine.go`: Added `TaskStatusProvider` and `TaskStatusProbe`.
  - `internal/engine/t3.go`: Implemented `(*T3Engine).ProbeStatus` via `t3 thread show --json`.
  - `internal/cli/watch.go`: Rewired `watchRunner.probeTarget` to check `engine.TaskStatusProvider` first.
  - `internal/cli/capture.go`, `internal/cli/send.go`, `internal/cli/diagnose.go`: Relaxed `sessID` guard for `t3` (`blkID != ""` only).
  - Tests: `TestT3Engine_ProbeStatus` (`internal/engine/t3_test.go`) and `TestWatch_T3_TransitionsAndProbeErrors` (`internal/cli/watch_test.go`) pass, and `buddy-ctl watch --engine t3` was verified live.

### Phase 2: T3 State Reconciliation (`status --sync`) & Untracked Thread Adoption (`task adopt`) — **NEXT (P0)** — **Size: M**
- **Objective**: Keep `~/.buddy/state/t3.json` fresh automatically, enable `context-inject` nudges for T3, and allow adopting out-of-band T3 threads.
- **Concrete Code Changes**:
  - `internal/backend/t3/runner.go`: Add `ThreadListOutput` / `ThreadSummary`, `BuildThreadListArgs(projectPath string)`, `RunThreadList(...)`, and `DefaultCommandRunner.ThreadList(ctx, projectPath)` wrapping `t3 thread list --project <path> --json`.
  - `internal/engine/t3.go`: Add `ThreadList` to `T3Runner` interface and expose `ListProjectThreads(ctx, projectPath)` (or implement a `ThreadReconciler` optional capability interface).
  - `internal/cli/sync.go`: Add `reconcileT3IfStale` and `reconcileT3WithEngine` to probe tracked T3 tasks via `ThreadShow` (updating `Status`, `LastSummary`, `LastVerifiedAt`, or `missing` if thread not found) and discover untracked threads via `t3 thread list --json` into `UntrackedWindows`.
  - `internal/cli/status.go`, `internal/cli/dispatch.go`, `internal/cli/task.go`, `internal/cli/watch.go`, `internal/cli/context_inject.go`: Dispatch reconciliation based on `engineName` (`rex` vs `t3`) instead of gating only on `rex`.
  - `internal/cli/task.go` (`newTaskAdoptCmd`): Support `engName == "t3"` by looking up untracked T3 threads or querying `t3 thread show` / `t3 thread list` instead of calling `defaultRexBackend()`.
- **Verification**:
  - Unit tests in `internal/backend/t3/runner_test.go` for `t3 thread list --json` parsing.
  - Unit tests in `internal/cli/status_test.go` and `internal/cli/dispatch_test.go` for `status --sync --engine t3` and `task adopt --engine t3`.

### Phase 3: Task Lifecycle Cleanup (`task close`, `focus`, `task start` without prompt) — **PARTIAL** — **Size: S**
- **Completed**:
  - `task close` state-level detach (`T3Engine.Close` returning `nil` and relaxed `sessID` guard in `newTaskCloseCmd`, commit `61b8183`).
- **Remaining**:
  - `internal/engine/t3.go` (`Focus`): Wire `T3Engine.Focus` to `e.runner.App(ctx, projectPath)` (resolving `projectPath` from state/cache or `ThreadShow`).
  - Verification: `internal/engine/t3_test.go` test for `Focus` calling `App`.

### Phase 4 (Optional / Polish): Docs, Skill Updates & `demo` Semantics — **PARTIAL** — **Size: S**
- **Completed**:
  - `skills/buddy/SKILL.md` & `templates/orchestrator-AGENTS.md` (commit `e969f2e`): Updated with T3 engine usage, `--agent <provider>/<model>`, and non-terminal caveats.
- **Remaining**:
  - `internal/cli/demo.go` (`newDemoCmd`): When `engName == "t3"`, focus the T3 app (`eng.Focus`) and skip/warn gracefully on `--cmd` rather than failing on `OpenDevSplit`, or only error when `--cmd` is explicitly passed.
  - Verification: `buddy-ctl demo --engine t3 -p <proj> -t <slug> --focus-only`.

---

## 6. Open Questions / Risks

1. **`Engine` Interface Mismatch (Terminal Multiplexer vs. Thread API)**
   - `internal/engine/engine.go` defines `Engine` around multiplexer concepts (`WindowID`, `BlockID`, `Capture(lines int)`, `Send(backend.Input{Text, Key, Enter})`, `OpenDevSplit`).
   - T3 has no panes, raw keystrokes (`--key`), or terminal splits, and returns structured JSON (`ThreadInfo.Status`, `PendingRequestCount`, `Messages`) rather than raw terminal buffers.
   - **Pattern established**: `TaskStatusProvider` (`internal/engine/engine.go`) avoids forcing T3 status through `Capture()` string-scraping; follow the same optional-capability pattern (e.g. `ReconcilableEngine`) for `sync.go`.

2. **`t3 thread show --json` Status Enum Values & `t3 thread list --json` Schema**
   - `t3 thread show --json` status values have now been **verified** against a live T3 server (`running`, `completed`, `failed`, `idle`, `interrupted` observed), and `T3Engine.ProbeStatus` handles them along with `plan` mode, blocking pending requests, `pendingRequestCount > 0`, and structured errors.
   - **Remaining unverified piece**: `t3 thread list --json` payload schema has not yet been exercised by a `runner.go` wrapper and should be verified when implementing Phase 2.

3. **No `t3 thread close` / `archive` or `t3 project list` in T3 CLI**
   - Verified via `t3 project --help` (`add`, `remove`, `rename`) and `t3 thread --help` (`list`, `start`, `send`, `show`).
   - While T3's MCP API supports `settle` / `archive` (`t3_thread_organize`), the `t3` CLI exposes no close or archive subcommand, so `buddy-ctl` cannot reach it today.
   - **Implication**: `buddy-ctl task close --engine t3` marks the task `closed` in Buddy's `t3.json` cache (state-only detach) while leaving the thread open in the T3 UI. Similarly, `t3` reconciliation can only list threads for projects already known in `t3.json` or configured `projects_roots`.

4. **`T3Engine.Focus` Needs `projectPath`, Not `sessionID`/`windowID`**
   - `Engine.Focus(ctx, sessionID, windowID)` does not receive `projectPath`, whereas `t3 app <project-path>` takes a filesystem path. Either `T3Engine` resolves path from state/cache or `Focus` looks up `ProjectPath`.

---

## 7. Verification Checklist (Rex vs. T3 Side-by-Side)

Pre-requisites:
- Ensure `t3` server/app is running and T3 provider/model is configured (`buddy-ctl config set engines.t3.provider <provider>` and `engines.t3.model <model>`, or `BUDDY_T3_MODEL=<provider>/<model>`).
- Build latest binary: `make build` (uses `./bin/buddy-ctl`).

| Step | Rex Command | T3 Command | Expected Today |
| :--- | :--- | :--- | :--- |
| **1. Engine resolution** | `./bin/buddy-ctl engine --engine rex --json` | `./bin/buddy-ctl engine --engine t3 --json` | Both return `engine` and `source: "flag"`. |
| **2. Dispatch** | `./bin/buddy-ctl dispatch --engine rex -p <proj> -t test-rex -m "Reply with OK only"` | `./bin/buddy-ctl dispatch --engine t3 -p <proj> -t test-t3 -m "Reply with OK only"` | Rex spawns a Rex window (`window:...`, `surface:...`). T3 registers project and starts thread (`picked_up: true`, `window_id` == `agent_block_id` == thread ID). |
| **3. Status (cached & sync)** | `./bin/buddy-ctl status --engine rex --sync --format table` | `./bin/buddy-ctl status --engine t3 --sync --format table` | Rex reconciles live windows and updates status/summary. T3 reads `~/.buddy/state/t3.json` only (`--sync` is still a no-op on T3; status stays `running`). |
| **4. Capture** | `./bin/buddy-ctl capture --engine rex -p <proj> -t test-rex -n 40` | `./bin/buddy-ctl capture --engine t3 -p <proj> -t test-t3 -n 40` | Rex returns terminal pane lines. T3 returns `[thread: ... \| status: ...]` + `[user]`/`[assistant]` blocks. Direct `-b <threadID>` without `-s` works on T3. |
| **5. Send follow-up** | `./bin/buddy-ctl send --engine rex -p <proj> -t test-rex --text "Say DONE"` | `./bin/buddy-ctl send --engine t3 -p <proj> -t test-t3 --text "Say DONE"` | Both succeed (including `-b <threadID>` without `-s` on T3); T3 `--key esc` returns an error (`raw key input is not supported`). |
| **6. Watch** | `./bin/buddy-ctl watch --engine rex -p <proj> -t test-rex --timeout 30` | `./bin/buddy-ctl watch --engine t3 -p <proj> -t test-t3 --timeout 30` | Both detect `ready_for_demo` / `waiting_for_input` / `error` and exit early with a real event (`T3` via `TaskStatusProvider`). |
| **7. Close** | `./bin/buddy-ctl task close --engine rex -p <proj> -t test-rex` | `./bin/buddy-ctl task close --engine t3 -p <proj> -t test-t3` | Rex closes window and marks `closed`. T3 prints `{"status":"closed"}` and marks `closed` in `t3.json` (state-only detach; thread stays visible in T3 UI). |
