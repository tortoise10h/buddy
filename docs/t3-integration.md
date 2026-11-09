# T3 Code Integration Guide

This guide walks you through setting up and using **T3 Code** (`t3code`) as your worker execution backend in Buddy.

---

## 1. Prerequisites

Before using the `t3code` worker backend, ensure the **T3 Code** desktop application and `t3` CLI are installed and running on your system.

### Checking the CLI
Verify that `t3` is available in your PATH or in `~/.local/bin/t3`:

```sh
t3 --version
```

Example output:
```text
t3-code 1.2.0 (darwin-arm64)
```

Buddy automatically searches for the `t3` binary in the following order:
1. Explicit path in `~/.buddy/config.json` (`t3.bin_path`)
2. `BUDDY_T3_BIN` or `T3_BIN` environment variables
3. System `PATH` (via `which t3`)
4. `~/.local/bin/t3`
5. `/usr/local/bin/t3`

### Installing T3 Code
If `t3` is not installed:
- **Desktop Application**: Download and install the T3 Code desktop app from the official site or releases.
- **CLI Utility**: Ensure the CLI helper is installed to `~/.local/bin/t3` or your PATH. Many users install or link it via:
  ```sh
  # If bundled with the desktop app:
  ln -s "/Applications/T3 Code.app/Contents/Resources/bin/t3" ~/.local/bin/t3
  ```
- **Local Server**: Launch T3 Code once so its background daemon starts on `http://127.0.0.1:3773`.

---

## 2. Selecting the Worker Backend

Buddy maintains a strict separation between two backend dimensions:

| Configuration Key | Purpose | Supported Values | Default |
| :--- | :--- | :--- | :--- |
| `terminal_backend` | Hosts the Control Tower TUI session (`bin/buddy`) and dev server splits | `rex`, `herdr` | `rex` |
| `worker_backend` | Executes background coding agents and feature tasks | `rex`, `herdr`, `t3code` | `rex` |

### Why Control Tower TUI Must Stay on Rex
The central Control Tower (`bin/buddy`) requires a terminal multiplexer (such as Rex or Herdr) to manage interactive PTY panes, keybindings, and split windows for human control. **T3 Code is an autonomous HTTP/MCP agent execution engine, not a terminal multiplexer.**

Attempting to set `terminal_backend` to `t3code` will be rejected by `bin/buddy`:
```text
Error: terminal_backend is set to "t3code". T3 Code is a worker engine for executing tasks, not a terminal multiplexer.
Use 'rex' or 'herdr' for terminal_backend, and set worker_backend to 't3code'.
```

### Precedence Order for Worker Backend Resolution
When dispatching a task, Buddy resolves the worker engine using the following order of precedence:
1. Explicit CLI flag: `--worker-backend <rex|herdr|t3code>`
2. Explicit CLI flag: `--engine <name>` (alias)
3. Global persistent CLI flag: `--engine <name>`
4. Environment variable: `BUDDY_WORKER_BACKEND=<rex|herdr|t3code>`
5. Persistent config: `worker_backend` in `~/.buddy/config.json`
6. Persistent config: `default_engine` in `~/.buddy/config.json`
7. Fallback default: `"rex"`

### Setting `t3code` as Default Worker Backend
You can configure `t3code` as your default worker backend persistently:
```sh
buddy-ctl config set worker_backend t3code
```

Verify your configuration:
```sh
buddy-ctl config get worker_backend
# Output: t3code
```

Or set the environment variable for your shell session:
```sh
export BUDDY_WORKER_BACKEND=t3code
```

---

## 3. Auth Pairing (30-Day OAuth PKCE)

T3 Code exposes a streamable `/mcp` API secured by 30-day OAuth 2.0 tokens. Before dispatching tasks, pair Buddy with your local T3 Code instance.

### Running the Pairing Flow
Run the login command:
```sh
buddy-ctl t3 auth login
```

Output:
```text
✓ Ephemeral loopback callback server listening on 127.0.0.1:54321
✓ Registered OAuth client with T3 Code (ID: buddy-ctl-...)

Opening browser for authorization...
If the browser did not open automatically, visit:
http://127.0.0.1:3773/oauth/mcp/authorize?client_id=...&code_challenge=...&scope=mcp:full_access

Waiting for approval...
✓ Exchanged authorization code for 30-day MCP access token

🎉 Successfully authenticated with T3 Code!
Scope:      mcp:full_access
Expires at: 2026-12-09T20:30:00Z (~30 days remaining)
Saved to:   ~/.buddy/t3_mcp_oauth.json (mode 0600)
```

### Headless / Remote Pairing (`--no-browser`)
If running on a remote server or headless environment without a local GUI browser:
```sh
buddy-ctl t3 auth login --no-browser
```
This prints the authorization URL directly to the terminal. Open the link on any browser that can reach the T3 Code server to approve.

### Token Storage and Security
- **Path**: `~/.buddy/t3_mcp_oauth.json`
- **File Permissions**: Strictly enforced as `0600` (`-rw-------`). Buddy refuses to store tokens inside `state.json`.
- **Token Contents**:
  ```json
  {
    "access_token": "mcp_tok_...",
    "origin": "http://127.0.0.1:3773",
    "issued_at": "2026-11-09T20:30:00Z",
    "expires_at": "2026-12-09T20:30:00Z",
    "scope": "mcp:full_access"
  }
  ```

### 30-Day Expiry & 72-Hour Warning
Buddy monitors token expiration automatically:
- When the token has more than 72 hours remaining, operations proceed silently.
- When less than 72 hours remain, Buddy injects proactive warning banners into status checks and context prompts:
  ```text
  [⚠️ T3 Code /mcp OAuth token expires in 48h (2026-12-09 20:30:00). Run 'buddy-ctl t3 auth login' to refresh.]
  ```
- When expired, operations fail with a clear recovery message:
  ```text
  Error: T3 Code MCP authorization required. Run 'buddy-ctl t3 auth login' to pair with T3 Code (token missing or expired).
  ```

### Checking Auth Health
To inspect the health and expiration of your pairing:
```sh
buddy-ctl t3 auth status --format text
```

Output:
```text
T3 Code Authentication Status
----------------------------------------
Server Origin:   http://127.0.0.1:3773 (online)
OAuth Token:     VALID
OAuth Path:      /Users/you/.buddy/t3_mcp_oauth.json
OAuth Expires:   2026-12-09T20:30:00Z (~718.5 hours remaining)
OAuth Scope:     mcp:full_access
```

---

## 4. First Dispatch

With `t3code` selected and authenticated, dispatch your first autonomous task.

### Fast-Path Dispatch
```sh
buddy-ctl dispatch --project myapp --task add-jwt-auth --prompt "Implement JWT auth middleware in src/auth.ts with unit tests" --worker-backend t3code
```

What happens automatically:
1. **Project Verification & Auto-Registration**: If `myapp` is not yet registered in T3 Code, Buddy calls `t3_project_create` via MCP to register it instantly without failing.
2. **Worktree Isolation**: T3 Code spawns the task in an isolated Git worktree (e.g. `worktrees/add-jwt-auth`).
3. **Execution**: The autonomous agent begins analyzing files, creating plans, and implementing changes.
4. **State Tracking**: Buddy records the session ID, block ID, and engine in `~/.buddy/state.json`.

JSON Response:
```json
{
  "project": "myapp",
  "task": "add-jwt-auth",
  "engine": "t3code",
  "status": "dispatched",
  "picked_up": true,
  "session_id": "proj_12345",
  "agent_block_id": "thread_67890"
}
```

### Specifying Models via `--agent`
Pass any provider and model supported by your T3 Code instance using `<provider>/<model>` format:

```sh
buddy-ctl dispatch \
  --project myapp \
  --task perf-tuning \
  --agent antigravity/gemini-4-argon-high \
  --prompt "Profile database queries and add caching" \
  --worker-backend t3code
```

Common agent spec examples:
- `antigravity/gemini-4-argon-high`
- `anthropic/claude-3-5-sonnet`
- `openai/gpt-4o`

### Batch Dispatching Multiple Tasks
Launch multiple tasks across different repositories simultaneously:

```sh
buddy-ctl batch --json '[
  {"project": "web-frontend", "task": "nav-refresh", "prompt": "Redesign top navigation bar", "engine": "t3code"},
  {"project": "auth-service", "task": "session-fix", "prompt": "Fix token refresh race condition", "engine": "t3code"}
]'
```

---

## 5. Day-to-Day Commands

### Check Status
Inspect all active projects, tasks, and worker engines:
```sh
buddy-ctl status
```

Example output:
```text
Projects & Tasks
--------------------------------------------------------------------------------
myapp
  [t3code] add-jwt-auth (waiting_for_input)
    Prompt: Implement JWT auth middleware in src/auth.ts with unit tests
    Worktree: /Users/you/Dev/myapp/.git/worktrees/add-jwt-auth
```

### Capture Output / Transcript
Read the recent turn history and logs from the worker agent:
```sh
buddy-ctl capture --project myapp --task add-jwt-auth --lines 50
```

### Answering Interactive Questions (`--answer`)
When T3 Code reaches an interactive decision point, its status transitions to `waiting_for_input`. You can answer using `--answer` flags:

- **Positional Answers**:
  ```sh
  buddy-ctl send --project myapp --task add-jwt-auth --answer "Use jsonwebtoken" --answer "15m expiration"
  ```
- **Keyed Answers (`key=value`)**:
  ```sh
  buddy-ctl send --project myapp --task add-jwt-auth --answer "library=jsonwebtoken" --answer "expiry=15m"
  ```
- **Free-Form Follow-Up Instructions**:
  ```sh
  buddy-ctl send --project myapp --task add-jwt-auth --text "Also make sure to handle expired tokens with a 401 response"
  ```

### Demonstrating Changes (`demo --cmd`)
Launch a development server for the task worktree:
```sh
buddy-ctl demo --project myapp --task add-jwt-auth --cmd "npm run dev"
```
Buddy launches the dev server inside a dedicated Rex terminal tab, running directly inside the task's isolated worktree. The Rex window handle is tracked in `state.json` under `dev_host_handle`.

### Closing Tasks & Dev Server Reaping
When task development is complete:
```sh
buddy-ctl task close --project myapp --task add-jwt-auth
```
Buddy performs clean, dual-reaping:
1. Interrupts and archives the T3 Code thread.
2. Gracefully closes the host Rex dev server window tracked by `dev_host_handle`.
3. Reaping is completely idempotent (safe to run repeatedly).

### Zero-OAuth Shell Snapshot
Inspect all active T3 threads, models, and execution states without OAuth tokens or node startup overhead:
```sh
buddy-ctl t3 snapshot
```
Returns raw JSON from `GET /api/orchestration/shell` in under 20ms.

---

## 6. Honest Limitations & Architectural Boundaries

Keep the following operational characteristics in mind:

1. **`buddy-ctl focus` is a No-Op for `t3code`**:
   - `focus` switches tabs in Rex or Herdr terminal multiplexers.
   - Because T3 Code manages its UI in its desktop application, `buddy-ctl focus --project myapp --task add-jwt-auth` performs a safe, clean no-op.
2. **`demo --cmd` Launches in Host Terminal (Rex), Not T3 Code**:
   - T3 Code does not provide a terminal window for interactive web dev servers (Vite, Next.js, etc.).
   - Buddy therefore launches dev servers in your host `terminal_backend` (Rex), rooted at the task's worktree.
3. **Session Reconnection**:
   - MCP connections are connection-scoped. If the T3 Code desktop app is restarted, Buddy automatically re-establishes the handshake and session using the 30-day token on the next command.
4. **Localhost Scope**:
   - T3 Code's API runs locally (`http://127.0.0.1:3773`). If accessing a remote server, port forwarding (e.g. `ssh -L 3773:127.0.0.1:3773 user@remote`) is required.

---

## 7. Troubleshooting

### 1. Missing or Expired OAuth Token
**Symptom**:
```text
Error: T3 Code MCP authorization required. Run 'buddy-ctl t3 auth login' to pair with T3 Code (token missing or expired).
```
**Fix**:
Run `buddy-ctl t3 auth login` to renew the 30-day token. Verify with `buddy-ctl t3 auth status`.

### 2. `t3` Binary Not Found
**Symptom**:
```text
Error: t3 binary not found in PATH or standard locations
```
**Fix**:
1. Check if T3 Code is installed.
2. Ensure `~/.local/bin` is in your `$PATH`, or set `BUDDY_T3_BIN`:
   ```sh
   export BUDDY_T3_BIN="/Applications/T3 Code.app/Contents/Resources/bin/t3"
   ```
3. Or update `~/.buddy/config.json`:
   ```json
   {
     "t3": {
       "bin_path": "/path/to/t3"
     }
   }
   ```

### 3. T3 Code Server Offline
**Symptom**:
```text
Error: failed to connect to T3 Code server: connection refused on 127.0.0.1:3773
```
**Fix**:
Start the T3 Code desktop application. Check server status with `buddy-ctl t3 auth status`.

### 4. Tasks Dispatched to Rex Instead of T3 Code
**Symptom**:
Dispatched task opens in a Rex terminal pane rather than T3 Code.
**Fix**:
Verify your worker backend resolution:
```sh
buddy-ctl config get worker_backend
```
Ensure it outputs `t3code`. If empty or `rex`, run:
```sh
buddy-ctl config set worker_backend t3code
```
Or override for the command:
```sh
buddy-ctl dispatch --project myapp --worker-backend t3code --prompt "..."
```

### 5. Authorization Permission / Scope Errors
**Symptom**:
```text
Error: insufficient scope for tool execution: required 'mcp:full_access'
```
**Fix**:
Re-authenticate with full access:
```sh
buddy-ctl t3 auth login
```
Ensure you approve the request with full workspace access in the browser dialog.
