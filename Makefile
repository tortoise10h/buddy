VERSION ?= v1.0.0
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X 'github.com/tortoise10h/buddy/internal/cli.Version=$(VERSION)' \
	-X 'github.com/tortoise10h/buddy/internal/cli.Commit=$(COMMIT)' \
	-X 'github.com/tortoise10h/buddy/internal/cli.Date=$(DATE)'

.PHONY: all build test vet clean benchmark parity

all: build

build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/buddy-ctl ./cmd/buddy-ctl

test:
	go test -v ./...

vet:
	go vet ./...

clean:
	rm -rf bin

benchmark:
	go test -bench=. -benchmem ./...

parity: build
	go test -v ./test/parity/...
