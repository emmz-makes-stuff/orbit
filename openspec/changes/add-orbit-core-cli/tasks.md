# Tasks

## 1. Scaffold and spikes

- [x] 1.1 Create the Go module (`go mod init`, go 1.27), `cmd/orbit/main.go` with a cobra root that prints help, and empty `internal/{orb,keychain,workspace,config}` packages. Verify that all four gates pass: `go build ./...`, `go test ./...`, `test -z "$(gofmt -l .)"`, `go vet ./... && staticcheck ./...`
- [x] 1.2 Spike (manual, scratch isolated machine): confirm that `orb create --isolated --forward-ssh-agent --mount <dir>` mounts `<dir>` writeable at the same path with no `/mnt/mac`, that `orbctl config get machine.<n>.mounts` lists it first, that `orbctl run -m <n> -w <dir>` with no command opens a login shell, and that `ORBENV=CLAUDE_CODE_OAUTH_TOKEN` reaches that shell. Record the results in a devlog note. If any fails, stop and take the D5/D6 fallback to Emmz
- [x] 1.3 Spike: confirm that `security -i` accepts `add-generic-password -U -s orbit-spike -a t -w <value>` on stdin, that `find-generic-password -w` returns it, and that `delete-generic-password` removes it. Record the results in a devlog note. If it fails, stop and raise the `go-keychain` fallback

## 2. Configuration

- [x] 2.1 Implement `internal/config`: load `~/.config/orbit/config.toml` (a missing file means empty config), expand `~`, canonicalise workspace keys, merge global and per-workspace extras, default extra mode to `ro`, and reject unknown modes and paths containing `:` or `,`. Verify with table tests for each case
- [x] 2.2 Verify the "workspace content cannot widen access" requirement: a unit test shows that config loading reads only `~/.config/orbit`, and that a `.orbit/config.toml` in a workspace has no effect

## 3. Workspace resolution

- [x] 3.1 Implement `internal/workspace` naming (`orbit-<basename>-<h6>`) and resolution (exact match, nearest ancestor with a path-segment prefix, overlap refusal, ignoring unmanaged machines, orphan detection via an injected stat). Verify with unit tests covering every `workspace-sessions` resolution and overlap scenario, plus `/a/b` vs `/a/bc`

## 4. OrbStack client

- [x] 4.1 Implement `internal/orb.Client` over `orbctl`: List (JSON, including `config.mounts`), Create (isolated, forward-ssh-agent, ordered `SRC:SRC` mounts, distro and limits), Start, Stop, Delete, Run (stdin, stdout and stderr wired), and ExecShell (`syscall.Exec` with env). Also detect a missing `orbctl` with a clear error. Verify with unit tests on the argument building and JSON parsing, using a fake runner
- [x] 4.2 Add a fake `Client` for use by command tests, and verify it compiles against the interface

## 5. Claude auth

- [ ] 5.1 Implement `internal/keychain.Store` over `security` (Set via `security -i` on stdin, with tokens outside `[A-Za-z0-9_-]+` rejected; Get, where exit 44 means none stored; Clear, with exit 44 treated as success). Verify with unit tests on the commands sent, checking that the token never appears in argv
- [ ] 5.2 Implement `orbit auth` (stdin when piped, hidden prompt otherwise, empty input rejected, nothing echoed) and `orbit auth --clear`. Verify with command tests covering the `claude-auth` store, empty-input and clear scenarios

## 6. Create, provision and attach

- [ ] 6.1 Implement the root command's create path: build the mounts (workspace first, then `rw` extras), check that extras exist before creating anything, create the VM, copy `ro` extras by tar stream (D8), run `baseline.sh` as root and then `.orbit/setup.sh` as the user, with output streamed and failures naming the script (D9), and write the provisioned marker. Verify with command tests against the fake client, covering the missing-extra, missing-baseline, setup-failure and ordering scenarios
- [ ] 6.2 Implement the attach path: start if stopped, warn if the provisioned marker is missing, warn if the Mac's SSH agent is empty (`ssh-add -l` exits 1 or 2; this check also runs on create), read the token (warn if absent), set `CLAUDE_CODE_OAUTH_TOKEN` and `ORBENV`, then exec the shell in the current directory. Verify with command tests covering stopped-VM, subdirectory working dir, empty-agent, keys-loaded, token-present and token-absent scenarios
- [ ] 6.3 Wire overlap refusal and the OrbStack-unavailable error into the root command. Verify with command tests for both error messages and the non-zero exits

## 7. Lifecycle commands

- [ ] 7.1 Implement target selection (an explicit name that must be managed, or resolution from the current directory) shared by `stop`, `start`, `destroy`, `rebuild` and `sync`. Verify with unit tests for the implicit, unmanaged-name and no-workspace scenarios
- [ ] 7.2 Implement `orbit list` (a table and `--json`, with orphans flagged). Verify with command tests covering the running/stopped, orphaned and JSON scenarios
- [ ] 7.3 Implement `orbit stop` and `orbit start`, both succeeding when the VM is already in the requested state. Verify with command tests
- [ ] 7.4 Implement `orbit destroy` (confirmation, `--yes`, refusal when stdin isn't a terminal and there's no `--yes`) and `orbit rebuild` (refusal for orphans, then destroy and the create path). Verify with command tests covering every `workspace-lifecycle` destroy and rebuild scenario
- [ ] 7.5 Implement `orbit sync` (remove the old copies, then tar-stream the `ro` extras). Verify with a command test that the old copy is removed before extraction

## 8. Documentation

- [ ] 8.1 Write `README.md`: install, `claude setup-token` + `orbit auth`, `ssh-add --apple-use-keychain` plus `ssh-add --apple-load-keychain` in `~/.zshrc`, the config file with examples, `baseline.sh` and `.orbit/setup.sh`, every command, and the security model (isolated machines share a kernel; push access and the open network are deliberate). Verify that each documented command matches `orbit --help`

## 9. Integration checks (manual, real OrbStack)

- [ ] 9.1 Add `integration/` tests behind `//go:build integration` that create a scratch workspace and drive the real binary through create, provision, list, stop/start, sync, rebuild and destroy. They also assert that there's no `/mnt/mac`, that the workspace is writeable from the VM, that `ro` extra edits don't reach the Mac, that the token is in the shell environment and not on disk, and that the internet is reachable. Verify with `go test -tags integration ./integration/...` passing on Emmz's Mac (run by hand, not a gate)
- [ ] 9.2 Push check: with a key loaded in the Mac's agent, `git push` to a scratch remote from inside a workspace VM succeeds. Record the result in a devlog note

## Workflow follow-up

- Run `/opsx:verify`, then archive the change once review is satisfied.
