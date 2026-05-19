BIN := gossipper
CMD := ./cmd/gossip
DIST := dist
VERSION ?= $(shell grep '^[[:space:]]*Version[[:space:]]*=' cmd/gossip/version.go | head -1 | cut -d'"' -f2)
BUILD_DATE := $(shell date +%Y-%m-%d)
BUILD_TIME := $(shell date +%H:%M:%S)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GO_VERSION := $(shell go version | cut -d' ' -f3)
OS ?= $(shell go env GOOS)
ARCH ?= $(shell go env GOARCH)
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildDate=$(BUILD_DATE) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT) -X main.GoVersion=$(GO_VERSION) -X main.BuildOS=$(OS) -X main.BuildArch=$(ARCH)"

.PHONY: all build build-go dynamic frontend package package-deb package-rpm benchmark bench-transport clean smoke smoke-webrtc

# Default `make`: Control UI (embed) then static binary in dist/.
.DEFAULT_GOAL := all
all: frontend build-go

# Control UI for -api_addr (embedded via go:embed); same role as Homer Makefile frontend target.
frontend:
	cd web/control-ui && npm ci && npm run build
	@git checkout -- internal/api/webdist/.gitkeep 2>/dev/null || : > internal/api/webdist/.gitkeep

build-go:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build $(LDFLAGS) -o $(DIST)/$(BIN) $(CMD)

build: all

dynamic: frontend
	mkdir -p $(DIST)
	GOOS=$(OS) GOARCH=$(ARCH) go build $(LDFLAGS) -o $(DIST)/$(BIN) $(CMD)

package:
	VERSION="$(VERSION)" ARCH="$(ARCH)" OS="$(OS)" scripts/build_package.sh all

package-deb:
	VERSION="$(VERSION)" ARCH="$(ARCH)" OS="$(OS)" scripts/build_package.sh deb

package-rpm:
	VERSION="$(VERSION)" ARCH="$(ARCH)" OS="$(OS)" scripts/build_package.sh rpm

benchmark:
	scripts/benchmark-sipp-vs-gossipper.sh "$${BENCH_TARGET:-127.0.0.1:5060}" "$${BENCH_CALLS:-1000}" "$${BENCH_RATE:-50}" "$${BENCH_CONCURRENT:-100}"

# UDP SharedUDP / reuseport benches (see scripts/bench_transport.sh).
bench-transport:
	bash scripts/bench_transport.sh

# Real-process /api/v2 smoke (requires dist/gossipper from build-go; frontend for GET /).
smoke:
	bash scripts/smoke-api-v2.sh

# Loopback WebRTC UAS+UAC supervisor jobs (builtin webrtc_uas/webrtc_uac).
smoke-webrtc:
	bash scripts/smoke-webrtc-loopback.sh

clean:
	rm -rf $(DIST)
