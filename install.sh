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
            echo "  BUDDY_ORCHESTRATOR_AGENT= Terminal orchestrator harness (default: agy)"
            echo "  BUDDY_WORKER_AGENT=       Terminal worker harness (default: agy)"
            echo "  BUDDY_DEFAULT_ENGINE=     Fallback engine when host is unknown (rex, wezterm, t3)"
            echo "  BUDDY_T3_PROVIDER=        T3 provider (when t3 is installed)"
            echo "  BUDDY_T3_MODEL=           T3 model or <provider>/<model> (when t3 is installed)"
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
        if [ "$r" = "$HOME" ]; then
            FORMATTED_EXISTING+=("~")
        elif [[ "$r" == "$HOME/"* ]]; then
            FORMATTED_EXISTING+=("~${r#$HOME}")
        else
            FORMATTED_EXISTING+=("$r")
        fi
    done
    DEFAULT_SUGGESTION=""
    for r in "${FORMATTED_EXISTING[@]}"; do
        if [ -z "$DEFAULT_SUGGESTION" ]; then
            DEFAULT_SUGGESTION="$r"
        else
            DEFAULT_SUGGESTION="$DEFAULT_SUGGESTION, $r"
        fi
    done
    echo "✔ Found existing configured project roots: $DEFAULT_SUGGESTION"
fi

expand_path() {
    local p="$1"
    p="$(echo "$p" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
    if [ "$p" = "~" ]; then
        p="$HOME"
    elif [[ "$p" == "~/"* ]]; then
        p="$HOME/${p#"~/"}"
    fi
    echo "$p"
}

parse_root_inputs() {
    python3 -c '
import re, shlex, sys

raw = sys.argv[1] if len(sys.argv) > 1 else ""

def split_outside_quotes(s):
    parts = []
    cur = []
    quote = None
    escaped = False
    for ch in s:
        if escaped:
            cur.append(ch)
            escaped = False
        elif ch == "\\" and quote != "\x27":
            cur.append(ch)
            escaped = True
        elif quote:
            if ch == quote:
                quote = None
            cur.append(ch)
        elif ch in ("\"", "\x27"):
            quote = ch
            cur.append(ch)
        elif ch == ",":
            parts.append("".join(cur))
            cur = []
        else:
            cur.append(ch)
    parts.append("".join(cur))
    return parts

for seg in split_outside_quotes(raw):
    seg = seg.strip()
    if not seg:
        continue
    if any(c in seg for c in ("\"", "\x27", "\\")):
        try:
            tokens = shlex.split(seg)
        except ValueError:
            tokens = [seg]
    else:
        tokens = re.split(r"\s+(?=(?:~(?:/|$)|/|\.\.?/))", seg)
    for tok in tokens:
        tok = tok.strip()
        if tok:
            print(tok)
' "$1"
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

if [ ${#EXISTING_ROOTS[@]} -gt 0 ]; then
    for r in "${EXISTING_ROOTS[@]}"; do
        exp=$(expand_path "$r")
        if [ -n "$exp" ]; then
            add_to_collected "$exp"
        fi
    done
fi

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
    elif [ ${#COLLECTED_ROOTS[@]} -eq 0 ]; then
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

    process_root_input() {
        local raw_input="$1"
        local parsed_items=()
        local item clean_item
        while IFS= read -r item; do
            if [ -n "$item" ]; then
                parsed_items+=("$item")
            fi
        done <<< "$(parse_root_inputs "$raw_input")"
        if [ ${#parsed_items[@]} -gt 0 ]; then
            for item in "${parsed_items[@]}"; do
                clean_item=$(expand_path "$item")
                if [ -n "$clean_item" ]; then
                    validate_and_add_dir "$clean_item" || true
                fi
            done
        fi
    }

    if [ ${#EXISTING_ROOTS[@]} -gt 0 ]; then
        read -r -p "Additional project directory(ies) (Enter to keep existing): " input_roots
        if [ -n "$input_roots" ]; then
            process_root_input "$input_roots"
        fi
    fi

    while [ ${#COLLECTED_ROOTS[@]} -eq 0 ]; do
        read -r -p "Project directory(ies) [$DEFAULT_SUGGESTION]: " input_roots
        input_roots="${input_roots:-$DEFAULT_SUGGESTION}"
        process_root_input "$input_roots"

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
                if [ -n "$extra_input" ]; then
                    process_root_input "$extra_input"
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
# Keep in sync with DefaultConfig() in internal/config/config.go.
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

# Detect existing configured agents and engines to preserve choices on re-install
EXISTING_ORCH=""
EXISTING_WORKER=""
EXISTING_ENGINE=""
EXISTING_T3_PROVIDER=""
EXISTING_T3_MODEL=""
if [ -f "$CONFIG_FILE" ]; then
    EXISTING_CFG_RAW=$(python3 -c "
import json
try:
    with open('$CONFIG_FILE', 'r', encoding='utf-8') as f:
        cfg = json.load(f)
    engines = cfg.get('engines') or {}
    rex = engines.get('rex') or {}
    t3 = engines.get('t3') or {}
    orch = rex.get('orchestrator_agent') or ''
    worker = rex.get('worker_agent') or ''
    def_eng = cfg.get('default_engine') or ''
    t3_prov = t3.get('provider') or ''
    t3_mod = t3.get('model') or ''
    print(f'{orch}\t{worker}\t{def_eng}\t{t3_prov}\t{t3_mod}')
except Exception:
    pass
" 2>/dev/null || true)
    if [ -n "$EXISTING_CFG_RAW" ]; then
        EXISTING_ORCH="$(echo "$EXISTING_CFG_RAW" | cut -f1)"
        EXISTING_WORKER="$(echo "$EXISTING_CFG_RAW" | cut -f2)"
        EXISTING_ENGINE="$(echo "$EXISTING_CFG_RAW" | cut -f3)"
        EXISTING_T3_PROVIDER="$(echo "$EXISTING_CFG_RAW" | cut -f4)"
        EXISTING_T3_MODEL="$(echo "$EXISTING_CFG_RAW" | cut -f5)"
    fi
fi

HAS_REX=0
if command -v rex >/dev/null 2>&1; then
    HAS_REX=1
fi
HAS_T3=0
if command -v t3 >/dev/null 2>&1 || [ -x "$HOME/.local/bin/t3" ]; then
    HAS_T3=1
fi

DEFAULT_ENGINE="$EXISTING_ENGINE"
if [ -z "$DEFAULT_ENGINE" ]; then
    if [ "$HAS_REX" -eq 1 ]; then
        DEFAULT_ENGINE="rex"
    elif [ "$HAS_T3" -eq 1 ]; then
        DEFAULT_ENGINE="t3"
    else
        DEFAULT_ENGINE="rex"
    fi
fi

CHOSEN_ENGINE="${BUDDY_DEFAULT_ENGINE:-$DEFAULT_ENGINE}"
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
echo "  • Default Engine: $CHOSEN_ENGINE"
echo "  • Terminal Orchestrator: $CHOSEN_ORCH"
echo "  • Terminal Worker: $CHOSEN_WORKER"

# Configure T3 provider/model when T3 CLI is installed or env vars are provided
CHOSEN_T3_PROVIDER="$EXISTING_T3_PROVIDER"
CHOSEN_T3_MODEL="$EXISTING_T3_MODEL"

if [ "$HAS_T3" -eq 1 ] || [ -n "${BUDDY_T3_MODEL:-}" ] || [ -n "${BUDDY_T3_PROVIDER:-}" ]; then
    if [ "$IS_NONINTERACTIVE" -eq 1 ]; then
        if [ -n "${BUDDY_T3_PROVIDER:-}" ]; then
            CHOSEN_T3_PROVIDER="$BUDDY_T3_PROVIDER"
            if [ -n "${BUDDY_T3_MODEL:-}" ]; then
                CHOSEN_T3_MODEL="$BUDDY_T3_MODEL"
            fi
        elif [ -n "${BUDDY_T3_MODEL:-}" ]; then
            if [[ "$BUDDY_T3_MODEL" == *"/"* ]]; then
                CHOSEN_T3_PROVIDER="${BUDDY_T3_MODEL%%/*}"
                CHOSEN_T3_MODEL="${BUDDY_T3_MODEL#*/}"
            else
                CHOSEN_T3_MODEL="$BUDDY_T3_MODEL"
            fi
        fi
    else
        echo ""
        echo "T3 Code CLI detected."
        T3_DEFAULT_SPEC=""
        if [ -n "$EXISTING_T3_PROVIDER" ] && [ -n "$EXISTING_T3_MODEL" ]; then
            T3_DEFAULT_SPEC="${EXISTING_T3_PROVIDER}/${EXISTING_T3_MODEL}"
        fi
        while true; do
            if [ -n "$T3_DEFAULT_SPEC" ]; then
                read -r -p "T3 worker model (<provider>/<model>) [$T3_DEFAULT_SPEC]: " input_t3
                input_t3="${input_t3:-$T3_DEFAULT_SPEC}"
            else
                read -r -p "T3 worker model (<provider>/<model>, e.g. antigravity/gemini-3.8-flash-medium): " input_t3
            fi
            input_t3="$(echo "$input_t3" | tr -d '[:space:]')"
            if [[ "$input_t3" == *"/"* ]]; then
                p_part="${input_t3%%/*}"
                m_part="${input_t3#*/}"
                if [ -n "$p_part" ] && [ -n "$m_part" ]; then
                    CHOSEN_T3_PROVIDER="$p_part"
                    CHOSEN_T3_MODEL="$m_part"
                    break
                fi
            fi
            echo "✖ Please enter both provider and model separated by '/' (e.g. antigravity/gemini-3.8-flash-medium)."
        done
    fi

    if [ -n "$CHOSEN_T3_PROVIDER" ] && [ -n "$CHOSEN_T3_MODEL" ]; then
        echo "  • T3 Worker: ${CHOSEN_T3_PROVIDER}/${CHOSEN_T3_MODEL}"
    fi
fi

# 6. Initialize ~/.buddy Control Tower & symlink workspace rules
echo "Initializing ~/.buddy Control Tower..."
mkdir -p "$HOME/.buddy/.agents"
ORCHESTRATOR_TEMPLATE="$REPO_DIR/templates/orchestrator-AGENTS.md"
DEST_AGENTS="$HOME/.buddy/AGENTS.md"
# Migration path for pre-split installs where ~/.buddy/AGENTS.md was symlinked to $REPO_DIR/AGENTS.md.
# TODO(2026-11-09): Remove OLD_AGENTS / canonical_old migration check after ~1 week once active users have upgraded.
OLD_AGENTS="$REPO_DIR/AGENTS.md"

if [ -L "$DEST_AGENTS" ]; then
    current_target="$(readlink "$DEST_AGENTS" 2>/dev/null || true)"
    canonical_repo="$(cd "$REPO_DIR" 2>/dev/null && pwd -P || echo "$REPO_DIR")"
    canonical_template="$(cd "$REPO_DIR/templates" 2>/dev/null && pwd -P || echo "$REPO_DIR/templates")/orchestrator-AGENTS.md"
    canonical_old="$canonical_repo/AGENTS.md"

    if [ "$current_target" = "$OLD_AGENTS" ] || [ "$current_target" = "$canonical_old" ]; then
        rm -f "$DEST_AGENTS"
    elif [ "$current_target" = "$ORCHESTRATOR_TEMPLATE" ] || [ "$current_target" = "$canonical_template" ]; then
        rm -f "$DEST_AGENTS"
    else
        backup="${DEST_AGENTS}.bak-$(date +%Y%m%d%H%M%S)"
        mv "$DEST_AGENTS" "$backup"
        echo "ℹ Backed up existing ~/.buddy/AGENTS.md -> $backup"
    fi
elif [ -e "$DEST_AGENTS" ]; then
    backup="${DEST_AGENTS}.bak-$(date +%Y%m%d%H%M%S)"
    mv "$DEST_AGENTS" "$backup"
    echo "ℹ Backed up existing ~/.buddy/AGENTS.md -> $backup"
fi

ln -sf "$ORCHESTRATOR_TEMPLATE" "$DEST_AGENTS"
echo "✔ Symlinked ~/.buddy/AGENTS.md -> $ORCHESTRATOR_TEMPLATE"

HOOKS_TEMPLATE="$REPO_DIR/templates/hooks.json"
DEST_HOOKS="$HOME/.buddy/.agents/hooks.json"
OLD_HOOKS="$REPO_DIR/.agents/hooks.json"

tmp_rendered="$(mktemp "${TMPDIR:-/tmp}/buddy-hooks.XXXXXX")"
sed "s|__BUDDY_CTL__|$LOCAL_BIN/buddy-ctl|g" "$HOOKS_TEMPLATE" > "$tmp_rendered"
chmod 644 "$tmp_rendered"

if [ -L "$DEST_HOOKS" ]; then
    current_target="$(readlink "$DEST_HOOKS" 2>/dev/null || true)"
    canonical_repo="$(cd "$REPO_DIR" 2>/dev/null && pwd -P || echo "$REPO_DIR")"
    canonical_old="$canonical_repo/.agents/hooks.json"

    if [ "$current_target" = "$OLD_HOOKS" ] || [ "$current_target" = "$canonical_old" ] || [ ! -e "$DEST_HOOKS" ]; then
        rm -f "$DEST_HOOKS"
    else
        backup="${DEST_HOOKS}.bak-$(date +%Y%m%d%H%M%S)"
        mv "$DEST_HOOKS" "$backup"
        echo "ℹ Backed up existing ~/.buddy/.agents/hooks.json -> $backup"
    fi
elif [ -f "$DEST_HOOKS" ]; then
    if ! cmp -s "$tmp_rendered" "$DEST_HOOKS"; then
        backup="${DEST_HOOKS}.bak-$(date +%Y%m%d%H%M%S)"
        mv "$DEST_HOOKS" "$backup"
        echo "ℹ Backed up existing ~/.buddy/.agents/hooks.json -> $backup"
    fi
elif [ -e "$DEST_HOOKS" ]; then
    backup="${DEST_HOOKS}.bak-$(date +%Y%m%d%H%M%S)"
    mv "$DEST_HOOKS" "$backup"
    echo "ℹ Backed up existing ~/.buddy/.agents/hooks.json -> $backup"
fi

mv "$tmp_rendered" "$DEST_HOOKS"
echo "✔ Generated ~/.buddy/.agents/hooks.json"

INIT_ARGS=()
for r in "${COLLECTED_ROOTS[@]}"; do
    INIT_ARGS+=(--projects-root "$r")
done
INIT_ARGS+=(--default-engine "$CHOSEN_ENGINE" --orchestrator-agent "$CHOSEN_ORCH" --worker-agent "$CHOSEN_WORKER")
if [ -n "$CHOSEN_T3_PROVIDER" ] && [ -n "$CHOSEN_T3_MODEL" ]; then
    INIT_ARGS+=(--t3-provider "$CHOSEN_T3_PROVIDER" --t3-model "$CHOSEN_T3_MODEL")
fi

"$LOCAL_BIN/buddy-ctl" init "${INIT_ARGS[@]}"

echo "=== Buddy installation complete! ==="
echo "You can now run 'buddy' from anywhere or use 'buddy-ctl' commands."
