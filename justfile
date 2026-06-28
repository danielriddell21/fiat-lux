#!/usr/bin/env -S just --justfile
# fiat-lux task runner. Run `just` with no args to see the recipes.

set shell := ["bash", "-cu"]

binary := "fiatlux"
cmd    := "./cmd/fiatlux"

# Default: list recipes.
default:
    @just --list --unsorted

# Static binary at ./fiatlux. Embeds the current git describe as version.
[group('build')]
build:
    go build -ldflags "-X 'main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)'" -o {{binary}} {{cmd}}

# Run the binary directly (no build artefact).
[group('run')]
run *args:
    go run {{cmd}} {{args}}

# Headless web-only mode (no TUI). Pass extra flags after the recipe name.
[group('run')]
serve *args:
    go run {{cmd}} serve {{args}}

# Local container build via the dev Dockerfile.
[group('build')]
image:
    docker build -t fiatlux:dev .

# GoReleaser dry-run: builds binaries + images locally, no push.
[group('dev')]
release-snapshot:
    goreleaser release --snapshot --clean --skip=announce,validate

# GoReleaser config validation.
[group('dev')]
release-check:
    goreleaser check

# Race-enabled unit tests, no cache.
[group('test')]
test:
    go test -race -count=1 ./...

# Coverage report (atomic mode so it composes with -race).
[group('test')]
cover:
    go test -race -coverprofile=coverage.txt -covermode=atomic ./...
    go tool cover -func=coverage.txt | tail -1

# Static analysis: go vet plus golangci-lint with the project config.
[group('dev')]
lint:
    go vet ./...
    golangci-lint run

# Resolve dependencies.
[group('dev')]
tidy:
    go mod tidy

# Remove build artefacts.
[group('dev')]
clean:
    rm -f {{binary}} coverage.txt coverage.html

# Quick "should I commit this?" gate.
[group('dev')]
check: lint test build
