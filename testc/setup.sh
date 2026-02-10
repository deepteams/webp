#!/bin/bash
set -euo pipefail

LIBWEBP_VERSION="v1.5.0"
LIBWEBP_REPO="https://chromium.googlesource.com/webm/libwebp"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TARGET_DIR="$SCRIPT_DIR/libwebp"

if [ -d "$TARGET_DIR/src" ]; then
    echo "libwebp already present at $TARGET_DIR"
    echo "To update, remove it first: rm -rf $TARGET_DIR"
    exit 0
fi

echo "Cloning libwebp $LIBWEBP_VERSION into $TARGET_DIR ..."
git clone --depth 1 --branch "$LIBWEBP_VERSION" "$LIBWEBP_REPO" "$TARGET_DIR"

echo "Done. Run conformance tests with:"
echo "  go test -tags testc ./testc/..."
