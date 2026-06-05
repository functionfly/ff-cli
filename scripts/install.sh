#!/usr/bin/env bash
set -e

VERSION="${VERSION:-1.0.0}"
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Linux*)   OS=linux ;;
  Darwin*)  OS=darwin ;;
  MINGW*|MSYS*|CYGWIN*) OS=windows ;;
  *)        echo "Unsupported OS: $OS" >&2; exit 1 ;;
esac

case "$ARCH" in
  x86_64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "Unsupported arch: $ARCH" >&2; exit 1 ;;
esac

EXT="${OS}.tar.gz"
FILENAME="ff_${VERSION}_${OS}_${ARCH}.${EXT}"
URL="https://github.com/functionfly/ff-cli/releases/download/${VERSION}/${FILENAME}"

echo "Installing ff-cli ${VERSION} for ${OS}/${ARCH}..."
curl -fsSL "$URL" -o "/tmp/${FILENAME}"
tar -xzf "/tmp/${FILENAME}" -C /tmp
sudo mv /tmp/ff /usr/local/bin/ff
chmod +x /usr/local/bin/ff
rm -f "/tmp/${FILENAME}"
echo "Installed ff $(ff version --short 2>/dev/null || echo "${VERSION}")"
