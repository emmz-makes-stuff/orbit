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

- [x] 5.1 Implement `internal/keychain.Store` over `security` (Set via `security -i` on stdin, with tokens outside `[A-Za-z0-9_-]+` rejected; Get, where exit 44 means none stored; Clear, with exit 44 treated as success). Verify with unit tests on the commands sent, checking that the token never appears in argv
- [x] 5.2 Implement `orbit auth` (stdin when piped, hidden prompt otherwise, empty input rejected, nothing echoed) and `orbit auth --clear`. Verify with command tests covering the `claude-auth` store, empty-input and clear scenarios

## 6. Create, provision and attach

- [x] 6.1 Implement the root command's create path: build the mounts (workspace first, then `rw` extras), check that extras exist before creating anything, create the VM, copy `ro` extras by tar stream (D8), run `baseline.sh` as root and then `.orbit/setup.sh` as the user, with output streamed and failures naming the script (D9), and write the provisioned marker. Verify with command tests against the fake client, covering the missing-extra, missing-baseline, setup-failure and ordering scenarios
- [x] 6.2 Implement the attach path: start if stopped, warn if the provisioned marker is missing, warn if the Mac's SSH agent is empty (`ssh-add -l` exits 1 or 2; this check also runs on create), read the token (warn if absent), set `CLAUDE_CODE_OAUTH_TOKEN` and `ORBENV`, then exec the shell in the current directory. Verify with command tests covering stopped-VM, subdirectory working dir, empty-agent, keys-loaded, token-present and token-absent scenarios
- [x] 6.3 Wire overlap refusal and the OrbStack-unavailable error into the root command. Verify with command tests for both error messages and the non-zero exits
- [x] 6.4 Refuse to create a workspace at the home directory or any ancestor of it (added after §6 at Emmz's request). Verify with command tests for `~`, `/`, `/Users`, and an allowed subdirectory
- [x] 6.5 Pass the VM's default user to `baseline.sh` as `ORBIT_USER` (added in §8 at Emmz's request, so the baseline can install per-user tools such as Claude Code). Verify with a command test that the baseline run sets it

## 7. Lifecycle commands

- [x] 7.1 Implement target selection (an explicit name that must be managed, or resolution from the current directory) shared by `stop`, `start`, `destroy`, `rebuild` and `sync`. Verify with unit tests for the implicit, unmanaged-name and no-workspace scenarios
- [x] 7.2 Implement `orbit list` (a table and `--json`, with orphans flagged). Verify with command tests covering the running/stopped, orphaned and JSON scenarios
- [x] 7.3 Implement `orbit stop` and `orbit start`, both succeeding when the VM is already in the requested state. Verify with command tests
- [x] 7.4 Implement `orbit destroy` (confirmation, `--yes`, refusal when stdin isn't a terminal and there's no `--yes`) and `orbit rebuild` (refusal for orphans, then destroy and the create path). Verify with command tests covering every `workspace-lifecycle` destroy and rebuild scenario
- [x] 7.5 Implement `orbit sync` (remove the old copies, then tar-stream the `ro` extras). Verify with a command test that the old copy is removed before extraction

## 8. Documentation

- [x] 8.1 Write `README.md`: install, `claude setup-token` + `orbit auth`, `ssh-add --apple-use-keychain` plus `ssh-add --apple-load-keychain` in `~/.zshrc`, the config file with examples, `baseline.sh` and `.orbit/setup.sh`, every command, and the security model (isolated machines share a kernel; push access and the open network are deliberate). Verify that each documented command matches `orbit --help`

## 9. Integration checks (manual, real OrbStack)

- [x] 9.1 Add `integration/` tests behind `//go:build integration` that create a scratch workspace and drive the real binary through create, provision, list, stop/start, sync, rebuild and destroy. They also assert that there's no `/mnt/mac`, that the workspace is writeable from the VM, that `ro` extra edits don't reach the Mac, that the token is in the shell environment and not on disk, and that the internet is reachable. Verify with `go test -tags integration ./integration/...` passing on Emmz's Mac (run by hand, not a gate)
- [x] 9.2 Push check: with a key loaded in the Mac's agent, `git push` to a scratch remote from inside a workspace VM succeeds. Record the result in a devlog note

## Workflow follow-up

- Run `/opsx:verify`, then archive the change once review is satisfied.

## 10. Review fixes: extras safety

- [x] 10.1 Refuse, on create, rebuild and sync, any two extras where one lies within the other and either is `rw` (ro-within-ro stays allowed: it can't touch the Mac). The message names both extras and the config file, and nothing is created or synced. Add a vm-access spec scenario. Test with a ro parent and a rw child, a rw parent and a ro child, and rw inside rw
- [x] 10.2 One extras check shared by `planFor` and `orbit sync`: existence, overlap with the workspace, overlap between extras, and the split into mounts and copies
- [x] 10.3 `copyExtras` reports the Mac-side archive error (e.g. an unreadable file) instead of discarding it, and fails even if tar in the VM exits 0. Test it with the fake

## 11. Review fixes: docs and nits

- [x] 11.1 Record the `ORBIT_CONFIG_DIR` override in `design.md ## Decisions` (why: tests and alternative setups; it is read only from the Mac's environment, so a VM can't set it). Add a scenario to the vm-access "Workspace content cannot widen access" requirement
- [x] 11.2 On attach, warn "never finished provisioning" only when the marker check exits non-zero. Any other `Run` failure gets its own warning naming the error. Test both
- [x] 11.3 README: a known-issue line saying workspaces under `/tmp` or `/var/folders` hit OrbStack's stale-delete behaviour (a VM can't recreate a file the Mac just deleted)

## 12. Review fixes: extras reach the VM user's home

- [x] 12.1 Map extras under the Mac home to the VM user's home: `~/<rel>` appears at `<vm home>/<rel>`, while other extras and the workspace keep their Mac path. Writeable ones are mounted with `--mount SRC:DEST`. The VM home is taken as `/home/<mac user>` at create, because orbctl needs DEST before the VM exists, and provisioning fails clearly if `getent passwd` says otherwise. Update the vm-access spec, design.md D8, the README and tests
- [x] 12.2 Missing parent directories of a copied extra under the VM home are owned by the default user, not root. Parents outside the home stay root-owned
- [x] 12.3 Integration test: assert the ro extra is readable as `~/<rel>` and the rw extra writeable as `~/<rel>` from the VM shell

## 13. Review fixes: copy and sync safety

- [x] 13.1 Refuse any extra that lies within another, whatever their modes. Update the vm-access "Overlapping extras" scenario and the README, and flip `TestNestedReadOnlyExtrasAllowed`
- [x] 13.2 `orbit sync` checks each copy's VM path against the VM's actual mounts (from `orbctl list`), and refuses if a copy would overlap a live mount, telling the user to rebuild. Test: created with an rw extra, config changed to ro, then sync is refused
- [x] 13.3 Skip sockets in read-only copies, with a warning naming each one, instead of failing the copy
- [x] 13.4 Only a not-exist error means there is no `.orbit/setup.sh`. Any other `Stat` error fails provisioning

## 14. Review fixes: errors and cleanup

- [x] 14.1 orbctl's own failures always print an `orbit:` message: `orb.output` stops wrapping `*ExitError`, and `cli.Run` drops the passthrough it no longer needs. Test a silent orbctl failure
- [x] 14.2 On attach, only exit 1 from the marker check means "never finished provisioning". Any other exit code gets the "could not check" warning
- [x] 14.3 `rebuild` keeps the VM's existing name
- [x] 14.4 `defaultUser` gets a clear message when `id -un` prints nothing
- [x] 14.5 `targetCmd` and `confirmedCmd` share one resolve-and-run path
