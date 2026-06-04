#!/usr/bin/env bash
# FunctionFly CLI installer
# Usage: curl -fsSL https://raw.githubusercontent.com/functionfly/fly/main/scripts/install.sh | bash

set -euo pipefail

REPO="functionfly/fly"
BINARY="ff"
INSTALL_DIR="${FLY_INSTALL_DIR:-/usr/local/bin}"

# ── helpers ────────────────────────────────────────────────────────────────────

info()  { printf "\033[1;34m[ff]\033[0m %s\n" "$*"; }
ok()    { printf "\033[1;32m[ff]\033[0m %s\n" "$*"; }
die()   { printf "\033[1;31m[ff]\033[0m error: %s\n" "$*" >&2; exit 1; }

need() {
  command -v "$1" >/dev/null 2>&1 || die "Required command not found: $1"
}

# ── detect platform ─────────────────────────────────────────────────────────

detect_os() {
  case "$(uname -s)" in
    Linux*)  echo "linux"  ;;
    Darwin*) echo "darwin" ;;
    MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
    *) die "Unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)  echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) die "Unsupported architecture: $(uname -m)" ;;
  esac
}

# ── fetch latest tag ─────────────────────────────────────────────────────────

latest_version() {
  need curl
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | head -1 \
    | sed -E 's/.*"([^"]+)".*/\1/'
}

# ── main ──────────────────────────────────────────────────────────────────────

main() {
  need curl
  need tar

  local os arch version ext tarball url tmp

  os="$(detect_os)"
  arch="$(detect_arch)"
  version="${FLY_VERSION:-$(latest_version)}"

  info "Installing ff ${version} for ${os}/${arch}..."

  ext="tar.gz"
  [[ "$os" == "windows" ]] && ext="zip"

  tarball="${BINARY}_${version#v}_${os}_${arch}.${ext}"
  url="https://github.com/${REPO}/releases/download/${version}/${tarball}"

  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT

  info "Downloading ${url}"
  if ! curl -fsSL "$url" -o "${tmp}/${tarball}"; then
    local legacy
    legacy="fly_${version#v}_${os}_${arch}.${ext}"
    url="https://github.com/${REPO}/releases/download/${version}/${legacy}"
    info "Falling back to legacy asset ${legacy}"
    curl -fsSL "$url" -o "${tmp}/${legacy}"
    tarball="${legacy}"
  fi

  if [[ "$ext" == "tar.gz" ]]; then
    tar -xzf "${tmp}/${tarball}" -C "$tmp"
  else
    need unzip
    unzip -q "${tmp}/${tarball}" -d "$tmp"
  fi

  local bin_src
  bin_src="$(find "$tmp" -name "${BINARY}" -type f | head -1)"
  [[ -n "$bin_src" ]] || die "Binary not found in archive"

  if [[ -w "$INSTALL_DIR" ]]; then
    install -m 0755 "$bin_src" "${INSTALL_DIR}/${BINARY}"
  else
    info "Installing to ${INSTALL_DIR} (sudo required)"
    sudo install -m 0755 "$bin_src" "${INSTALL_DIR}/${BINARY}"
  fi

  ok "ff ${version} installed to ${INSTALL_DIR}/${BINARY}"
  ok "Run 'ff --help' to get started."
}

main "$@"
