# orbit — gate targets.
#
# Every gate prints its own exit code as `LABEL_EXIT:<n>` and exits with it, so a
# result can be quoted rather than inferred from output that may look clean.
# `gates` runs the whole set with -k, so one run reports every failure.

SHELL := /bin/bash

# Go-installed tools (staticcheck) live in $(go env GOPATH)/bin, which may not be on PATH.
export PATH := $(shell go env GOPATH 2>/dev/null)/bin:$(PATH)

CHANGES := $(notdir $(patsubst %/,%,$(filter-out %/archive/,$(wildcard openspec/changes/*/))))

.PHONY: build test format lint validate changes gates deps

build:
	@go build ./...; code=$$?; echo "BUILD_EXIT:$$code"; exit $$code

test:
	@go test ./...; code=$$?; echo "TEST_EXIT:$$code"; exit $$code

format:
	@out=$$(gofmt -l .); test -z "$$out"; code=$$?; [ -n "$$out" ] && echo "$$out"; echo "FORMAT_EXIT:$$code"; exit $$code

lint:
	@go vet ./... && staticcheck ./...; code=$$?; echo "LINT_EXIT:$$code"; exit $$code

# Validates every active change. No active change is a failure, not a silent pass.
validate:
	@fail=0; \
	if [ -z "$(CHANGES)" ]; then \
		echo "no active change found under openspec/changes/"; fail=1; \
	else \
		for c in $(CHANGES); do openspec validate $$c --strict || fail=1; done; \
	fi; \
	echo "VALIDATE_EXIT:$$fail"; exit $$fail

changes:
	@echo "$(CHANGES)"

gates:
	@$(MAKE) --no-print-directory -k build test format lint validate; code=$$?; \
	echo "GATES_EXIT:$$code"; exit $$code

# Every tool and login in docs/dependencies.md — keep the two in step. Not part of
# `gates`: it checks the machine, not the code, and auth checks may need the network.
deps:
	@fail=0; \
	go version 2>/dev/null | grep -qE 'go1\.(2[7-9]|[3-9][0-9])' || { echo "MISSING/WRONG VERSION go — need >=1.27: brew install go"; fail=1; }; \
	staticcheck -version >/dev/null 2>&1 || { echo "MISSING staticcheck — go install honnef.co/go/tools/cmd/staticcheck@latest"; fail=1; }; \
	openspec --version >/dev/null 2>&1 || { echo "MISSING openspec — npm i -g @fission-ai/openspec"; fail=1; }; \
	orbctl version 2>/dev/null | grep -qE 'Version: (2\.([2-9]|[1-9][0-9])|[3-9])\.' || { echo "MISSING/WRONG VERSION orbctl — need OrbStack >=2.2: brew install orbstack"; fail=1; }; \
	orbctl status >/dev/null 2>&1 || { echo "NOT RUNNING OrbStack — open OrbStack once"; fail=1; }; \
	command -v security >/dev/null 2>&1 || { echo "MISSING security — ships with macOS"; fail=1; }; \
	claude --version >/dev/null 2>&1 || { echo "MISSING claude — curl -fsSL https://claude.ai/install.sh | bash"; fail=1; }; \
	test -n "$$SSH_AUTH_SOCK" || { echo "MISSING ssh-agent — SSH_AUTH_SOCK is unset"; fail=1; }; \
	ssh-add -l >/dev/null 2>&1 || { echo "NOT AUTHENTICATED ssh-agent — ssh-add --apple-use-keychain ~/.ssh/<key>"; fail=1; }; \
	echo "DEPS_EXIT:$$fail"; exit $$fail
