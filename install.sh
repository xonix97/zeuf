#!/usr/bin/env bash
# Zeuf installer.
#
# Builds the zeuf binary and ensures its model backends exist:
#   - OpenCode CLI  (model backend)
#   - Kilo Code CLI (model backend)
#   - Gemini CLI    (model backend)
#   - zeuf itself   (built from this source)
#
# Everything is idempotent: present tools are detected and skipped.
# Nothing is force-reinstalled and no shell rc files are touched by zeuf
# itself (the upstream installers may add their own PATH entries).
#
# Usage:
#   ./install.sh
#   ZEUF_BIN_DIR=~/.local/bin ./install.sh
#
set -euo pipefail

ZEUF_BIN_DIR="${ZEUF_BIN_DIR:-$HOME/.local/bin}"
REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

info() { printf '  %s\n' "$1"; }
ok()   { printf '● %s\n' "$1"; }
warn() { printf '○ %s\n' "$1"; }
die()  { printf '✗ %s\n' "$1" >&2; exit 1; }

have() { command -v "$1" >/dev/null 2>&1; }

path_hint() {
	case ":$PATH:" in
		*":$1:"*) return 0 ;;
	esac
	warn "Add to PATH for this session and your shell rc: export PATH=\"$1:\$PATH\""
	return 1
}

need_curl() {
	have curl || die "curl is required to fetch upstream installers"
}

echo "Zeuf installer"
echo "repo: $REPO_DIR"
echo "bin:  $ZEUF_BIN_DIR"
echo

# ---- 1. Go toolchain (builds zeuf) -----------------------------------------
if have go; then
	ok "go $(go version | awk '{print $3}')"
else
	die "Go toolchain not found. Install Go >= 1.24 (https://go.dev/dl, or: pacman -S go / brew install go / apt install golang), then re-run ./install.sh"
fi

# ---- 2. Node.js (Gemini CLI ships via npm) ----------------------------------
HAVE_NODE=0
if have node; then
	NODE_MAJOR="$(node -v | sed 's/^v//' | cut -d. -f1)"
	if [ "${NODE_MAJOR:-0}" -ge 20 ] 2>/dev/null; then
		ok "node $(node -v)"
		HAVE_NODE=1
	else
		warn "node $(node -v) too old for Gemini CLI (need >= 20); skipping gemini, rest continues"
	fi
else
	warn "node not found; skipping Gemini CLI (install node >= 20, then re-run ./install.sh)"
fi
if ! have npm && [ "$HAVE_NODE" = "1" ]; then
	warn "npm not found alongside node; skipping npm-based installs"
	HAVE_NODE=0
fi

# ---- 3. OpenCode CLI ---------------------------------------------------------
if have opencode; then
	ok "opencode $(opencode --version 2>/dev/null | head -n 1)"
else
	need_curl
	info "installing opencode (official installer)…"
	curl -fsSL https://opencode.ai/install | bash
	hash -r
	have opencode || die "opencode install finished but binary not on PATH; check its installer output above"
	ok "opencode installed"
fi

# ---- 4. Kilo Code CLI --------------------------------------------------------
if have kilo; then
	ok "kilo $(kilo --version 2>/dev/null | head -n 1)"
else
	need_curl
	info "installing kilo (official installer → ~/.kilo/bin)…"
	curl -fsSL https://kilo.ai/cli/install | bash
	hash -r
	if ! have kilo; then
		if [ -x "$HOME/.kilo/bin/kilo" ]; then
			warn "kilo installed to ~/.kilo/bin which is not on PATH"
			path_hint "$HOME/.kilo/bin" || true
		else
			die "kilo install finished but no binary found; try: npm install -g @kilocode/cli"
		fi
	else
		ok "kilo installed"
	fi
fi
if [ -x "$HOME/.kilo/bin/kilo" ]; then
	path_hint "$HOME/.kilo/bin" || true
fi

# ---- 5. Gemini CLI -----------------------------------------------------------
if have gemini; then
	ok "gemini $(gemini --version 2>/dev/null | head -n 1)"
elif [ "$HAVE_NODE" = "1" ]; then
	info "installing @google/gemini-cli (npm)…"
	npm install -g @google/gemini-cli
	hash -r
	if ! have gemini; then
		NPM_BIN="$(npm config get prefix 2>/dev/null)/bin"
		if [ -x "$NPM_BIN/gemini" ]; then
			warn "gemini installed to $NPM_BIN which is not on PATH"
			path_hint "$NPM_BIN" || true
		else
			die "gemini install finished but no binary found"
		fi
	else
		ok "gemini installed"
	fi
	if command -v npm >/dev/null 2>&1; then
		path_hint "$(npm config get prefix 2>/dev/null)/bin" || true
	fi
else
	warn "skipped gemini (needs node >= 20 + npm)"
fi

# ---- 6. zeuf itself ----------------------------------------------------------
mkdir -p "$ZEUF_BIN_DIR"
info "building zeuf…"
(
	cd "$REPO_DIR"
	go build -o "$ZEUF_BIN_DIR/zeuf" .
)
hash -r
[ -x "$ZEUF_BIN_DIR/zeuf" ] || die "zeuf build produced no binary"
ok "zeuf installed → $ZEUF_BIN_DIR/zeuf"
path_hint "$ZEUF_BIN_DIR" || true

# ---- 7. First-run config + health --------------------------------------------
if "$ZEUF_BIN_DIR/zeuf" init >/dev/null 2>&1; then
	ok "zeuf config initialized"
else
	info "zeuf config already present (or init skipped)"
fi

echo
echo "Next: authenticate the backends you want models from —"
echo "  opencode auth login | kilo auth login | gemini   (once, in your terminal)"
echo "  …or attach API keys anytime with:  zeuf connect"
echo
"$ZEUF_BIN_DIR/zeuf" doctor || true
