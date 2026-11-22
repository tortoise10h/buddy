#!/usr/bin/env bash
# Buddy installation bootstrapper
# Installs buddy-ctl and the buddy launcher, then hands off onboarding to `buddy-ctl setup`.

set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_SRC="$REPO_DIR/bin/buddy"
LOCAL_BIN="${PREFIX:-$HOME/.local/bin}"
GITHUB_REPO="tortoise10h/buddy"

BUILD_FROM_SOURCE=false
NO_SETUP=false
DEV_LINK_MODE="auto" # auto | on | off

while [ $# -gt 0 ]; do
    case "$1" in
        --build|-b)
            BUILD_FROM_SOURCE=true
            shift
            ;;
        --prefix)
            if [ $# -lt 2 ]; then
                echo "✖ --prefix requires a directory argument" >&2
                exit 1
            fi
            LOCAL_BIN="$2"
            shift 2
            ;;
        --prefix=*)
            LOCAL_BIN="${1#*=}"
            shift
            ;;
        --no-setup)
            NO_SETUP=true
            shift
            ;;
        --dev-link)
            DEV_LINK_MODE="on"
            shift
            ;;
        --no-dev-link)
            DEV_LINK_MODE="off"
            shift
            ;;
        --help|-h)
            cat <<'EOF'
Usage: ./install.sh [options]

Options:
  --build, -b       Build buddy-ctl from source using Go (implies --dev-link)
  --prefix <dir>    Install binaries into <dir> (default: ~/.local/bin)
  --dev-link        Symlink ~/.buddy/AGENTS.md and skills/buddy to this repo checkout
  --no-dev-link     Materialize embedded assets even when using --build
  --no-setup        Install binaries only; skip running `buddy-ctl setup`
  --help, -h        Show this help message and exit

Environment variables (forwarded to `buddy-ctl setup`):
  BUDDY_BUILD=1             Equivalent to --build
  BUDDY_NONINTERACTIVE=1    Run `buddy-ctl setup --non-interactive`
  BUDDY_PROJECTS_ROOTS=...  Colon- or comma-separated list of project roots
  BUDDY_DEFAULT_ENGINE=     Fallback engine (rex, wezterm, t3, opencode)
  BUDDY_ORCHESTRATOR_AGENT= Terminal orchestrator harness (default: agy)
  BUDDY_WORKER_AGENT=       Terminal worker harness (default: agy)
  BUDDY_T3_PROVIDER=        T3 provider
  BUDDY_T3_MODEL=           T3 model or <provider>/<model>
  BUDDY_OPENCODE_MODEL=     OpenCode default model (<provider>/<model>)
EOF
            exit 0
            ;;
        *)
            echo "✖ Unknown option: $1 (run ./install.sh --help)" >&2
            exit 2
            ;;
    esac
done

if [ "${BUDDY_BUILD:-0}" = "1" ]; then
    BUILD_FROM_SOURCE=true
fi

echo "=== Installing Buddy ==="

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

    echo "Downloading pre-built buddy-ctl (${platform}) from GitHub Releases..."
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

# 1. Provision buddy-ctl binary
PLATFORM="$(detect_platform || echo "unknown")"
BUDDY_CTL_TARGET="$REPO_DIR/bin/buddy-ctl"

if [ "$BUILD_FROM_SOURCE" = true ]; then
    if ! build_buddy_ctl "$BUDDY_CTL_TARGET"; then
        echo ""
        echo "✖ Failed to build buddy-ctl from source. Go compiler is required."
        echo "  • macOS: brew install go"
        echo "  • Linux: https://go.dev/doc/install"
        exit 1
    fi
    echo "✔ Built buddy-ctl from source"
else
    DOWNLOADED=false
    if [ "$PLATFORM" != "unknown" ]; then
        if download_buddy_ctl "$BUDDY_CTL_TARGET" "$PLATFORM"; then
            DOWNLOADED=true
            echo "✔ Downloaded pre-built buddy-ctl (${PLATFORM})"
        fi
    fi

    if [ "$DOWNLOADED" = false ]; then
        if command -v go >/dev/null 2>&1; then
            echo "ℹ Pre-built release unavailable; falling back to local Go build..."
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
            echo "✖ Pre-built binary could not be downloaded and Go is not installed."
            echo "  • macOS: brew install go"
            echo "  • Linux: https://go.dev/doc/install"
            echo "Once installed, re-run: ./install.sh --build"
            exit 1
        fi
    fi
fi

# 2. Install binaries into LOCAL_BIN
chmod +x "$BIN_SRC" "$BUDDY_CTL_TARGET"
mkdir -p "$LOCAL_BIN"

ln -sf "$BIN_SRC" "$LOCAL_BIN/buddy"
rm -f "$LOCAL_BIN/buddy-ctl"
cp "$BUDDY_CTL_TARGET" "$LOCAL_BIN/buddy-ctl"
chmod +x "$LOCAL_BIN/buddy-ctl"

echo "✔ Installed $LOCAL_BIN/buddy and $LOCAL_BIN/buddy-ctl"

if [ "$NO_SETUP" = true ]; then
    echo "ℹ Skipping setup (--no-setup). Run '$LOCAL_BIN/buddy-ctl setup' when ready."
    exit 0
fi

# 3. Hand off onboarding / upgrade to `buddy-ctl setup`
SETUP_ARGS=(setup --buddy-ctl-path "$LOCAL_BIN/buddy-ctl")

USE_DEV_LINK=false
if [ "$DEV_LINK_MODE" = "on" ]; then
    USE_DEV_LINK=true
elif [ "$DEV_LINK_MODE" = "auto" ] && [ "$BUILD_FROM_SOURCE" = true ]; then
    USE_DEV_LINK=true
fi

if [ "$USE_DEV_LINK" = true ]; then
    SETUP_ARGS+=(--dev-link "$REPO_DIR")
fi

BUDDY_DIR="${BUDDY_DIR:-$HOME/.buddy}"
IS_NONINTERACTIVE=false
if [ "${BUDDY_NONINTERACTIVE:-0}" = "1" ] || [ "${BUDDY_NONINTERACTIVE:-}" = "true" ] || [ "${CI:-}" = "true" ]; then
    IS_NONINTERACTIVE=true
fi

if [ "$IS_NONINTERACTIVE" = true ]; then
    "$LOCAL_BIN/buddy-ctl" "${SETUP_ARGS[@]}" --non-interactive
elif [ -f "$BUDDY_DIR/config.json" ]; then
    "$LOCAL_BIN/buddy-ctl" "${SETUP_ARGS[@]}" --upgrade
elif [ -t 0 ] && [ -t 1 ]; then
    "$LOCAL_BIN/buddy-ctl" "${SETUP_ARGS[@]}"
elif [ -r /dev/tty ] && [ -w /dev/tty ]; then
    "$LOCAL_BIN/buddy-ctl" "${SETUP_ARGS[@]}" </dev/tty
else
    "$LOCAL_BIN/buddy-ctl" "${SETUP_ARGS[@]}" --non-interactive
fi
