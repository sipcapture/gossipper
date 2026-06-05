#!/usr/bin/env bash
# Build script mirroring .github/workflows/release.yml (local nfpm packages).
set -euo pipefail

PACKAGE="gossipper"
BINARY="gossipper"
NFPM_VERSION="2.41.1"

BUILD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${BUILD_DIR}/dist"
VERSION_FILE="${BUILD_DIR}/cmd/gossip/version.go"

OS="${OS:-linux}"
ARCH="${ARCH:-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')}"

# deb | rpm | all (default: all — same spirit as Homer local script)
PACKAGER="${1:-all}"

case "${PACKAGER}" in
  deb | rpm | all | binary) ;;
  *)
    echo "usage: $0 [deb|rpm|all|binary]" >&2
    exit 1
    ;;
esac

# nfpm packages are linux-only; on macOS build the binary and skip deb/rpm.
if [ "${OS}" != "linux" ] && [ "${PACKAGER}" != "binary" ]; then
  echo "==> OS=${OS}: compiling binary only (deb/rpm require OS=linux)" >&2
  PACKAGER="binary"
fi

# ── Remove old packages in dist/ ───────────────────────────────────────────────
mkdir -p "${DIST_DIR}"
shopt -s nullglob
for f in "${DIST_DIR}/${PACKAGE}"_*.deb "${DIST_DIR}/${PACKAGE}"_*.rpm; do
  rm -f "$f"
done
shopt -u nullglob

# ── Version (from version.go — source of truth) ───────────────────────────────
VERSION="${VERSION:-}"
if [ -z "${VERSION}" ]; then
  VERSION=$(grep '^[[:space:]]*Version[[:space:]]*=' "${VERSION_FILE}" 2>/dev/null | head -1 | cut -d'"' -f2)
fi
if [ -z "${VERSION}" ]; then
  VERSION=$(git -C "${BUILD_DIR}" describe --tags --exact-match 2>/dev/null | sed 's/^v//' || true)
fi
VERSION="${VERSION:-0.0.0}"

GIT_COMMIT=$(git -C "${BUILD_DIR}" rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE=$(date +%Y-%m-%d)
BUILD_TIME=$(date +%H:%M:%S)
GO_VERSION=$(go version | cut -d' ' -f3)

echo "==> Building ${PACKAGE} ${VERSION} (commit ${GIT_COMMIT}, ${OS}/${ARCH})"

# ── Frontend (embedded Control UI) ────────────────────────────────────────────
if [ -d "${BUILD_DIR}/web/control-ui" ]; then
  echo "==> Checking Node.js (Control UI) ..."
  if ! node -e '
    const [a, b, c = 0] = process.version.slice(1).split(".").map(Number);
    const ok =
      a >= 23 ||
      (a === 22 && (b > 12 || (b === 12 && c >= 12))) ||
      (a === 21) ||
      (a === 20 && (b > 19 || (b === 19 && c >= 0)));
    if (!ok) {
      console.error("Node.js " + process.version + " is too old for this UI (Vite + Tailwind v4).");
      console.error("Use Node.js 20.19+ or 22.12+ (e.g. nvm install 22 && nvm use 22), then:");
      console.error("  rm -rf web/control-ui/node_modules && npm ci --prefix web/control-ui");
      process.exit(1);
    }
  '; then
    exit 1
  fi
  echo "==> Building frontend ..."
  (cd "${BUILD_DIR}/web/control-ui" && npm ci --silent && npm run build --silent)
  cd "${BUILD_DIR}"
  git checkout -- internal/api/webdist/.gitkeep 2>/dev/null || : >internal/api/webdist/.gitkeep
fi

# ── Compile gossipper (static binary, same flags as CI / Makefile build-go) ─
echo "==> Compiling ${BINARY} ..."
cd "${BUILD_DIR}"
CGO_ENABLED=0 GOOS="${OS}" GOARCH="${ARCH}" go build \
  -ldflags "-X main.Version=${VERSION} -X main.BuildDate=${BUILD_DATE} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT} -X main.GoVersion=${GO_VERSION} -X main.BuildOS=${OS} -X main.BuildArch=${ARCH}" \
  -o "${DIST_DIR}/${BINARY}" ./cmd/gossip

echo "    Binary: $(ls -lh "${DIST_DIR}/${BINARY}" | awk '{print $5, $9}')"

if [ "${PACKAGER}" = "binary" ]; then
  mv -f "${DIST_DIR}/${BINARY}" "${DIST_DIR}/${BINARY}_${OS}_${ARCH}"
  echo "==> Done: ${DIST_DIR}/${BINARY}_${OS}_${ARCH}"
  exit 0
fi

# ── Download nfpm (same version as CI) ─────────────────────────────────────────
NFPM="${BUILD_DIR}/nfpm"
if [ ! -x "${NFPM}" ]; then
  echo "==> Downloading nfpm v${NFPM_VERSION} ..."
  NFPM_ARCH="x86_64"
  [ "${ARCH}" = "arm64" ] && NFPM_ARCH="arm64"
  if command -v wget >/dev/null 2>&1; then
    wget -qO- "https://github.com/goreleaser/nfpm/releases/download/v${NFPM_VERSION}/nfpm_${NFPM_VERSION}_Linux_${NFPM_ARCH}.tar.gz" \
      | tar --directory "${BUILD_DIR}" -xz nfpm
  elif command -v curl >/dev/null 2>&1; then
    curl -fsSL "https://github.com/goreleaser/nfpm/releases/download/v${NFPM_VERSION}/nfpm_${NFPM_VERSION}_Linux_${NFPM_ARCH}.tar.gz" \
      | tar --directory "${BUILD_DIR}" -xz nfpm
  else
    echo "error: need wget or curl to download nfpm (or place an nfpm binary at ${NFPM})" >&2
    exit 1
  fi
  chmod +x "${NFPM}"
fi

run_pack() {
  local ext=$1
  echo "==> Packaging ${BINARY}_${VERSION}_${OS}_${ARCH}.${ext} ..."
  VERSION="${VERSION}" ARCH="${ARCH}" OS="${OS}" \
    "${NFPM}" package \
    --config "${BUILD_DIR}/nfpm.yaml" \
    --packager "${ext}" \
    --target "${DIST_DIR}/${BINARY}_${VERSION}_${OS}_${ARCH}.${ext}"
}

cd "${BUILD_DIR}"
case "${PACKAGER}" in
  all)
    run_pack deb
    run_pack rpm
    echo ""
    echo "==> Done:"
    ls -lh "${DIST_DIR}/${BINARY}_${VERSION}_${OS}_${ARCH}".{deb,rpm} 2>/dev/null || true
    ;;
  deb) run_pack deb ;;
  rpm) run_pack rpm ;;
esac
