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

VERSION="${VERSION:-$(grep '^[[:space:]]*Version[[:space:]]*=' "${ROOT_DIR}/cmd/gossip/version.go" | head -1 | cut -d'"' -f2)}"
BUILD_DATE="${BUILD_DATE:-$(date +%Y-%m-%d)}"
BUILD_TIME="${BUILD_TIME:-$(date +%H:%M:%S)}"
GIT_COMMIT="${GIT_COMMIT:-$(git -C "${ROOT_DIR}" rev-parse --short HEAD 2>/dev/null || echo "unknown")}"
GO_VERSION="${GO_VERSION:-$(go version | cut -d' ' -f3)}"
OS="${OS:-linux}"
ARCH="${ARCH:-$(go env GOARCH)}"

mkdir -p "${DIST_DIR}"

(
  cd "${ROOT_DIR}"
  GOOS="${OS}" GOARCH="${ARCH}" go build \
    -ldflags "-X main.Version=${VERSION} -X main.BuildDate=${BUILD_DATE} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT} -X main.GoVersion=${GO_VERSION} -X main.BuildOS=${OS} -X main.BuildArch=${ARCH}" \
    -o "${DIST_DIR}/${BIN_NAME}" "${CMD_PATH}"
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
