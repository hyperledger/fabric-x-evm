#!/bin/bash
# Copyright IBM Corp. All Rights Reserved.
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Pinned ethereum/execution-specs conformance fixtures (tests@v21.0.0: Osaka + BPO1 + BPO2
# + Amsterdam, which the runner's fork filter excludes).
# Bump the tag and checksum together, deliberately.
VERSION="tests@v21.0.0"
# Derive the URL from VERSION (URL-encoding the '@') so a version bump only
# touches VERSION + SHA256, never a separately-pinned URL.
URL="https://github.com/ethereum/execution-specs/releases/download/${VERSION/@/%40}/fixtures.tar.gz"
SHA256="dd642a261ac63f910b85c47d2549d2bb3ae4ad044fb5e852657f783441483bb2"

DEST_DIR="${PROJECT_ROOT}/testdata/execution-specs-tests"
TARBALL="${DEST_DIR}/fixtures.tar.gz"
FIXTURES_DIR="${DEST_DIR}/fixtures"
# Records which tarball's checksum the current FIXTURES_DIR was extracted
# from, so a re-run can skip the (slow) extraction step the same way
# the download step is already skipped once the tarball is present and verified.
EXTRACTED_STAMP="${DEST_DIR}/.fixtures-extracted.sha256"

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
  echo "==> Downloading ${VERSION} fixtures (~950 MB)..."
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

# Same idiom as the download above: matching stamp -> idempotent skip;
# missing/stale stamp (version bump, interrupted prior extraction, etc.) ->
# re-extract and rewrite it.
if [ -f "${EXTRACTED_STAMP}" ] && [ "$(cat "${EXTRACTED_STAMP}")" = "${SHA256}" ]; then
  echo "==> fixtures already extracted; skipping"
else
  echo "==> Extracting into ${DEST_DIR}/ ..."
  rm -rf "${FIXTURES_DIR}"
  # Only the trees the suites read: the blockchain_tests* ones are most of the
  # archive and nothing opens them.
  tar -xzf "${TARBALL}" -C "${DEST_DIR}" \
    fixtures/state_tests fixtures/transaction_tests
  echo "${SHA256}" > "${EXTRACTED_STAMP}"
fi

echo "==> Done. Fixtures at: ${FIXTURES_DIR}"
