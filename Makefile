BIN := gossipper
CMD := ./cmd/gossip
DIST := dist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || git rev-parse --short HEAD)
OS ?= $(shell go env GOOS)
ARCH ?= $(shell go env GOARCH)

.PHONY: build package package-deb package-rpm clean

build:
	mkdir -p $(DIST)
	GOOS=$(OS) GOARCH=$(ARCH) go build -o $(DIST)/$(BIN) $(CMD)

package: package-deb package-rpm

package-deb:
	VERSION="$(VERSION)" ARCH="$(ARCH)" scripts/build-package.sh deb

package-rpm:
	VERSION="$(VERSION)" ARCH="$(ARCH)" scripts/build-package.sh rpm

clean:
	rm -rf $(DIST)
