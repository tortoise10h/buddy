VERSION ?= v1.0.0
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X 'github.com/tortoise10h/buddy/internal/cli.Version=$(VERSION)' \
	-X 'github.com/tortoise10h/buddy/internal/cli.Commit=$(COMMIT)' \
	-X 'github.com/tortoise10h/buddy/internal/cli.Date=$(DATE)'

PREFIX ?= $(HOME)/.local/bin
INSTALL_NAME ?= buddy-ctl-go

.PHONY: all build test vet clean benchmark parity install uninstall cross checksums dist

all: build

build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/buddy-ctl ./cmd/buddy-ctl

install: build
	install -d $(PREFIX)
	install -m 0755 bin/buddy-ctl $(PREFIX)/$(INSTALL_NAME)

uninstall:
	rm -f $(PREFIX)/$(INSTALL_NAME)

cross:
	@mkdir -p dist
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/buddy-ctl_darwin_arm64 ./cmd/buddy-ctl
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/buddy-ctl_darwin_amd64 ./cmd/buddy-ctl
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/buddy-ctl_linux_amd64 ./cmd/buddy-ctl
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o dist/buddy-ctl_linux_arm64 ./cmd/buddy-ctl

checksums:
	@mkdir -p dist
	(cd dist && shasum -a 256 buddy-ctl_* > checksums.txt)

dist: cross checksums

test:
	go test -v ./...

vet:
	go vet ./...

clean:
	rm -rf bin/buddy-ctl dist

benchmark:
	go test -bench=. -benchmem ./...

parity: build
	go test -v ./test/parity/...
