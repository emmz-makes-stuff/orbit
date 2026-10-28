# ADR-0002: orbit is written in Go

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

`orbit` is a macOS command-line tool that mostly runs other commands (`orbctl`, `security`). It must
attach "near-instantly" (success criterion: fast attach), and it should be publishable to other
OrbStack users later.

## Decision

Go (≥ 1.27), using `github.com/spf13/cobra` for the command tree and stdlib `testing` for tests. It's
distributed as a single static binary (a Homebrew formula later).

## Consequences

- Startup takes milliseconds, and there's no runtime to install.
- `orbctl` and the Keychain sit behind Go interfaces, so unit tests never touch OrbStack.
- Gates: `go build ./...`, `go test ./...`, `test -z "$(gofmt -l .)"`, `go vet ./... && staticcheck ./...`.

## Alternatives rejected

- **.NET 10 Native AOT.** It's Emmz's house language and can produce a fast single binary, but it
  needs per-arch publishes and a heavier toolchain for a small exec-heavy tool.
- **A shell script.** JSON handling, path ancestry and Keychain logic get fragile. Testing (bats) and
  shipping it to others are weaker.
