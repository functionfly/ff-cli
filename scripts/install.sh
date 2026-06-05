#!/usr/bin/env bash
set -e

VERSION="${VERSION:-1.0.0}"
TAG="v${VERSION}"
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Linux*)   OS=linux ;;
  Darwin*)  OS=darwin ;;
  MINGW*|MSYS*|CYGWIN*) OS=windows ;;
  *) echo "Unsupported OS: $OS" >&2; exit 1 ;;
esac

case "$ARCH" in
  x86_64)   ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "Unsupported arch: $ARCH" >&2; exit 1 ;;
esac

case "${OS}_${ARCH}" in
  linux_amd64)   EXT="tar.gz" ;;
  linux_arm64)   EXT="tar.gz" ;;
  darwin_amd64)  EXT="tar.gz" ;;
  darwin_arm64)  EXT="tar.gz" ;;
  windows_amd64) EXT="zip" ;;
  *) echo "No asset for ${OS}/${ARCH}" >&2; exit 1 ;;
esac

FILENAME="ff_${VERSION}_${OS}_${ARCH}.${EXT}"
URL="https://github.com/functionfly/ff-cli/releases/download/${TAG}/${FILENAME}"

INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

echo "Installing ff-cli ${VERSION} for ${OS}/${ARCH}..."
curl -fsSL "$URL" -o "/tmp/${FILENAME}"

case "$EXT" in
  tar.gz) tar -xzf "/tmp/${FILENAME}" -C /tmp ;;
  zip)    unzip -o "/tmp/${FILENAME}" -d /tmp ;;
esac

EXTRACTED_DIR="/tmp/ff_${VERSION}_${OS}_${ARCH}"

if [ -f "${EXTRACTED_DIR}/ff" ]; then
  sudo mv "${EXTRACTED_DIR}/ff" "${INSTALL_DIR}/ff"
elif [ -f "/tmp/ff" ]; then
  sudo mv "/tmp/ff" "${INSTALL_DIR}/ff"
else
  echo "Error: could not find extracted ff binary" >&2
  exit 1
fi

sudo chmod +x "${INSTALL_DIR}/ff"
rm -f "/tmp/${FILENAME}"
rm -rf "${EXTRACTED_DIR}"
echo "Installed ff $(ff version --short 2>/dev/null || echo "${VERSION}")"
