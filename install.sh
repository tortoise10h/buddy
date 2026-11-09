#!/usr/bin/env bash
# Buddy installation script
# Links Buddy skill and CLI into standard agent paths

set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SKILL_DIR="$REPO_DIR/skills/buddy"
BIN_SRC="$REPO_DIR/bin/buddy"
LOCAL_BIN="$HOME/.local/bin"

# Parse CLI options
BUILD_FROM_SOURCE=false
for arg in "${@:-}"; do
    case "$arg" in
        --build|-b)
            BUILD_FROM_SOURCE=true
            ;;
        --help|-h)
            echo "Usage: ./install.sh [options]"
            echo ""
            echo "Options:"
            echo "  --build, -b    Build buddy-ctl from source using Go (uses latest code on main)"
            echo "  --help, -h     Show this help message and exit"
            echo ""
            echo "Environment variables:"
            echo "  BUDDY_BUILD=1             Equivalent to --build"
            echo "  BUDDY_NONINTERACTIVE=1    Run without interactive prompts"
            echo "  BUDDY_PROJECTS_ROOTS=...  Colon-separated list of roots"
            echo "  BUDDY_ORCHESTRATOR_AGENT= Orchestrator agent harness (default: agy)"
            echo "  BUDDY_WORKER_AGENT=       Worker agent harness (default: agy)"
            echo "  BUDDY_WORKER_BACKEND=     Worker execution backend (rex | herdr | t3code, default: rex)"
            exit 0
            ;;
    esac
done

if [ "${BUDDY_BUILD:-0}" = "1" ]; then
    BUILD_FROM_SOURCE=true
fi

echo "=== Installing Buddy ==="

GITHUB_REPO="tortoise10h/buddy"

detect_platform() {
    local os arch
    os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    arch="$(uname -m)"

    case "$arch" in
        x86_64|amd64) arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *)
            echo "unsupported"
            return 1
            ;;
    esac

    case "$os" in
        darwin|linux) ;;
        *)
            echo "unsupported"
            return 1
            ;;
    esac

    echo "${os}-${arch}"
}

build_buddy_ctl() {
    local target="$1"
    if ! command -v go >/dev/null 2>&1; then
        return 1
    fi

    echo "Building buddy-ctl from source using Go..."
    local commit date version
    commit="$(git -C "$REPO_DIR" rev-parse --short HEAD 2>/dev/null || echo "unknown")"
    date="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
    version="v0.1.0-dev"

    mkdir -p "$(dirname "$target")"
    (
        cd "$REPO_DIR"
        CGO_ENABLED=0 go build -trimpath \
            -ldflags="-s -w -X 'github.com/tortoise10h/buddy/internal/cli.Version=${version}' -X 'github.com/tortoise10h/buddy/internal/cli.Commit=${commit}' -X 'github.com/tortoise10h/buddy/internal/cli.Date=${date}'" \
            -o "$target" ./cmd/buddy-ctl
    )
}

download_buddy_ctl() {
    local target="$1"
    local platform="$2"
    local url="https://github.com/${GITHUB_REPO}/releases/latest/download/buddy-ctl-${platform}"

    echo "Attempting to download pre-built buddy-ctl (${platform}) from GitHub Releases..."
    local tmp_bin
    tmp_bin="$(mktemp "${TMPDIR:-/tmp}/buddy-ctl.XXXXXX")"

    if command -v curl >/dev/null 2>&1; then
        if curl -fsSL -o "$tmp_bin" "$url" 2>/dev/null; then
            chmod +x "$tmp_bin"
            mkdir -p "$(dirname "$target")"
            mv "$tmp_bin" "$target"
            return 0
        fi
    elif command -v wget >/dev/null 2>&1; then
        if wget -q -O "$tmp_bin" "$url" 2>/dev/null; then
            chmod +x "$tmp_bin"
            mkdir -p "$(dirname "$target")"
            mv "$tmp_bin" "$target"
            return 0
        fi
    fi

    rm -f "$tmp_bin"
    return 1
}

# 1. Provision buddy-ctl executable
PLATFORM="$(detect_platform || echo "unknown")"
BUDDY_CTL_TARGET="$REPO_DIR/bin/buddy-ctl"

if [ "$BUILD_FROM_SOURCE" = true ]; then
    if ! build_buddy_ctl "$BUDDY_CTL_TARGET"; then
        echo ""
        echo "✖ Failed to build buddy-ctl from source. Go compiler (>= 1.22) is required."
        echo "  • macOS: brew install go"
        echo "  • Linux: https://go.dev/doc/install"
        exit 1
    fi
    echo "✔ Successfully built buddy-ctl from source"
else
    # Default path: try downloading pre-built binary
    DOWNLOADED=false
    if [ "$PLATFORM" != "unknown" ]; then
        if download_buddy_ctl "$BUDDY_CTL_TARGET" "$PLATFORM"; then
            DOWNLOADED=true
            echo "✔ Downloaded pre-built buddy-ctl (${PLATFORM})"
        fi
    fi

    if [ "$DOWNLOADED" = false ]; then
        # Fallback to local Go build if compiler is present
        if command -v go >/dev/null 2>&1; then
            echo "ℹ Pre-built release not found or unavailable; falling back to local Go build..."
            if build_buddy_ctl "$BUDDY_CTL_TARGET"; then
                echo "✔ Built buddy-ctl using local Go"
            else
                echo "✖ Failed to build buddy-ctl with local Go."
                exit 1
            fi
        elif [ -x "$BUDDY_CTL_TARGET" ]; then
            echo "✔ Using existing compiled binary at $BUDDY_CTL_TARGET"
        else
            echo ""
            echo "✖ Pre-built binary could not be downloaded and Go is not installed on this system."
            echo ""
            echo "To install Go and build from source:"
            echo "  • macOS: brew install go"
            echo "  • Linux: https://go.dev/doc/install"
            echo ""
            echo "Once installed, re-run: ./install.sh --build"
            exit 1
        fi
    fi
fi

# 2. Ensure scripts are executable and install binaries
chmod +x "$REPO_DIR/bin/buddy"
chmod +x "$BUDDY_CTL_TARGET"

mkdir -p "$LOCAL_BIN"
ln -sf "$BIN_SRC" "$LOCAL_BIN/buddy"

# Remove any old symlink or file before installing standalone binary
rm -f "$LOCAL_BIN/buddy-ctl"
cp "$BUDDY_CTL_TARGET" "$LOCAL_BIN/buddy-ctl"
chmod +x "$LOCAL_BIN/buddy-ctl"

echo "✔ Symlinked ~/.local/bin/buddy -> $BIN_SRC"
echo "✔ Installed ~/.local/bin/buddy-ctl"

# 3. Symlink skills/buddy into agent directories
SKILL_TARGETS=(
    "$HOME/.agents/skills/buddy"
    "$HOME/.gemini/config/skills/buddy"
    "$HOME/.gemini/antigravity-cli/skills/buddy"
)

# Optional agent targets if parent directory exists
if [ -d "$HOME/.pi/agent/skills" ]; then
    SKILL_TARGETS+=("$HOME/.pi/agent/skills/buddy")
fi

if [ -d "$HOME/.config/opencode/skills" ]; then
    SKILL_TARGETS+=("$HOME/.config/opencode/skills/buddy")
fi

for target in "${SKILL_TARGETS[@]}"; do
    mkdir -p "$(dirname "$target")"
    rm -rf "$target"
    ln -sf "$SKILL_DIR" "$target"
    echo "✔ Linked skill -> $target"
done

# 4. Project directories configuration
echo "Configuring project roots..."

CONFIG_FILE="$HOME/.buddy/config.json"
EXISTING_ROOTS=()
if [ -f "$CONFIG_FILE" ]; then
    EXISTING_RAW=$(python3 -c "
import json
try:
    with open('$CONFIG_FILE', 'r', encoding='utf-8') as f:
        cfg = json.load(f)
    roots = cfg.get('projects_roots', [])
    for r in roots:
        r_str = str(r).strip()
        if r_str:
            print(r_str)
except Exception:
    pass
" 2>/dev/null || true)
    while IFS= read -r line; do
        if [ -n "$line" ]; then
            EXISTING_ROOTS+=("$line")
        fi
    done <<< "$EXISTING_RAW"
fi

DEFAULT_SUGGESTION="~/Dev/Personal/Projects"
if [ ${#EXISTING_ROOTS[@]} -gt 0 ]; then
    FORMATTED_EXISTING=()
    for r in "${EXISTING_ROOTS[@]}"; do
        if [[ "$r" == "$HOME"* ]]; then
            FORMATTED_EXISTING+=("~${r#$HOME}")
        else
            FORMATTED_EXISTING+=("$r")
        fi
    done
    DEFAULT_SUGGESTION=$(IFS=', '; echo "${FORMATTED_EXISTING[*]}")
    echo "✔ Found existing configured project roots: $DEFAULT_SUGGESTION"
fi

expand_path() {
    local p="$1"
    p="$(echo "$p" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
    p="${p/#\~/$HOME}"
    echo "$p"
}

COLLECTED_ROOTS=()

add_to_collected() {
    local candidate="$1"
    if [ ${#COLLECTED_ROOTS[@]} -gt 0 ]; then
        for existing in "${COLLECTED_ROOTS[@]}"; do
            if [ "$existing" = "$candidate" ]; then
                return 0
            fi
        done
    fi
    COLLECTED_ROOTS+=("$candidate")
}

IS_NONINTERACTIVE=0
if [ -n "${BUDDY_NONINTERACTIVE:-}" ] || [ ! -t 0 ]; then
    IS_NONINTERACTIVE=1
fi

if [ "$IS_NONINTERACTIVE" -eq 1 ]; then
    if [ -n "${BUDDY_PROJECTS_ROOTS:-}" ]; then
        IFS=':,' read -ra RAW_PARTS <<< "$BUDDY_PROJECTS_ROOTS"
        for part in "${RAW_PARTS[@]}"; do
            exp=$(expand_path "$part")
            if [ -n "$exp" ]; then
                mkdir -p "$exp" 2>/dev/null || true
                add_to_collected "$exp"
            fi
        done
    elif [ ${#EXISTING_ROOTS[@]} -gt 0 ]; then
        for r in "${EXISTING_ROOTS[@]}"; do
            exp=$(expand_path "$r")
            if [ -n "$exp" ]; then
                add_to_collected "$exp"
            fi
        done
    else
        DEFAULT_PATH=$(expand_path "$DEFAULT_SUGGESTION")
        mkdir -p "$DEFAULT_PATH" 2>/dev/null || true
        add_to_collected "$DEFAULT_PATH"
    fi
else
    echo "Where do you keep your project repositories?"
    echo "(You can enter multiple directories separated by commas or spaces)"

    validate_and_add_dir() {
        local target="$1"
        if [ -z "$target" ]; then
            return 1
        fi
        if [ -d "$target" ]; then
            add_to_collected "$target"
            return 0
        fi

        echo "Directory '$target' does not exist."
        while true; do
            read -r -p "Create directory '$target'? [Y/n]: " create_yn
            create_yn="${create_yn:-y}"
            case "$create_yn" in
                [yY]|[yY][eE][sS])
                    if mkdir -p "$target" 2>/dev/null; then
                        echo "✔ Created directory: $target"
                        add_to_collected "$target"
                        return 0
                    else
                        echo "✖ Could not create '$target'. Please enter a valid path."
                        return 1
                    fi
                    ;;
                [nN]|[nN][oO])
                    echo "Skipping '$target'."
                    return 1
                    ;;
                *)
                    echo "Please answer 'y' or 'n'."
                    ;;
            esac
        done
    }

    while [ ${#COLLECTED_ROOTS[@]} -eq 0 ]; do
        read -r -p "Project directory(ies) [$DEFAULT_SUGGESTION]: " input_roots
        input_roots="${input_roots:-$DEFAULT_SUGGESTION}"

        input_items=()
        if [[ "$input_roots" == *","* ]]; then
            IFS=',' read -ra input_items <<< "$input_roots"
        else
            read -ra input_items <<< "$input_roots"
        fi

        for raw_item in "${input_items[@]}"; do
            clean_item=$(expand_path "$raw_item")
            if [ -n "$clean_item" ]; then
                validate_and_add_dir "$clean_item" || true
            fi
        done

        if [ ${#COLLECTED_ROOTS[@]} -eq 0 ]; then
            echo "No valid project roots specified. Please provide at least one directory."
        fi
    done

    while true; do
        read -r -p "Add another project directory? [y/N]: " add_another
        add_another="${add_another:-n}"
        case "$add_another" in
            [yY]|[yY][eE][sS])
                read -r -p "Additional project directory: " extra_input
                clean_extra=$(expand_path "$extra_input")
                if [ -n "$clean_extra" ]; then
                    if validate_and_add_dir "$clean_extra"; then
                        echo "✔ Added: $clean_extra"
                    fi
                fi
                ;;
            *)
                break
                ;;
        esac
    done
fi

echo "✔ Project roots configured:"
for r in "${COLLECTED_ROOTS[@]}"; do
    echo "  • $r"
done

# 5. Agent selection configuration
echo ""
echo "Configuring Buddy agents..."

# Harness candidates, filtered to those actually installed on this machine.
# Keep in sync with DEFAULT_CONFIG["agents"] in skills/buddy/scripts/buddy-ctl.
HARNESS_CANDIDATES=("agy" "pi" "opencode" "claude" "codex" "cursor" "gemini")

AVAILABLE_AGENTS=()
for h in "${HARNESS_CANDIDATES[@]}"; do
    if command -v "$h" >/dev/null 2>&1; then
        AVAILABLE_AGENTS+=("$h")
    fi
done
if [ ${#AVAILABLE_AGENTS[@]} -eq 0 ]; then
    AVAILABLE_AGENTS=("${HARNESS_CANDIDATES[@]}")
fi

# Detect existing configured agents to preserve choices on re-install
EXISTING_ORCH=""
EXISTING_WORKER=""
if [ -f "$CONFIG_FILE" ]; then
    EXISTING_AGENTS_RAW=$(python3 -c "
import json
try:
    with open('$CONFIG_FILE', 'r', encoding='utf-8') as f:
        cfg = json.load(f)
    legacy = cfg.get('default_agent')
    orch = cfg.get('orchestrator_agent') or legacy or ''
    worker = cfg.get('worker_agent') or legacy or ''
    print(f'{orch}\t{worker}')
except Exception:
    pass
" 2>/dev/null || true)
    if [ -n "$EXISTING_AGENTS_RAW" ]; then
        EXISTING_ORCH="$(echo "$EXISTING_AGENTS_RAW" | cut -f1)"
        EXISTING_WORKER="$(echo "$EXISTING_AGENTS_RAW" | cut -f2)"
    fi
fi

DEFAULT_ORCH="${EXISTING_ORCH:-agy}"
DEFAULT_WORKER="${EXISTING_WORKER:-agy}"

is_valid_agent() {
    local target="$1"
    for a in "${AVAILABLE_AGENTS[@]}"; do
        if [ "$a" = "$target" ]; then
            return 0
        fi
    done
    if [ -n "$EXISTING_ORCH" ] && [ "$target" = "$EXISTING_ORCH" ]; then
        return 0
    fi
    if [ -n "$EXISTING_WORKER" ] && [ "$target" = "$EXISTING_WORKER" ]; then
        return 0
    fi
    return 1
}

CHOSEN_ORCH=""
CHOSEN_WORKER=""

if [ "$IS_NONINTERACTIVE" -eq 1 ]; then
    if [ -n "${BUDDY_ORCHESTRATOR_AGENT:-}" ]; then
        CHOSEN_ORCH="$BUDDY_ORCHESTRATOR_AGENT"
    else
        CHOSEN_ORCH="$DEFAULT_ORCH"
    fi

    if [ -n "${BUDDY_WORKER_AGENT:-}" ]; then
        CHOSEN_WORKER="$BUDDY_WORKER_AGENT"
    else
        CHOSEN_WORKER="$DEFAULT_WORKER"
    fi
else
    echo "Available agent harnesses:"
    for a in "${AVAILABLE_AGENTS[@]}"; do
        echo "  • $a"
    done
    echo ""

    # Orchestrator prompt
    while true; do
        read -r -p "Orchestrator harness (runs Buddy control tower) [$DEFAULT_ORCH]: " input_orch
        input_orch="${input_orch:-$DEFAULT_ORCH}"
        input_orch="$(echo "$input_orch" | tr -d '[:space:]')"
        if is_valid_agent "$input_orch"; then
            CHOSEN_ORCH="$input_orch"
            break
        fi
        echo "✖ Unknown agent harness '$input_orch'. Please choose from: $(IFS=', '; echo "${AVAILABLE_AGENTS[*]}")"
    done

    # Worker prompt
    while true; do
        read -r -p "Worker harness (default for new task tabs) [$DEFAULT_WORKER]: " input_worker
        input_worker="${input_worker:-$DEFAULT_WORKER}"
        input_worker="$(echo "$input_worker" | tr -d '[:space:]')"
        if is_valid_agent "$input_worker"; then
            CHOSEN_WORKER="$input_worker"
            break
        fi
        echo "✖ Unknown agent harness '$input_worker'. Please choose from: $(IFS=', '; echo "${AVAILABLE_AGENTS[*]}")"
    done
fi

echo "✔ Agents configured:"
echo "  • Orchestrator: $CHOSEN_ORCH"
echo "  • Worker: $CHOSEN_WORKER"

# 6. Worker backend selection configuration
echo ""
echo "Configuring worker execution backend..."

EXISTING_BACKEND=""
if [ -f "$CONFIG_FILE" ]; then
    EXISTING_BACKEND=$(python3 -c "
import json
try:
    with open('$CONFIG_FILE', 'r', encoding='utf-8') as f:
        cfg = json.load(f)
    print(cfg.get('worker_backend') or cfg.get('default_engine') or '')
except Exception:
    pass
" 2>/dev/null || true)
fi

DEFAULT_BACKEND="${EXISTING_BACKEND:-rex}"
VALID_BACKENDS=("rex" "herdr" "t3code")

is_valid_backend() {
    local target="$1"
    for b in "${VALID_BACKENDS[@]}"; do
        if [ "$b" = "$target" ]; then
            return 0
        fi
    done
    return 1
}

CHOSEN_WORKER_BACKEND=""
if [ "$IS_NONINTERACTIVE" -eq 1 ]; then
    if [ -n "${BUDDY_WORKER_BACKEND:-}" ]; then
        CHOSEN_WORKER_BACKEND="$BUDDY_WORKER_BACKEND"
    else
        CHOSEN_WORKER_BACKEND="$DEFAULT_BACKEND"
    fi
else
    echo "Available worker execution backends:"
    echo "  • rex    - Rex terminal multiplexer (PTY panes, default)"
    echo "  • herdr  - Herdr terminal multiplexer (requires HERDR_ENV=1)"
    echo "  • t3code - T3 Code autonomous threads (via localhost HTTP/MCP)"
    echo ""

    while true; do
        read -r -p "Worker backend [$DEFAULT_BACKEND]: " input_backend
        input_backend="${input_backend:-$DEFAULT_BACKEND}"
        input_backend="$(echo "$input_backend" | tr -d '[:space:]')"
        if is_valid_backend "$input_backend"; then
            CHOSEN_WORKER_BACKEND="$input_backend"
            break
        fi
        echo "✖ Unknown backend '$input_backend'. Please choose from: rex, herdr, t3code"
    done
fi

echo "✔ Worker backend configured: $CHOSEN_WORKER_BACKEND"

if [ "$CHOSEN_WORKER_BACKEND" = "t3code" ]; then
    # Verify t3 CLI presence
    T3_FOUND=false
    if command -v t3 >/dev/null 2>&1; then
        T3_FOUND=true
    elif [ -x "$HOME/.local/bin/t3" ] || [ -x "/usr/local/bin/t3" ]; then
        T3_FOUND=true
    fi

    if [ "$T3_FOUND" = false ]; then
        echo "ℹ Note: 't3' binary was not found on PATH or at ~/.local/bin/t3."
        echo "  Ensure T3 Code desktop app or CLI is installed to run t3code tasks."
    fi
    echo "👉 Important: Run 'buddy-ctl t3 auth login' to pair Buddy with T3 Code."
fi

# 7. Initialize ~/.buddy Control Tower & symlink workspace rules
echo "Initializing ~/.buddy Control Tower..."
mkdir -p "$HOME/.buddy/.agents"
ln -sf "$REPO_DIR/AGENTS.md" "$HOME/.buddy/AGENTS.md"
ln -sf "$REPO_DIR/.agents/hooks.json" "$HOME/.buddy/.agents/hooks.json"
echo "✔ Symlinked ~/.buddy/AGENTS.md -> $REPO_DIR/AGENTS.md"
echo "✔ Symlinked ~/.buddy/.agents/hooks.json -> $REPO_DIR/.agents/hooks.json"

INIT_ARGS=()
for r in "${COLLECTED_ROOTS[@]}"; do
    INIT_ARGS+=(--projects-root "$r")
done
INIT_ARGS+=(--orchestrator-agent "$CHOSEN_ORCH" --worker-agent "$CHOSEN_WORKER" --worker-backend "$CHOSEN_WORKER_BACKEND")

"$LOCAL_BIN/buddy-ctl" init "${INIT_ARGS[@]}"

echo "=== Buddy installation complete! ==="
echo "You can now run 'buddy' from anywhere or use 'buddy-ctl' commands."
