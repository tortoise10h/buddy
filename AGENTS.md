# Buddy Repository Development Guide

> [!IMPORTANT]
> **This session develops Buddy itself.**
> The Control Tower orchestrator instructions and dispatch rules in `~/.buddy/AGENTS.md` (such as the Zero-Tolerance Direct Editing Policy / Rule #1) do **NOT** apply here.
> In this repository, you are expected to create, modify, test, and edit code, documentation, and configuration files directly.

---

## Project Layout

- `cmd/buddy-ctl/`: Go entry point (`main.go`) invoking `cli.Execute()`.
- `internal/backend/`: Terminal multiplexer abstraction (`TerminalBackend` interface) and backend implementations (`rex`, `wezterm`, `t3`).
- `internal/cli/`: Cobra CLI commands (`root.go`, `dispatch.go`, `task.go`, `status.go`, `demo.go`, `init.go`, `batch.go`, etc.).
- `internal/config/`: Configuration manager for `~/.buddy/config.json` (project roots, agent harnesses).
- `internal/engine/`: Task lifecycle engine (dispatch, reactive TUI probing, prompt injection, verification).
- `internal/notify/`: Notification manager (macOS desktop alerts via AppleScript, Glass sound).
- `internal/project/`: Project scanner and repo resolver across configured roots.
- `internal/state/`: Local O(1) state cache (`~/.buddy/state.json`) and advisory lock (`.state.lock`).
- `skills/buddy/`: Antigravity skill package (`SKILL.md` and helper scripts in `scripts/`).
- `templates/`: Provisioning templates such as `orchestrator-AGENTS.md` provisioned to `~/.buddy/AGENTS.md`.
- `.agents/hooks.json`: PreInvocation hook configuration running `buddy-ctl context-inject`.
- `install.sh`: Idempotent installer and migrator.
- `Makefile`: Build, test, lint, and install automation.
- `plans/`: Architectural designs and feature proposals.

---

## Build, Test, Lint, and Run

All standard developer tasks are defined in the `Makefile`:

- **Build binary**: `make` or `make build` (compiles `cmd/buddy-ctl` to `bin/buddy-ctl` with version ldflags).
- **Run tests**: `make test` (executes `go test -v ./...`).
- **Lint / Vet**: `make vet` (runs `go vet ./...`).
- **Install locally**: `make install` (installs `bin/buddy-ctl` to `PREFIX` / `~/.local/bin/buddy-ctl`).
- **Clean artifacts**: `make clean` (removes `bin/buddy-ctl` and `dist/`).
- **Run locally**: `./bin/buddy-ctl <subcommand> [flags]`

---

## Code Extension Conventions

### 1. Adding a new `buddy-ctl` subcommand
- Add a file in `internal/cli/<subcommand>.go`.
- Define a constructor `new<Subcommand>Cmd() *cobra.Command` with flags, JSON/brief formatting, and robust error handling.
- Register the command in `init()` within `internal/cli/root.go` via `rootCmd.AddCommand(new<Subcommand>Cmd())`.
- Add unit tests in `internal/cli/<subcommand>_test.go`.

### 2. Adding a new terminal backend
- Create a subpackage under `internal/backend/<name>/`.
- Implement the `backend.TerminalBackend` interface (`Name()`, `IsAvailable()`, `EnsureSession()`, `ListSessions()`, `ListWindows()`, `InspectWindow()`, `CreateTaskWindow()`, `Close()`, `Focus()`, `Capture()`, `Send()`, `OpenDevSplit()`, `IsBlockAlive()`).
- Keep backends as pure multiplexer primitives with zero agent or prompt logic.
- Register and wire into backend detection in `internal/engine/` and CLI commands.

---

## Runtime Files & Test Isolation

- Runtime files in `~/.buddy/`:
  - `config.json`: user settings (project roots, agent harnesses).
  - `state.json`: O(1) state cache of projects, task tabs, and block IDs.
  - `.state.lock`: flock-based advisory lock ensuring safe atomic updates.
- **Test Isolation Rule**: Tests must **NEVER** mutate a real user's `~/.buddy` or `$HOME`.
  - Always override the state directory in tests using `t.Setenv("BUDDY_DIR", t.TempDir())` or an isolated temporary `HOME`.

---

## Shell Script Conventions

For `install.sh` and scripts in `skills/buddy/scripts`:
- Use `#!/usr/bin/env bash` with `set -euo pipefail`.
- Maintain strict compatibility with macOS default Bash 3.2 (no `declare -A` associative arrays, avoid Bash 4+ parameter expansions).
- Keep operations idempotent and provide clean CLI feedback with standard prefixes (`✔`, `✖`, `ℹ`).

---

## Commit, PR & Documentation Conventions

- Follow Conventional Commits format (`feat:`, `fix:`, `refactor:`, `docs:`, `chore:`).
- When changing CLI commands, options, or behaviors:
  - Update `README.md` and Cobra command help text / usage strings.
  - Keep documentation in sync.

---

## Architecture References

- Feature designs and historical specifications: see [`plans/`](plans/).
- High-level overview and installation: see [`README.md`](README.md).
