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

.PHONY: build dynamic package package-deb package-rpm benchmark clean

build:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=$(OS) GOARCH=$(ARCH) go build $(LDFLAGS) -o $(DIST)/$(BIN) $(CMD)

dynamic:
	mkdir -p $(DIST)
	GOOS=$(OS) GOARCH=$(ARCH) go build $(LDFLAGS) -o $(DIST)/$(BIN) $(CMD)

package: package-deb package-rpm

package-deb:
	VERSION="$(VERSION)" ARCH="$(ARCH)" scripts/build-package.sh deb

package-rpm:
	VERSION="$(VERSION)" ARCH="$(ARCH)" scripts/build-package.sh rpm

benchmark:
	scripts/benchmark-sipp-vs-gossipper.sh "$${BENCH_TARGET:-127.0.0.1:5060}" "$${BENCH_CALLS:-1000}" "$${BENCH_RATE:-50}" "$${BENCH_CONCURRENT:-100}"

clean:
	rm -rf $(DIST)
