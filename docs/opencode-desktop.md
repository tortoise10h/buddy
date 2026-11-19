# Using Buddy with OpenCode Desktop (`--engine opencode`)

OpenCode v2 runs a **shared local background server** (`opencode serve --service`) that both **OpenCode Desktop** and the **`opencode` CLI** connect to. Buddy's `opencode` engine speaks directly to this shared server over HTTP/SSE, so tasks dispatched from `buddy-ctl` appear live in OpenCode Desktop—and sessions started directly in OpenCode Desktop or `opencode` CLI can be inspected and adopted by Buddy.

> [!NOTE]
> **`--engine opencode` vs. `--engine rex --agent opencode`**
> - `--engine opencode`: Controls OpenCode Desktop directly via its shared local HTTP/SSE server (state stored in `~/.buddy/state/opencode.json`).
> - `--engine rex --agent opencode`: Launches the `opencode` TUI inside a Rex terminal tab (state stored in `~/.buddy/state/rex.json`), which also connects to the same shared local server by default.

---

## 1. Prerequisites

- **OpenCode Desktop** (v2.2+ / v2.3+) and/or the **`opencode` CLI** installed.
- **`buddy-ctl`** built and installed:
  ```sh
  make build && make install
  ```

---

## 2. Verify the Shared Local Service

OpenCode Desktop automatically starts the shared background server and writes its loopback URL and auth token to `~/.local/state/opencode/service.json`.

Verify it is running:
```sh
opencode service status
# Example output: http://127.0.0.1:49374
```

If OpenCode Desktop is closed, `buddy-ctl` can auto-start the shared background service for you via `opencode service start` (enabled by default), so OpenCode Desktop will attach to the exact same server when you open it later.

---

## 3. Configure Buddy (Optional)

You can use `--engine opencode` on any command with zero configuration, or set it as your default engine and configure defaults via `buddy-ctl init`:

```sh
# Make OpenCode Desktop the default engine
buddy-ctl init --default-engine opencode

# Optional: configure default OpenCode agent mode (e.g. build, plan) or provider/model
buddy-ctl init --opencode-agent build --opencode-model opencode-go/mimo-v2.6-flash

# Or configure provider and model explicitly via config set
buddy-ctl config set engines.opencode.provider opencode-go
buddy-ctl config set engines.opencode.model mimo-v2.6-flash
```

This updates `~/.buddy/config.json` under `engines.opencode`:
```json
{
  "default_engine": "opencode",
  "engines": {
    "opencode": {
      "server_url": "",
      "auto_start_service": true,
      "default_agent": "",
      "provider": "opencode-go",
      "model": "mimo-v2.6-flash",
      "focus_desktop": true
    }
  }
}
```
When `engines.opencode.provider` and `engines.opencode.model` are configured, Buddy propagates `model: { providerID, id }` on both initial `dispatch` prompts and follow-up `send` prompts.

### Environment Overrides
- `BUDDY_ENGINE=opencode`: Force the `opencode` engine for the current shell.
- `BUDDY_OPENCODE_URL` / `OPENCODE_SERVER_URL`: Connect to a specific OpenCode server URL instead of `~/.local/state/opencode/service.json`.
- `BUDDY_OPENCODE_PASSWORD` / `OPENCODE_SERVER_PASSWORD`: Basic Auth password when using a custom server URL.
- `BUDDY_OPENCODE_BIN`: Explicit path to the `opencode` binary (Buddy also falls back to OpenCode Desktop's bundled `opencode-cli`).

---

## 4. Day-to-Day Workflow

*(Omit `--engine opencode` in the commands below if `default_engine` is `"opencode"` or `BUDDY_ENGINE=opencode` is set.)*

### Dispatch a New Task
```sh
buddy-ctl dispatch -p myapp -t feat-auth \
  -m "Add OAuth login with unit tests" \
  --engine opencode
```
- Creates a session scoped to `myapp`'s directory, enqueues the prompt, and records the `ses_...` ID in `~/.buddy/state/opencode.json`.
- The session appears immediately in **OpenCode Desktop**.
- Pass `--agent plan` (or `--agent <provider>/<model>`) to override the agent mode or model for a single dispatch.

### Watch Live Progress
```sh
buddy-ctl watch -p myapp -t feat-auth --engine opencode --interval 3 --timeout 600
```
Buddy probes structured server state (no terminal scraping):
- Active session → `running` (or `planning` when using the `plan` agent)
- Pending permission or interactive form → `waiting_for_input`
- Completed turn (`succeeded`) → `ready_for_demo`
- Failed/interrupted turn → `error`

### Inspect Output & Send Follow-Ups
```sh
# View formatted session header and recent messages
buddy-ctl capture -p myapp -t feat-auth --engine opencode

# Send a follow-up prompt
buddy-ctl send -p myapp -t feat-auth --text "Also add edge-case tests for expired tokens" --engine opencode

# Interrupt an actively running turn
buddy-ctl send -p myapp -t feat-auth --key ctrl-c --engine opencode

# Bring OpenCode Desktop to the foreground (macOS)
buddy-ctl focus -p myapp -t feat-auth --engine opencode
```

### Sync & Adopt Out-of-Band Desktop/CLI Sessions
Sessions created directly in OpenCode Desktop or via `opencode run` are automatically discovered when syncing:
```sh
buddy-ctl status --sync --format table --engine opencode
```
- Surfaces untracked sessions that are **currently active** or **updated within the last 24 hours** (capped at 10 per project, excluding child/subagent and archived sessions).
- Adopt any untracked session into Buddy tracking:
  ```sh
  buddy-ctl task adopt -p myapp -w <ses_id> -t my-task-slug --engine opencode
  ```

### Close & Clean Up (Non-Destructive)
```sh
# 1. Close: interrupts the session if actively running and marks it "closed" in Buddy state
buddy-ctl task close -p myapp -t feat-auth --engine opencode

# 2. Clean: removes the closed task from active state and logs an audit entry in task history
buddy-ctl task clean -p myapp -t feat-auth --reason "merged PR" --engine opencode
```
Closing and cleaning **never deletes** your conversation history in OpenCode Desktop, and cleaned session IDs are remembered so they will not reappear as untracked on future syncs.

---

## 5. Troubleshooting

- **`opencode background service is not running`**: Open **OpenCode Desktop** or run `opencode service start`, then verify with `opencode service status`.
- **`HTTP 401 Unauthorized`**: If you restarted OpenCode Desktop, Buddy automatically re-reads `~/.local/state/opencode/service.json` once on auth failure. If connecting to a custom URL via `BUDDY_OPENCODE_URL`, ensure `BUDDY_OPENCODE_PASSWORD` (or `OPENCODE_SERVER_PASSWORD`) is set.
- **Inspect engine resolution**: Run `buddy-ctl engine` to confirm which engine and signal source Buddy resolved.
