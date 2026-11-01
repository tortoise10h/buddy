#!/usr/bin/env bash
# Buddy installation script
# Installs the buddy-ctl CLI and configures Buddy agent integrations

set -euo pipefail

DEFAULT_PREFIX="$HOME/.local/bin"
DEFAULT_NAME="buddy-ctl"
DEFAULT_BUDDY_DIR="$HOME/.buddy"

PREFIX="$DEFAULT_PREFIX"
NAME="$DEFAULT_NAME"
TAG=""
FROM_SOURCE=0
DRY_RUN=0
NO_SKILLS=0
FORCE=0

# Detect repository directory if running from a local checkout
REPO_DIR=""
if [ -n "${BASH_SOURCE[0]:-}" ] && [ -f "${BASH_SOURCE[0]}" ]; then
    SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [ -f "$SCRIPT_DIR/go.mod" ]; then
        REPO_DIR="$SCRIPT_DIR"
    fi
elif [ -f "./go.mod" ]; then
    REPO_DIR="$(pwd)"
fi

print_help() {
    cat <<'EOF'
Usage: install.sh [options]

Installs the buddy-ctl binary and configures Buddy agent integrations.

Options:
  --prefix DIR       Install directory for binaries (default: ~/.local/bin)
  --name NAME        Binary name (default: buddy-ctl)
  --version TAG      Release version to download (e.g. v1.0.0; default: latest)
  --from-source      Force building from source using Go ('make build')
  --dry-run          Print installation steps without modifying files
  --no-skills        Skip agent skill symlinks and ~/.buddy configuration
  --force            Overwrite existing binary even if not the Go buddy-ctl
  -h, --help         Show this help message and exit

Environment Variables:
  BUDDY_INSTALL_BASE_URL   Override release download base URL (for local testing/mirrors)
  BUDDY_DIR                Override ~/.buddy control tower directory (default: ~/.buddy)
EOF
}

# Parse command line flags
while [ $# -gt 0 ]; do
    case "$1" in
        -h|--help)
            print_help
            exit 0
            ;;
        --prefix)
            [ $# -ge 2 ] || { echo "Error: --prefix requires a directory path" >&2; exit 1; }
            PREFIX="$2"
            shift 2
            ;;
        --prefix=*)
            PREFIX="${1#*=}"
            shift
            ;;
        --name)
            [ $# -ge 2 ] || { echo "Error: --name requires a binary name" >&2; exit 1; }
            NAME="$2"
            shift 2
            ;;
        --name=*)
            NAME="${1#*=}"
            shift
            ;;
        --version)
            [ $# -ge 2 ] || { echo "Error: --version requires a version tag" >&2; exit 1; }
            TAG="$2"
            shift 2
            ;;
        --version=*)
            TAG="${1#*=}"
            shift
            ;;
        --from-source)
            FROM_SOURCE=1
            shift
            ;;
        --dry-run)
            DRY_RUN=1
            shift
            ;;
        --no-skills)
            NO_SKILLS=1
            shift
            ;;
        --force)
            FORCE=1
            shift
            ;;
        *)
            echo "Error: unknown option '$1'" >&2
            echo "Run './install.sh --help' for usage." >&2
            exit 1
            ;;
    esac
done

# Resolve installation mode: "source" or "download"
INSTALL_MODE=""
if [ "$FROM_SOURCE" -eq 1 ]; then
    if [ -z "$REPO_DIR" ] || [ ! -f "$REPO_DIR/go.mod" ]; then
        echo "Error: --from-source requested, but no repository with go.mod was found." >&2
        echo "Please clone the repository first: git clone https://github.com/tortoise10h/buddy.git" >&2
        exit 1
    fi
    if ! command -v go >/dev/null 2>&1; then
        echo "Error: --from-source requested, but the Go compiler ('go') was not found on PATH." >&2
        echo "Install Go 1.26+ or run install.sh without --from-source to use prebuilt binaries." >&2
        exit 1
    fi
    INSTALL_MODE="source"
elif [ -n "${BUDDY_INSTALL_BASE_URL:-}" ]; then
    INSTALL_MODE="download"
elif [ -n "$REPO_DIR" ] && [ -f "$REPO_DIR/go.mod" ] && command -v go >/dev/null 2>&1; then
    INSTALL_MODE="source"
else
    INSTALL_MODE="download"
fi

TARGET_BIN="$PREFIX/$NAME"

# Safety check: if target binary exists, ensure we do not clobber non-Go files without --force
if [ -e "$TARGET_BIN" ] || [ -L "$TARGET_BIN" ]; then
    IS_GO_BIN=0
    if [ -x "$TARGET_BIN" ]; then
        VER_OUT="$("$TARGET_BIN" version 2>/dev/null || true)"
        if [[ "$VER_OUT" == *"buddy-ctl version"* ]]; then
            IS_GO_BIN=1
        fi
    fi

    if [ "$IS_GO_BIN" -ne 1 ]; then
        if [ "$FORCE" -ne 1 ]; then
            echo "Error: '$TARGET_BIN' already exists and is not the Go buddy-ctl binary." >&2
            if [ -L "$TARGET_BIN" ] || head -n 1 "$TARGET_BIN" 2>/dev/null | grep -q "python"; then
                echo "Existing file appears to be the Python buddy-ctl script or symlink." >&2
            fi
            echo "To avoid accidentally clobbering your current CLI, installation was aborted." >&2
            echo "" >&2
            echo "Options:" >&2
            echo "  • Pass --force to overwrite: ./install.sh --force" >&2
            echo "  • Or install under a different name: ./install.sh --name buddy-ctl-go" >&2
            exit 1
        else
            echo "Warning: Overwriting existing non-Go binary at '$TARGET_BIN' (--force specified)."
        fi
    fi
fi

# Execute installation mode
if [ "$INSTALL_MODE" = "source" ]; then
    echo "=== Installing Buddy from source ==="
    echo "Building binary in $REPO_DIR..."
    if [ "$DRY_RUN" -eq 1 ]; then
        echo "[dry-run] Would build: make build (or go build)"
        echo "[dry-run] Would install to: $TARGET_BIN"
    else
        if command -v make >/dev/null 2>&1; then
            (cd "$REPO_DIR" && make build)
        else
            (cd "$REPO_DIR" && CGO_ENABLED=0 go build -trimpath -o bin/buddy-ctl ./cmd/buddy-ctl)
        fi

        if [ ! -f "$REPO_DIR/bin/buddy-ctl" ]; then
            echo "Error: Build succeeded but '$REPO_DIR/bin/buddy-ctl' was not created." >&2
            exit 1
        fi

        mkdir -p "$PREFIX"
        install -m 0755 "$REPO_DIR/bin/buddy-ctl" "$TARGET_BIN"
        echo "✔ Installed binary to $TARGET_BIN"
    fi

elif [ "$INSTALL_MODE" = "download" ]; then
    echo "=== Installing Buddy from prebuilt release ==="

    OS_RAW="$(uname -s)"
    case "$(echo "$OS_RAW" | tr '[:upper:]' '[:lower:]')" in
        darwin) OS="darwin" ;;
        linux)  OS="linux" ;;
        *)
            echo "Error: Unsupported operating system '$OS_RAW'." >&2
            echo "Buddy currently provides prebuilt binaries for macOS (darwin) and Linux." >&2
            exit 1
            ;;
    esac

    ARCH_RAW="$(uname -m)"
    case "$(echo "$ARCH_RAW" | tr '[:upper:]' '[:lower:]')" in
        x86_64|amd64)   ARCH="amd64" ;;
        arm64|aarch64) ARCH="arm64" ;;
        *)
            echo "Error: Unsupported architecture '$ARCH_RAW'." >&2
            echo "Buddy currently provides prebuilt binaries for amd64 and arm64." >&2
            exit 1
            ;;
    esac

    ARTIFACT="buddy-ctl_${OS}_${ARCH}"

    # Resolve version tag
    if [ -n "$TAG" ]; then
        if [[ "$TAG" =~ ^[0-9] ]]; then
            RELEASE_TAG="v$TAG"
        else
            RELEASE_TAG="$TAG"
        fi
    elif [ -n "${BUDDY_INSTALL_BASE_URL:-}" ]; then
        RELEASE_TAG=""
    else
        echo "Resolving latest release from GitHub..."
        LATEST_JSON="$(curl -fsSL https://api.github.com/repos/tortoise10h/buddy/releases/latest 2>/dev/null || true)"
        RELEASE_TAG="$(echo "$LATEST_JSON" | grep -o '"tag_name": *"[^"]*"' | head -n 1 | cut -d'"' -f4 || true)"
        if [ -z "$RELEASE_TAG" ]; then
            echo "Error: Could not determine latest release version from GitHub." >&2
            echo "Please specify a version explicitly: ./install.sh --version v1.0.0" >&2
            exit 1
        fi
    fi

    # Determine artifact and checksums URLs
    if [ -n "${BUDDY_INSTALL_BASE_URL:-}" ]; then
        BASE="${BUDDY_INSTALL_BASE_URL%/}"
        BIN_URL="$BASE/$ARTIFACT"
        CHECKSUMS_URL="$BASE/checksums.txt"
        if [ -n "$RELEASE_TAG" ]; then
            if ! curl -fsSL -I "$BIN_URL" >/dev/null 2>&1; then
                BIN_URL="$BASE/$RELEASE_TAG/$ARTIFACT"
                CHECKSUMS_URL="$BASE/$RELEASE_TAG/checksums.txt"
            fi
        fi
    else
        BIN_URL="https://github.com/tortoise10h/buddy/releases/download/${RELEASE_TAG}/${ARTIFACT}"
        CHECKSUMS_URL="https://github.com/tortoise10h/buddy/releases/download/${RELEASE_TAG}/checksums.txt"
    fi

    if [ "$DRY_RUN" -eq 1 ]; then
        echo "[dry-run] Would download binary from: $BIN_URL"
        echo "[dry-run] Would download checksums from: $CHECKSUMS_URL"
        echo "[dry-run] Would verify sha256 checksum for $ARTIFACT"
        echo "[dry-run] Would install to: $TARGET_BIN"
    else
        TMP_DIR="$(mktemp -d -t buddy-install-XXXXXX)"
        cleanup() {
            rm -rf "$TMP_DIR"
        }
        trap cleanup EXIT

        echo "Downloading checksums from $CHECKSUMS_URL..."
        if ! curl -fsSL "$CHECKSUMS_URL" -o "$TMP_DIR/checksums.txt"; then
            echo "Error: Failed to download checksums from: $CHECKSUMS_URL" >&2
            exit 1
        fi

        echo "Downloading $ARTIFACT from $BIN_URL..."
        if ! curl -fsSL "$BIN_URL" -o "$TMP_DIR/buddy-ctl"; then
            echo "Error: Failed to download binary from: $BIN_URL" >&2
            exit 1
        fi

        # Verify SHA-256
        EXPECTED_HASH="$(grep -E "(^|[[:space:]])${ARTIFACT}([[:space:]]|$)" "$TMP_DIR/checksums.txt" | awk '{print $1}' | head -n 1 || true)"
        if [ -z "$EXPECTED_HASH" ]; then
            EXPECTED_HASH="$(grep "${ARTIFACT}" "$TMP_DIR/checksums.txt" | awk '{print $1}' | head -n 1 || true)"
        fi
        if [ -z "$EXPECTED_HASH" ]; then
            echo "Error: Could not find sha256 hash for '$ARTIFACT' in checksums.txt" >&2
            exit 1
        fi

        ACTUAL_HASH=""
        if command -v sha256sum >/dev/null 2>&1; then
            ACTUAL_HASH="$(sha256sum "$TMP_DIR/buddy-ctl" | awk '{print $1}')"
        elif command -v shasum >/dev/null 2>&1; then
            ACTUAL_HASH="$(shasum -a 256 "$TMP_DIR/buddy-ctl" | awk '{print $1}')"
        elif command -v openssl >/dev/null 2>&1; then
            ACTUAL_HASH="$(openssl dgst -sha256 "$TMP_DIR/buddy-ctl" | awk '{print $NF}')"
        else
            echo "Error: No sha256 verification tool found (checked sha256sum, shasum, openssl)." >&2
            exit 1
        fi

        if [ "$EXPECTED_HASH" != "$ACTUAL_HASH" ]; then
            rm -f "$TMP_DIR/buddy-ctl"
            echo "Error: SHA256 checksum mismatch for $ARTIFACT:" >&2
            echo "  Expected: $EXPECTED_HASH" >&2
            echo "  Actual:   $ACTUAL_HASH" >&2
            echo "Installation aborted. The downloaded binary has been deleted." >&2
            exit 1
        fi
        echo "✔ SHA256 checksum verified ($ACTUAL_HASH)"

        # macOS Gatekeeper quarantine removal & ad-hoc codesign
        if [ "$OS" = "darwin" ]; then
            if command -v xattr >/dev/null 2>&1; then
                xattr -d com.apple.quarantine "$TMP_DIR/buddy-ctl" 2>/dev/null || true
            fi
            if command -v codesign >/dev/null 2>&1; then
                codesign --force -s - "$TMP_DIR/buddy-ctl" 2>/dev/null || true
            fi
        fi

        mkdir -p "$PREFIX"
        install -m 0755 "$TMP_DIR/buddy-ctl" "$TARGET_BIN"
        echo "✔ Installed binary to $TARGET_BIN"
    fi
fi

# Install launcher script 'buddy' if repository is present
if [ -n "$REPO_DIR" ] && [ -f "$REPO_DIR/bin/buddy" ] && [ "$DRY_RUN" -ne 1 ]; then
    if [ "$PREFIX" = "$DEFAULT_PREFIX" ]; then
        ln -sf "$REPO_DIR/bin/buddy" "$PREFIX/buddy"
        echo "✔ Symlinked $PREFIX/buddy -> $REPO_DIR/bin/buddy"
    else
        cp "$REPO_DIR/bin/buddy" "$PREFIX/buddy"
        chmod +x "$PREFIX/buddy"
        echo "✔ Installed $PREFIX/buddy"
    fi
fi

# Handle agent skills and control tower configuration
BUDDY_DIR="${BUDDY_DIR:-$DEFAULT_BUDDY_DIR}"

if [ "$NO_SKILLS" -eq 1 ]; then
    echo "✔ Skipping agent skills and control tower symlinks (--no-skills specified)."
elif [ "$PREFIX" != "$DEFAULT_PREFIX" ] && [ -z "${BUDDY_FORCE_SKILLS:-}" ]; then
    echo "✔ Non-default prefix '$PREFIX' detected. Skipping global agent skills and control tower symlinks."
elif [ -z "$REPO_DIR" ] || [ ! -d "$REPO_DIR/skills/buddy" ]; then
    echo "Notice: Repository skills directory not found locally; skipping agent skill symlinks."
    echo "To install agent skills, clone https://github.com/tortoise10h/buddy.git and run ./install.sh"
else
    echo "Configuring agent skills..."
    SKILL_DIR="$REPO_DIR/skills/buddy"
    SKILL_TARGETS=(
        "$HOME/.agents/skills/buddy"
        "$HOME/.gemini/config/skills/buddy"
        "$HOME/.gemini/antigravity-cli/skills/buddy"
    )
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

    echo "Initializing control tower in $BUDDY_DIR..."
    mkdir -p "$BUDDY_DIR/.agents"
    ln -sf "$REPO_DIR/AGENTS.md" "$BUDDY_DIR/AGENTS.md"
    ln -sf "$REPO_DIR/.agents/hooks.json" "$BUDDY_DIR/.agents/hooks.json"
    echo "✔ Symlinked $BUDDY_DIR/AGENTS.md -> $REPO_DIR/AGENTS.md"
    echo "✔ Symlinked $BUDDY_DIR/.agents/hooks.json -> $REPO_DIR/.agents/hooks.json"

    # Initialize ~/.buddy config if target binary is executable
    if [ -x "$TARGET_BIN" ]; then
        "$TARGET_BIN" init >/dev/null 2>&1 || true
    fi
fi

echo ""
echo "=== Buddy installation complete! ==="
echo "Binary: $TARGET_BIN"
if ! echo "$PATH" | tr ':' '\n' | grep -qx "$PREFIX"; then
    echo ""
    echo "Note: '$PREFIX' is not on your PATH."
    echo "Add it to your shell profile (~/.zshrc, ~/.bashrc, or ~/.config/fish/config.fish):"
    echo "  export PATH=\"$PREFIX:\$PATH\""
fi
