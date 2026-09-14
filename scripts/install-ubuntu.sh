#!/usr/bin/env sh
# Installs what the migration needs on Ubuntu (22.04 or newer, amd64 or
# arm64): Go, chorctl and mc. Docker is checked but not installed, because
# that is a system-level choice; the script prints the official link.
#
# Safe to re-run: anything already present at the right version is skipped.
# Uses sudo for /usr/local; run it as your normal user.
set -eu

GO_VERSION="${GO_VERSION:-1.27.1}"
CHORCTL_VERSION="${CHORCTL_VERSION:-0.7.10}"
MC_RELEASE="${MC_RELEASE:-RELEASE.2025-08-13T08-35-41Z}"
BIN=/usr/local/bin

case "$(uname -m)" in
  x86_64)  ARCH=amd64 ;;
  aarch64) ARCH=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

SUDO=""
[ "$(id -u)" -eq 0 ] || SUDO=sudo

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "== Docker"
if docker compose version >/dev/null 2>&1; then
  echo "   ok: $(docker compose version)"
else
  echo "   missing. Install Docker Engine with the Compose plugin:"
  echo "   https://docs.docker.com/engine/install/ubuntu/"
  echo "   then: sudo usermod -aG docker \$USER && newgrp docker"
  DOCKER_MISSING=1
fi

echo "== Go $GO_VERSION"
if /usr/local/go/bin/go version 2>/dev/null | grep -q "go$GO_VERSION "; then
  echo "   ok: $(/usr/local/go/bin/go version)"
else
  curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" -o "$TMP/go.tgz"
  $SUDO rm -rf /usr/local/go
  $SUDO tar -C /usr/local -xzf "$TMP/go.tgz"
  echo "   installed: $(/usr/local/go/bin/go version)"
fi
if ! grep -qs '/usr/local/go/bin' "$HOME/.profile"; then
  echo 'export PATH=$PATH:/usr/local/go/bin' >> "$HOME/.profile"
  echo "   added /usr/local/go/bin to PATH in ~/.profile (open a new shell, or: export PATH=\$PATH:/usr/local/go/bin)"
fi

echo "== chorctl $CHORCTL_VERSION"
if chorctl --version 2>/dev/null | grep -q "$CHORCTL_VERSION"; then
  echo "   ok: $(chorctl --version)"
else
  curl -fsSL "https://github.com/clyso/chorus/releases/download/v${CHORCTL_VERSION}/chorctl_v${CHORCTL_VERSION}_linux_${ARCH}.tar.gz" \
    | tar -xz -C "$TMP" chorctl
  $SUDO install "$TMP/chorctl" "$BIN/chorctl"
  echo "   installed: $(chorctl --version)"
fi

echo "== mc $MC_RELEASE"
if command -v mc >/dev/null 2>&1; then
  echo "   ok: $(mc --version | head -1)"
else
  curl -fsSL "https://github.com/minio/mc/releases/download/${MC_RELEASE}/mc.linux-${ARCH}.${MC_RELEASE}" -o "$TMP/mc"
  $SUDO install "$TMP/mc" "$BIN/mc"
  echo "   installed: $(mc --version | head -1)"
fi

echo
if [ "${DOCKER_MISSING:-}" = 1 ]; then
  echo "Install Docker (see above), then: make config"
else
  echo "All set. Next: cp .env.example .env, edit it, then make config"
fi
