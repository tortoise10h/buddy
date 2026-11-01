VERSION ?= v0.1.0
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w

.PHONY: all build test vet clean

all: build

build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/buddy-ctl ./cmd/buddy-ctl

test:
	go test -v ./...

vet:
	go vet ./...

clean:
	rm -rf bin/buddy-ctl dist
