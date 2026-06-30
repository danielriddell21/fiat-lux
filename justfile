#!/usr/bin/env -S just --justfile
# fiat-lux task runner. Run `just` with no args to see the recipes.

set shell := ["bash", "-cu"]

binary := "fiatlux"
cmd    := "./cmd/fiatlux"

# list available recipes
default:
    @just --list

# static binary at ./fiatlux. Embeds the current git describe as version.
[group('build')]
build:
    go build -ldflags "-X 'main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)'" -o {{binary}} {{cmd}}

# race-enabled unit tests, no cache.
[group('test')]
test:
    go test -race -count=1 ./...

# golangci-lint with the project config.
[group('dev')]
lint:
    golangci-lint run

# vet the code.
[group('dev')]
vet:
    go vet ./...

# format the code.
[group('dev')]
fmt:
    gofmt -w .

# resolve dependencies.
[group('dev')]
tidy:
    go mod tidy

# full gate: lint + test + build. All must pass before committing.
[group('dev')]
ci: lint test build

# run the binary directly (no build artefact).
[group('run')]
run *args:
    go run {{cmd}} {{args}}

# headless web-only mode (no TUI). Pass extra flags after the recipe name.
[group('run')]
serve *args:
    go run {{cmd}} serve {{args}}

# local container build via the dev Dockerfile.
[group('build')]
image:
    docker build -t fiatlux:dev .

# coverage report (atomic mode so it composes with -race).
[group('test')]
cover:
    go test -race -coverprofile=coverage.txt -covermode=atomic ./...
    go tool cover -func=coverage.txt | tail -1

# GoReleaser dry-run: builds binaries + images locally, no push.
[group('dev')]
release-snapshot:
    goreleaser release --snapshot --clean --skip=announce,validate

# GoReleaser config validation.
[group('dev')]
release-check:
    goreleaser check

# remove build artefacts.
[group('dev')]
clean:
    rm -f {{binary}} coverage.txt coverage.html
