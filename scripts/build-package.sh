#!/usr/bin/env bash

set -euo pipefail

PACKAGER="${1:-deb}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"
BIN_NAME="gossipper"
CMD_PATH="./cmd/gossip"

case "${PACKAGER}" in
  deb) EXT="deb" ;;
  rpm) EXT="rpm" ;;
  *)
    echo "unsupported packager: ${PACKAGER}" >&2
    echo "usage: scripts/build-package.sh [deb|rpm]" >&2
    exit 1
    ;;
esac

if ! command -v nfpm >/dev/null 2>&1; then
  if ! command -v docker >/dev/null 2>&1; then
    echo "nfpm was not found in PATH and docker is unavailable" >&2
    exit 1
  fi
fi

VERSION="${VERSION:-$(git -C "${ROOT_DIR}" describe --tags --always --dirty 2>/dev/null || git -C "${ROOT_DIR}" rev-parse --short HEAD)}"
OS="${OS:-linux}"
ARCH="${ARCH:-$(go env GOARCH)}"

mkdir -p "${DIST_DIR}"

(
  cd "${ROOT_DIR}"
  GOOS="${OS}" GOARCH="${ARCH}" go build -o "${DIST_DIR}/${BIN_NAME}" "${CMD_PATH}"
  if command -v nfpm >/dev/null 2>&1; then
    nfpm package \
      --config "${ROOT_DIR}/nfpm.yaml" \
      --packager "${PACKAGER}" \
      --target "${DIST_DIR}/${BIN_NAME}_${VERSION}_${OS}_${ARCH}.${EXT}"
  else
    docker run --rm \
      -e VERSION="${VERSION}" \
      -e OS="${OS}" \
      -e ARCH="${ARCH}" \
      -v "${ROOT_DIR}:/work" \
      -w /work \
      goreleaser/nfpm package \
      --config /work/nfpm.yaml \
      --packager "${PACKAGER}" \
      --target "/work/dist/${BIN_NAME}_${VERSION}_${OS}_${ARCH}.${EXT}"
  fi
)

echo "package created: ${DIST_DIR}/${BIN_NAME}_${VERSION}_${OS}_${ARCH}.${EXT}"
