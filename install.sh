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

# 4. Initialize ~/.buddy Control Tower & symlink workspace rules
echo "Initializing ~/.buddy Control Tower..."
mkdir -p "$HOME/.buddy/.agents"
ln -sf "$REPO_DIR/AGENTS.md" "$HOME/.buddy/AGENTS.md"
ln -sf "$REPO_DIR/.agents/hooks.json" "$HOME/.buddy/.agents/hooks.json"
echo "✔ Symlinked ~/.buddy/AGENTS.md -> $REPO_DIR/AGENTS.md"
echo "✔ Symlinked ~/.buddy/.agents/hooks.json -> $REPO_DIR/.agents/hooks.json"

python3 "$LOCAL_BIN/buddy-ctl" init

echo "=== Buddy installation complete! ==="
echo "You can now run 'buddy' from anywhere or use 'buddy-ctl' commands."
