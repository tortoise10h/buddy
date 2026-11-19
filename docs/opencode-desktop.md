# Using Buddy with OpenCode Desktop

OpenCode v2 runs a **shared local background server** (`opencode serve --service`) that both **OpenCode Desktop** and the **`opencode` CLI** connect to. Buddy's `opencode` engine speaks directly to this shared server over HTTP/SSE, so tasks Buddy dispatches appear live in OpenCode Desktop—and sessions you start directly in OpenCode Desktop or `opencode` CLI can be inspected and adopted by Buddy.

> [!NOTE]
> **`opencode` engine vs. Rex with `opencode` TUI**
> - **OpenCode Desktop engine (`--engine opencode`)**: Controls OpenCode Desktop directly via its shared local HTTP/SSE server (state stored in `~/.buddy/state/opencode.json`). Auto-detected when running inside OpenCode Desktop.
> - **Terminal `opencode` (`--engine rex --agent opencode`)**: Runs the `opencode` TUI inside a Rex terminal tab (state stored in `~/.buddy/state/rex.json`).

---

## 1. Prerequisites

### 1a. Install OpenCode Desktop and Buddy
1. Install **OpenCode Desktop** (v2.2+ / v2.3+).
2. Clone and install **Buddy** using `install.sh` (pass `--build` to compile `buddy-ctl` from the latest repository source):
   ```sh
   ./install.sh          # Standard install
   ./install.sh --build  # Build buddy-ctl from the latest repo code
   ```

### 1b. Verify the Shared Local Service
OpenCode Desktop automatically starts the shared background server and writes its loopback URL and auth token to `~/.local/state/opencode/service.json`.

Verify it is running:
```sh
opencode service status
# Example output: http://127.0.0.1:49374
```
*(If OpenCode Desktop is closed, `buddy-ctl` automatically starts the shared background service via `opencode service start`, so OpenCode Desktop attaches to the same server when you open it.)*

---

## 2. Use Buddy in OpenCode Desktop

Once installed, you are ready to go—no extra configuration is required:

1. Open **OpenCode Desktop**.
2. Open the **`~/.buddy`** folder as a project and start a new session.
3. Just talk to Buddy as your orchestrator, for example:
   - *"Dispatch a task on `myapp` to add OAuth login with unit tests."*
   - *"How is the `myapp` auth task going?"*
   - *"Tell the `myapp` worker to also cover expired tokens."*
   - *"Close and clean up the `myapp` auth task."*

### What Happens Automatically
- **Auto-detection**: Inside OpenCode Desktop, `buddy-ctl` detects the OpenCode runtime environment (`OPENCODE`, `OPENCODE_SESSION_ID`, or `__CFBundleIdentifier=ai.opencode.desktop`) and selects the `opencode` engine automatically.
- **Shared sessions**: Worker sessions dispatched by Buddy run on the same local server, so you can inspect or jump into them directly in OpenCode Desktop (under **All sessions**, or under the project in the sidebar if you have added that project folder to your sidebar).
- **Non-destructive cleanup**: Closing or cleaning a task interrupts it if running and removes it from Buddy's active tracking (`~/.buddy/state/opencode.json`), while keeping the full conversation history intact in OpenCode Desktop.

### Optional: Pin a Default Agent or Model
By default, Buddy lets OpenCode use its own default agent and model. If you want Buddy-dispatched workers to use a specific agent mode or provider/model, set them once:
```sh
buddy-ctl config set engines.opencode.default_agent build
buddy-ctl config set engines.opencode.provider opencode-go
buddy-ctl config set engines.opencode.model mimo-v2.6-flash
```
When `provider` and `model` are configured, Buddy propagates `model: { providerID, id }` on both initial dispatches and follow-up messages.

---

## 3. Using `buddy-ctl` Directly (Optional)

You rarely need to run `buddy-ctl` by hand, but it is useful for scripting, debugging, or controlling OpenCode Desktop from an external terminal (pass `--engine opencode` or set `BUDDY_ENGINE=opencode` when running outside OpenCode Desktop):

```sh
# Check which engine Buddy resolves in your current environment
buddy-ctl engine

# Dispatch a task and watch live status transitions (running / planning / waiting_for_input / ready_for_demo / error)
buddy-ctl dispatch -p myapp -t feat-auth -m "Add OAuth login" --engine opencode
buddy-ctl watch -p myapp -t feat-auth --engine opencode

# Inspect messages, send a follow-up, or interrupt a running turn
buddy-ctl capture -p myapp -t feat-auth --engine opencode
buddy-ctl send -p myapp -t feat-auth --text "Add expired-token tests" --engine opencode
buddy-ctl send -p myapp -t feat-auth --key ctrl-c --engine opencode

# Sync out-of-band Desktop/CLI sessions (active or updated within 24h, max 10/project) and adopt one
buddy-ctl status --sync --format table --engine opencode
buddy-ctl task adopt -p myapp -w <ses_id> -t my-task --engine opencode

# Non-destructive close & clean
buddy-ctl task close -p myapp -t feat-auth --engine opencode
buddy-ctl task clean -p myapp -t feat-auth --reason "merged PR" --engine opencode
```

**Environment overrides**: `BUDDY_ENGINE=opencode`, `BUDDY_OPENCODE_URL` / `OPENCODE_SERVER_URL`, `BUDDY_OPENCODE_PASSWORD` / `OPENCODE_SERVER_PASSWORD`, `BUDDY_OPENCODE_BIN`.

---

## 4. Troubleshooting

- **`opencode background service is not running`**: Open **OpenCode Desktop** or run `opencode service start`, then verify with `opencode service status`.
- **`HTTP 401 Unauthorized`**: If OpenCode Desktop restarted on a new port/token, Buddy automatically re-reads `~/.local/state/opencode/service.json` once on auth failure. When using a custom server via `BUDDY_OPENCODE_URL`, make sure `BUDDY_OPENCODE_PASSWORD` (or `OPENCODE_SERVER_PASSWORD`) is set.
- **Dispatched session doesn't appear in the sidebar**: OpenCode Desktop shows newly created API sessions in **All sessions** until clicked (its Electron UI does not currently expose an API/deep-link to auto-open a tab). Adding the project folder to your **Projects** sidebar once lets new sessions refresh under that project automatically.
- **Buddy is picking the wrong engine**: Run `buddy-ctl engine` to see the detected host, signals, and resolution source (override with `--engine opencode` or `BUDDY_ENGINE=opencode` if needed).
