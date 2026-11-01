#!/usr/bin/env bash
# Buddy installation script
# Links Buddy skill and CLI into standard agent paths

set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SKILL_DIR="$REPO_DIR/skills/buddy"
BIN_SRC="$REPO_DIR/bin/buddy"
LOCAL_BIN="$HOME/.local/bin"

echo "=== Installing Buddy ==="

# 1. Ensure scripts are executable
chmod +x "$REPO_DIR/bin/buddy"
chmod +x "$REPO_DIR/skills/buddy/scripts/buddy-ctl"

# 2. Symlink binaries into ~/.local/bin
mkdir -p "$LOCAL_BIN"
ln -sf "$BIN_SRC" "$LOCAL_BIN/buddy"
ln -sf "$SKILL_DIR/scripts/buddy-ctl" "$LOCAL_BIN/buddy-ctl"
echo "✔ Symlinked ~/.local/bin/buddy -> $BIN_SRC"
echo "✔ Symlinked ~/.local/bin/buddy-ctl -> $SKILL_DIR/scripts/buddy-ctl"

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

# 6. Initialize ~/.buddy Control Tower & symlink workspace rules
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
INIT_ARGS+=(--orchestrator-agent "$CHOSEN_ORCH" --worker-agent "$CHOSEN_WORKER")

python3 "$LOCAL_BIN/buddy-ctl" init "${INIT_ARGS[@]}"

echo "=== Buddy installation complete! ==="
echo "You can now run 'buddy' from anywhere or use 'buddy-ctl' commands."
