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
               | buddy-ctl (Go CLI tool)       |
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

## Execution Backends

Buddy cleanly separates two backend dimensions:

1. **Terminal Backend (`terminal_backend`, default: `"rex"`)**:
   - Hosts the central Buddy Control Tower TUI session (`bin/buddy`) and interactive demo server tabs (`buddy-ctl demo --cmd`).
   - Supported options: `rex` (Rex terminal multiplexer) or `herdr` (Herdr terminal multiplexer).
   - *Note: `terminal_backend` cannot be set to `t3code` because Control Tower requires an interactive terminal multiplexer.*

2. **Worker Backend (`worker_backend`, default: `"rex"`)**:
   - Executes autonomous coding tasks and feature implementations dispatched via `buddy-ctl dispatch` or `buddy-ctl task start`.
   - Supported options:
     - `rex`: Spawns CLI agents (`agy`, `pi`, `opencode`) into dedicated Rex tabs with PTY interaction.
     - `herdr`: Spawns CLI agents in Herdr tabs.
     - `t3code`: Runs autonomous coding threads in T3 Code via localhost HTTP and Streamable `/mcp` APIs, utilizing native git worktrees and structured Q&A.

To configure defaults:
```sh
buddy-ctl config set terminal_backend rex
buddy-ctl config set worker_backend t3code
```
Or override per command:
```sh
buddy-ctl dispatch --project docnotate --worker-backend t3code --prompt "Implement auth flow"
```

---

## T3 Code Integration

> For the full end-to-end setup guide, prerequisites, troubleshooting, and daily usage reference, see the [T3 Code Integration Guide](docs/t3-integration.md).

When using `t3code` as your worker backend:
- **30-Day OAuth Pairing**: Pair once every ~30 days with a single command:
  ```sh
  buddy-ctl t3 auth login
  ```
  Buddy starts an ephemeral loopback HTTP server, generates a PKCE `S256` challenge, and pairs with T3 Code via `POST /oauth/mcp/register` and `/oauth/mcp/token`. The 30-day token is securely stored in `~/.buddy/t3_mcp_oauth.json` with file mode `0600`.
- **Check Auth Health**:
  ```sh
  buddy-ctl t3 auth status
  ```
  Displays token status, remaining hours, and scope. Proactive warnings surface in `status` and `context-inject` if the token is within 72 hours of expiration.
- **Agent Models (`<provider>/<model>`)**:
  Pass any provider and model supported by your T3 Code desktop app directly via `--agent`:
  ```sh
  buddy-ctl dispatch --project docnotate --worker-backend t3code --agent antigravity/gemini-4-argon-high --prompt "Optimize search"
  ```
- **Interactive Multi-Question Answering**:
  ```sh
  buddy-ctl send -p docnotate -t search-opt --answer "option A" --answer "env=production"
  ```
- **Zero-OAuth Watchdog Snapshots**:
  Monitor all threads in <20ms without OAuth or Node startup overhead:
  ```sh
  buddy-ctl t3 snapshot
  buddy-ctl watch --all
  ```
- **Cross-Host Demos & Reaping**:
  - `buddy-ctl demo -p <proj> -t <task>` without `--cmd`: Outputs a structured task summary (thread-level focus in T3 Code is unsupported).
  - `buddy-ctl demo -p <proj> -t <task> --cmd "npm run dev"`: Spawns the dev server inside your host terminal (`Rex`) rooted at T3's managed git worktree directory.
  - `buddy-ctl task close -p <proj> -t <task>`: Archives the T3 thread and automatically reaps the host Rex dev server tab (idempotent double-close).

---

## Installation

Clone and run the installer:

```sh
cd /Users/sunday/Dev/Personal/Projects/buddy
./install.sh           # Downloads pre-built binary (or compiles with Go if release unavailable)
./install.sh --build   # Or build locally from source with Go (tracks latest code on main)
```

During installation, Buddy prompts you for:
1. **Project root directories** where your repositories live (e.g. `~/Dev/Personal/Projects`, `~/Dev/Work/repos`, `~/Code/oss`):
   - Hit **Enter** to accept the default (`~/Dev/Personal/Projects`).
   - Enter multiple paths separated by commas or spaces, or add more iteratively when prompted.
   - Non-existent directories are validated with an option to auto-create them.
2. **Orchestrator and Worker agent harnesses**:
   - Buddy dynamically discovers installed agent harnesses from `agents` (e.g. `agy`, `opencode`, `pi`) and prompts you to select the harness for each role. Each harness starts with that agent's own default model.
   - Hit **Enter** to accept the default (`agy`, or previously configured values on re-install).
   - Unknown harness names are validated with re-prompting.

- Re-running `install.sh` on an existing installation automatically detects and preserves your configured roots and agent choices.
- For headless CI or scripted installs, set `BUDDY_NONINTERACTIVE=1` (and optionally `BUDDY_PROJECTS_ROOTS="dir1:dir2"`, `BUDDY_ORCHESTRATOR_AGENT="opencode"`, `BUDDY_WORKER_AGENT="agy"`).

This:
1. Links `bin/buddy` and `buddy-ctl` -> `~/.local/bin/`
2. Links `skills/buddy` into:
   - `~/.agents/skills/buddy`
   - `~/.gemini/config/skills/buddy`
   - `~/.gemini/antigravity-cli/skills/buddy`
   - `~/.pi/agent/skills/buddy`
   - `~/.config/opencode/skills/buddy`
3. Initializes `~/.buddy/` (`config.json`, `state.json`, `AGENTS.md`, `.agents/hooks.json`) with your configured project roots and agent roles.

---

## Agent Configuration & Roles

Buddy cleanly decouples the agent running the control tower from the agents performing implementation tasks:

1. **Orchestrator Agent** (`orchestrator_agent`, default: `"agy"`):
   - The agent running Buddy itself (the central control session), launched by `bin/buddy`.
   - Handles task routing, status monitoring, QA delegation, and demos.
2. **Worker Agent** (`worker_agent`, default: `"agy"`):
   - The default agent used when Buddy kicks off a new task or implementation tab (`buddy-ctl dispatch`, `buddy-ctl task start`).
   - Executes code modifications, tests, and feature work in project tabs.

### Example: Opencode Orchestrator with Agy Workers
A common setup is using `opencode` to coordinate the Buddy session while using `agy` for worker tasks:

```json
{
  "orchestrator_agent": "opencode",
  "worker_agent": "agy"
}
```

Or configure via `buddy-ctl`:
```sh
buddy-ctl config set orchestrator_agent opencode
buddy-ctl config set worker_agent agy
```

### Pinning a Model (Model Variants)
The install picker intentionally offers agent harnesses only (using each harness's own default model).

To pin an agent to a specific model:
1. Add an entry to the `agents` map in `~/.buddy/config.json` with the model arguments in its `command` array:
   ```json
   {
     "agents": {
       "agy-flash": {
         "command": ["agy", "--model", "gemini-3.8-flash-medium"],
         "submit_delay_ms": 1500,
         "submit_key": "enter"
       }
     }
   }
   ```
2. You can then select that pinned model on demand via `--agent`:
   ```sh
   buddy --agent agy-flash
   buddy-ctl dispatch --project docnotate --agent agy-flash --prompt "Refactor auth"
   ```
   Or set it as the persistent default for either role:
   ```sh
   buddy-ctl config set worker_agent agy-flash
   buddy-ctl config set orchestrator_agent agy-flash
   ```

### Command Array Resolution
Both settings resolve their full command array from `config.agents.<name>.command`. Variant agents configured with command arguments (such as `agy-flash` -> `["agy", "--model", "gemini-3.8-flash-medium"]`) are executed as full argument arrays rather than a single string.

### Resolution Precedence
- **Orchestrator Agent**:
  1. Explicit CLI argument (`buddy --agent <name>`, `buddy -a <name>`, or `buddy <name>`)
  2. `BUDDY_AGENT` environment variable
  3. `config.orchestrator_agent` (set via `install.sh`, `buddy-ctl config set`, or config file)
  4. `config.default_agent` (legacy fallback)
  5. Fallback default: `"agy"`
- **Worker Agent**:
  1. Explicit CLI argument (`buddy-ctl dispatch --agent <name>`, `buddy-ctl task start --agent <name>`)
  2. `config.worker_agent` (set via `install.sh`, `buddy-ctl config set`, or config file)
  3. `config.default_agent` (legacy fallback)
  4. Fallback default: `"agy"`

### Backward Compatibility
Existing `~/.buddy/config.json` files that only define `default_agent` continue to work without modification. The legacy `default_agent` key acts as the fallback for both orchestrator and worker roles when `orchestrator_agent` or `worker_agent` is not explicitly set.

---

## Quickstart

### Launch Buddy
```sh
buddy
```
Switches to or creates the `Buddy` Rex session and launches your orchestrator agent (`agy`).

Use `buddy --help` (or `-h`) to inspect launcher options, supported agents, and agent resolution rules. To pass options directly through to the agent without interpretation, use `--` (e.g. `buddy -- --model pro`). Unrecognized options passed directly to `buddy` will report an error and the full help message to stderr and exit with code 2.

### CLI Usage (`buddy-ctl`)

Run `buddy-ctl --help` for top-level command usage or `buddy-ctl <subcommand> --help` for command-specific flags. Any unknown options or invalid subcommands print an error and the full help message to stderr and exit with code 2.

```sh
# View tracked projects and tasks from cache (O(1))
buddy-ctl status

# Reconcile cache against live Rex sessions
buddy-ctl status --sync

# List available projects across all configured project roots
buddy-ctl projects
buddy-ctl projects --query docnotate

# Manage project roots
buddy-ctl projects roots
buddy-ctl projects add-root ~/Dev/Work/repos
buddy-ctl projects remove-root ~/Dev/Work/repos

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

## Documentation & Implementation Plans

- [T3 Code Integration Guide](docs/t3-integration.md)
- [Implementation Handoff & Context Notes](plans/buddy-handoff.md)
- [Full Architecture Plan](plans/buddy-terminal-orchestrator.md)
