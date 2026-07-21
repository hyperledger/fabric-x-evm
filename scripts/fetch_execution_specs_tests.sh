#!/bin/bash
# Copyright IBM Corp. All Rights Reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Pinned ethereum/execution-specs conformance fixtures (tests@v20.0.1: Osaka + BPO1 + BPO2).
# Bump the tag and checksum together, deliberately.
VERSION="tests@v20.0.1"
# Derive the URL from VERSION (URL-encoding the '@') so a version bump only
# touches VERSION + SHA256, never a separately-pinned URL.
URL="https://github.com/ethereum/execution-specs/releases/download/${VERSION/@/%40}/fixtures.tar.gz"
SHA256="3586193db06d4d5745d5e90b3c3008c2255a4e19ccd8f11a3ce887aec8c0b17c"

DEST_DIR="${PROJECT_ROOT}/testdata/execution-specs-tests"
TARBALL="${DEST_DIR}/fixtures.tar.gz"
FIXTURES_DIR="${DEST_DIR}/fixtures"

# sha256 helper: sha256sum on Linux/CI, shasum on macOS.
sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "error: need 'sha256sum' or 'shasum' to verify the download" >&2
    exit 1
  fi
}

mkdir -p "${DEST_DIR}"

if [ -f "${TARBALL}" ]; then
  # Present already: verify it. Matching checksum -> idempotent skip.
  # Mismatch -> fail loudly rather than silently re-downloading over it.
  if [ "$(sha256_of "${TARBALL}")" = "${SHA256}" ]; then
    echo "==> ${VERSION} fixtures already present and verified; skipping download"
  else
    echo "error: existing ${TARBALL} failed checksum verification." >&2
    echo "  expected ${SHA256}" >&2
    echo "  delete the file and re-run to fetch a clean copy." >&2
    exit 1
  fi
else
  echo "==> Downloading ${VERSION} fixtures (~400 MB)..."
  # Show a progress bar interactively; stay quiet in CI/non-TTY logs.
  if [ -t 1 ]; then
    curl --fail --location --progress-bar "${URL}" --output "${TARBALL}"
  else
    curl --fail --location --silent --show-error "${URL}" --output "${TARBALL}"
  fi
  if [ "$(sha256_of "${TARBALL}")" != "${SHA256}" ]; then
    echo "error: downloaded ${TARBALL} failed checksum verification; removing it." >&2
    echo "  expected ${SHA256}" >&2
    rm -f "${TARBALL}"
    exit 1
  fi
fi

echo "==> Extracting into ${DEST_DIR}/ ..."
rm -rf "${FIXTURES_DIR}"
tar -xzf "${TARBALL}" -C "${DEST_DIR}"

echo "==> Done. Fixtures at: ${FIXTURES_DIR}"
