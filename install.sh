#!/bin/sh
set -e

# Versioned install target.
# By default the binary is installed under $RHO_HOME/bin (default ~/.rho/bin).
# Override with RHO_HOME env var, or pass --prefix <dir> as the first flag.
RHO_HOME="${RHO_HOME:-$HOME/.rho}"
if [ "$1" = "--prefix" ]; then
  RHO_HOME="$2"
  shift 2
fi

REPO="GrayCodeAI/rho"
BINARY="rho"

# Version pin: --version <ver> or RHO_VERSION env (e.g. 0.1.0 or v0.1.0)
VERSION_PIN="${RHO_VERSION:-}"
if [ "$1" = "--version" ]; then
  VERSION_PIN="$2"
  shift 2
elif [ "$3" = "--version" ]; then
  # handle --prefix <dir> --version <ver> order
  VERSION_PIN="$4"
  set -- "$1" "$2"
fi
VERSION_PIN=$(printf '%s' "$VERSION_PIN" | sed 's/^v//')

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
esac

ARCHIVE_EXT="tar.gz"
BIN_NAME="$BINARY"

case "$OS" in
  mingw*|msys*|cygwin*)
    OS="windows"
    ARCHIVE_EXT="zip"
    BIN_NAME="${BINARY}.exe"
    ;;
esac

if [ -n "$VERSION_PIN" ]; then
  LATEST="$VERSION_PIN"
else
  LATEST=$(curl -fsSL --proto '=https' --tlsv1.2 "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | sed -E 's/.*"v([^"]+)".*/\1/')
  if [ -z "$LATEST" ]; then
    echo "Error: could not determine latest version"
    exit 1
  fi
fi

ARCHIVE_NAME="${BINARY}_${LATEST}_${OS}_${ARCH}.${ARCHIVE_EXT}"
URL="https://github.com/$REPO/releases/download/v${LATEST}/${ARCHIVE_NAME}"
echo "Downloading rho v${LATEST} for ${OS}/${ARCH}..."

TMP=$(mktemp -d)
ARCHIVE="$TMP/${ARCHIVE_NAME}"
curl -fsSL --proto '=https' --tlsv1.2 "$URL" -o "$ARCHIVE"

CHECKSUMS_URL="https://github.com/$REPO/releases/download/v${LATEST}/checksums.txt"
CHECKSUMS="$TMP/checksums.txt"
curl -fsSL --proto '=https' --tlsv1.2 "$CHECKSUMS_URL" -o "$CHECKSUMS"

# Verify the checksums.txt signature with cosign if available. This protects
# against a compromised release (not just transport corruption). When cosign
# is not installed, we fall back to checksum-only verification.
CERT_URL="https://github.com/$REPO/releases/download/v${LATEST}/checksums.txt.cert"
SIG_URL="https://github.com/$REPO/releases/download/v${LATEST}/checksums.txt.sig"
CERT_FILE="$TMP/checksums.txt.cert"
SIG_FILE="$TMP/checksums.txt.sig"
if command -v cosign >/dev/null 2>&1; then
  echo "Verifying release signature with cosign..."
  if ! curl -fsSL --proto '=https' --tlsv1.2 --max-time 30 "$CERT_URL" -o "$CERT_FILE"; then
    echo "Error: could not download signature certificate — refusing to install unverified release"
    rm -rf "$TMP"
    exit 1
  fi
  if ! curl -fsSL --proto '=https' --tlsv1.2 --max-time 30 "$SIG_URL" -o "$SIG_FILE"; then
    echo "Error: could not download signature file — refusing to install unverified release"
    rm -rf "$TMP"
    exit 1
  fi
  # Anchor and escape the identity regex so '.' matches literal dots only.
  IDENTITY="https://github\.com/${REPO}/\.github/workflows/release\.yml@refs/tags/v${LATEST}"
  if ! cosign verify-blob \
    --certificate "$CERT_FILE" \
    --signature "$SIG_FILE" \
    --certificate-identity-regexp "$IDENTITY" \
    --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
    "$CHECKSUMS"; then
    echo "Error: signature verification failed — release may be compromised"
    rm -rf "$TMP"
    exit 1
  fi
  echo "Signature verified."
else
  echo "Note: cosign not installed — install from https://docs.sigstore.dev to verify release signatures"
fi

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL=$(sha256sum "$ARCHIVE" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL=$(shasum -a 256 "$ARCHIVE" | awk '{print $1}')
else
  echo "Error: no sha256sum or shasum found; cannot verify checksum"
  rm -rf "$TMP"
  exit 1
fi

# Exact-field match (not a regex grep): avoids '.' wildcards in the archive
# name matching other lines and producing a multi-line EXPECTED value.
EXPECTED=$(awk -v f="$ARCHIVE_NAME" '$2 == f { print $1 }' "$CHECKSUMS")
if [ -z "$EXPECTED" ]; then
  echo "Error: checksum not found for ${ARCHIVE_NAME} in checksums.txt"
  rm -rf "$TMP"
  exit 1
fi

if [ "$ACTUAL" != "$EXPECTED" ]; then
  echo "Error: checksum verification failed"
  echo "  expected: $EXPECTED"
  echo "  actual:   $ACTUAL"
  rm -rf "$TMP"
  exit 1
fi
echo "Checksum verified."

if [ "$OS" = "windows" ] && ! command -v unzip >/dev/null 2>&1; then
  echo "Error: unzip is required to install Windows release archives"
  rm -rf "$TMP"
  exit 1
fi

if [ "$OS" = "windows" ]; then
  unzip -q "$ARCHIVE" -d "$TMP"
else
  tar xz -C "$TMP" -f "$ARCHIVE"
fi

# Strip a leading "v" if the release tag was returned with one, so the
# versioned file name is the bare semver (e.g. 1.2.3).
VERSION=$(printf '%s' "$LATEST" | sed 's/^v//')

# --- Versioned install -------------------------------------------------------
# Adopted from SpaceXAI grok's postinstall.js. We never overwrite a binary in
# place. On macOS (and any codesigned platform) replacing a file that a running
# process has mmap'd invalidates the kernel's code-signature cache; the kernel
# then SIGKILLs that process. Installing into a per-version file and swapping
# the symlink means a running process keeps its open fd on the old inode and
# keeps running the previous version untouched — no SIGKILL, no disruption.
#
# The symlink is written to a temp name and then renamed into place so the
# rename is atomic; a racing process either sees the old or new link, never a
# half-written one.
#
# Release checksums are signed with cosign keyless (OIDC) in the release
# workflow. install.sh verifies the signature when cosign is available,
# falling back to checksum-only verification otherwise. Versioned install is a
# prerequisite for safe in-place self-update tooling: once installs land at a
# stable versioned path + symlink, a future updater can swap the link without
# ever replacing a binary a running rho has mmap'd (same SIGKILL rationale).
#
# Windows lacks reliable non-admin symlinks, so the launcher is a plain copy.

BINDIR="$RHO_HOME/bin"
mkdir -p "$BINDIR"

if [ "$OS" = "windows" ]; then
  mv -f "$TMP/$BIN_NAME" "$BINDIR/rho-$VERSION.exe"
  cp -f "$BINDIR/rho-$VERSION.exe" "$BINDIR/rho.exe"
  echo ""
  echo "Installed rho v$VERSION to $BINDIR/rho-$VERSION.exe"
  echo "Linked launcher: $BINDIR/rho.exe"
else
  mv -f "$TMP/$BIN_NAME" "$BINDIR/rho-$VERSION"
  ln -sf "rho-$VERSION" "$BINDIR/rho.tmp" \
    && mv -f "$BINDIR/rho.tmp" "$BINDIR/rho"
  echo ""
  echo "Installed rho v$VERSION to $BINDIR/rho-$VERSION (linked: $BINDIR/rho)"
fi

rm -rf "$TMP"
echo ""
echo "Add $BINDIR to your PATH if it is not already, e.g."
echo "  export PATH=\"\$PATH:$BINDIR\""
echo ""
echo "Restart any running rho sessions to pick up the new binary — the old"
echo "process keeps running the previous version until it is restarted."
