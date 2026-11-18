export GOCACHE := $(CURDIR)/.local/cache
export GOMODCACHE := $(CURDIR)/.local/mod
export GOTMPDIR := $(CURDIR)/.local/tmp
export TMPDIR := $(CURDIR)/.local/tmp
export GOLANGCI_LINT_CACHE := $(CURDIR)/.local/lint

.PHONY: prepare demo cyberpunk test lint build

prepare:
	mkdir -p .local/cache .local/mod .local/tmp .local/lint

demo: prepare
	go run . -demo

cyberpunk: prepare
	go run .

test: prepare
	go test -race -shuffle=on ./...

lint: prepare
	go vet ./...
	golangci-lint run ./...

build: prepare
	go build -o yamusic .
