# Design

## Context

Greenfield: there's no code yet. Requirements are in `docs/DISCOVERY.md` and the behaviour is in this
change's specs. The foundational calls are ADR-0001 (OrbStack isolated machines), ADR-0002 (Go) and
ADR-0003 (OrbStack is the record; config split by trust). Runtime and build dependencies are in
`docs/dependencies.md`.

Verified locally against OrbStack 2.2.4. The spike results from §1 are in this change's `devlog/`.

- `orb create --isolated --mount SOURCE[:DEST] --forward-ssh-agent` exists. A mount **without** DEST
  lands at `/mnt/mac/<source>`, not at the same path. With an explicit `SRC:SRC` it appears at the same
  path, writeable both ways. Nothing else from the Mac is visible: `/mnt/mac` holds only the mounts'
  own paths, `/Users` is absent, and the `mac` command fails.
- Mounts can be changed with `orb config set machine.<name>.mounts` (comma-separated) plus a restart.
- There is no read-only mount option.
- `orbctl list -f json` gives `name`, `state` and `config` per machine, including
  `config.mounts: [{source, destination?}]` in creation order.
- `orbctl run -m <name> -w <dir>` with no command opens the default user's login shell (`-bash`) in
  `<dir>`. If `<dir>` doesn't exist in the VM, it silently starts in `/` with exit 0. Exit codes pass
  through.
- `ORBENV=VAR1:VAR2` forwards those Mac environment variables into `orbctl run`, isolated machines
  included. Without `ORBENV` they don't arrive.
- `security -i` takes Keychain commands on stdin and passes their exit codes through. "Item not found"
  exits 44.
- `claude setup-token` exists (Claude Code 2.1.312).

## Goals / Non-Goals

**Goals:**

- A single static `orbit` binary whose attach path is a few `orbctl` calls and then `exec`.
- All OrbStack and Keychain access behind interfaces, so every spec scenario that isn't about real-VM
  effects can be unit-tested with fakes.
- A short spike phase that confirms the OrbStack and Keychain behaviours this design depends on, before
  building on them.

**Non-Goals:**

- Packaging and distribution (Homebrew), scoped git credentials, other agents: *Later* in discovery.
- Network restriction: explicitly out.
- Windows or Linux hosts: explicitly out.
- Shell completion and man pages.

## Decisions

### D1. Language and CLI: Go ≥ 1.27, cobra, stdlib `testing` (ADR-0002)

`orbit` is a module at the repo root with `cmd/orbit/main.go`, and packages under `internal/`. cobra
provides the command tree: root (attach), `auth`, `list`, `start`, `stop`, `destroy`, `rebuild` and
`sync`. The root command takes no positional arguments.
*Rejected:* .NET 10 Native AOT (per-arch publishes, heavier toolchain), a shell script (fragile JSON
and path logic, weaker tests), stdlib `flag` (no subcommand help).

### D2. Gate commands, run from the repo root

- **build:** `go build ./...`
- **test:** `go test ./...`
- **fmt (check):** `test -z "$(gofmt -l .)"`
- **lint:** `go vet ./... && staticcheck ./...`

All are non-interactive and exit non-zero on failure. `gofmt -l` prints offending files, and `test -z`
turns that into an exit code. There is a single stack, so there's no target prefix.
*Rejected:* golangci-lint (more config for little gain at this size), vet only (staticcheck catches
real bugs that vet misses).

### D3. Workspace VMs are isolated OrbStack machines (ADR-0001)

Create with `orb create --isolated --forward-ssh-agent --mount <ws>:<ws> [--mount <extra>:<extra>]...
<distro> <name>`. Every mount names its destination explicitly as its own Mac path. Without a DEST,
OrbStack puts it under `/mnt/mac/`. `isolate_network` is left off, so
the network stays open. Distro and resource limits come from config (D7). The default is `ubuntu`,
which means OrbStack's default Ubuntu.
*Rejected:* non-isolated machines (whole Mac filesystem shared), Docker containers (not a full VM).

### D4. OrbStack is the record (ADR-0003)

- **Managed** means the machine name starts with `orbit-`.
- **Name:** `orbit-<basename>-<h>`, where `<basename>` is the workspace's basename lowercased and cut
  down to `[a-z0-9-]`, and `<h>` is the first 6 hex digits of the SHA-256 of the canonical workspace
  path. That's deterministic, readable in OrbStack's UI, and collisions are vanishingly unlikely.
- **Workspace path** is `config.mounts[0].source` from `orbctl list -f json`, so a single list call
  resolves everything. `orbit` always passes the workspace mount first, and extras after it.
- **Resolution:** canonicalise the current directory with `filepath.EvalSymlinks` and `filepath.Abs`.
  List the managed machines and read each workspace path. Pick the longest workspace path that equals
  the current directory or is a path-segment prefix of it (so `/a/b` covers `/a/b/c` but not
  `/a/bc`). If there's none, and any workspace has the current directory as a strict prefix, refuse
  (overlap). Otherwise create.
- **Orphaned** means `os.Stat` of the workspace path fails.
- Workspace and extra paths containing `:` are rejected at create time, because `--mount` uses it to
  separate SOURCE from DEST. Paths containing `,` are also rejected, so a mounts list edited by hand
  with `orb config set` stays unambiguous.

*Rejected:* a state file in `~/.config/orbit`, because it drifts from OrbStack.

### D5. Attach is `exec orbctl run`

After any needed start, `orbit` replaces itself (`syscall.Exec`) with
`orbctl run -m <name> -w <cwd>`, which runs the default user's shell. Replacing the process passes the
terminal, signals and exit code straight through, with no PTY plumbing. Concurrent shells are just
concurrent `orbctl run` processes. Starting a stopped VM is `orbctl start <name>` beforehand.
`orbctl run` silently falls back to `/` when `-w` doesn't exist in the VM, so `orbit` only ever passes
a directory it has resolved to lie within the workspace mount. The current directory always does, by
construction.
*Rejected:* `ssh <name>@orb` (needs SSH config and `AcceptEnv` for the token), and running `orbctl` as
a child process (orbit would have to relay signals and exit codes).

### D6. Claude token: Keychain on the Mac, `ORBENV` into the shell

- **Store:** `orbit auth` reads the token (from stdin when piped, otherwise from a hidden prompt via
  `golang.org/x/term`). It sends `add-generic-password -U -s orbit -a claude-oauth-token -w <token>`
  to `security -i` **on stdin**, so the token never appears in a process argument list. `security -i`
  parses its own quoting, so the token must match `[A-Za-z0-9_-]+` (real `sk-ant-oat01-…` tokens do).
  Anything else is rejected before it reaches `security`.
- **Clear:** `security delete-generic-password -s orbit -a claude-oauth-token`. Exit 44 (not found)
  counts as success.
- **Read:** `security find-generic-password -s orbit -a claude-oauth-token -w`, at every attach. Exit 44
  means no token is stored. Any other non-zero exit is an error.
- **Inject:** before the `exec`, set `CLAUDE_CODE_OAUTH_TOKEN` in orbit's environment and add it to
  `ORBENV`, keeping any `ORBENV` entries already present. The token lives only in the process
  environment of the shell and its children, and is never in argv or on the VM's disk.
- If no token is stored, warn on stderr and attach without it.

*Rejected:* writing the token into the VM (it's on disk, and goes stale on rotation), and the
`go-keychain` library (needs cgo).

### D7. Configuration split by trust (ADR-0003)

`~/.config/orbit/config.toml` lives on the Mac only, parsed with `github.com/BurntSushi/toml`:

```toml
distro = "ubuntu"          # optional; OrbStack image, e.g. "ubuntu:24.04"
cpus = 4                   # optional
memory = "8G"              # optional
disk = "64G"               # optional

[[extras]]                 # global: every workspace VM
path = "~/.gitconfig"      # mode defaults to "ro" (copied in)

[[workspaces."~/github/emmz".extras]]
path = "~/notes/emmz"
mode = "rw"                # mounted writeable
```

`~` expands to the Mac home directory. Workspace keys are canonicalised the same way as in D4. A
workspace's extras are added to the global ones. `~/.config/orbit/baseline.sh` is the common baseline.
The only workspace file `orbit` reads is `<workspace>/.orbit/setup.sh`, and only as a script to run
in the VM. `orbit` never parses config from the workspace. A missing config file is the same as an
empty one.
*Rejected:* config in the workspace (Claude could widen access), YAML (heavier parser, and
indentation mistakes in a hand-edited file).

**Override:** if `$ORBIT_CONFIG_DIR` is set, both files are read from it instead of
`~/.config/orbit`. It lets the integration tests, or anyone trying out a config, use a scratch
directory without touching the real one. It is read from the environment orbit runs in on the Mac.
Nothing in a workspace is consulted, and a VM can't set it.
*Rejected:* a `--config` flag (it has to reach every command, and is easy to forget on one, such as
`sync` with a different config from the one used at create); redirecting `$HOME` in tests (that also
moves `~` expansion and the home guard).

### D8. Read-only extras are copied by a tar stream

On create, rebuild and `orbit sync`, `orbit` runs `tar -C / -cf - <paths relative to />` on the Mac,
piped into `orbctl run -m <name> -u root sh -c 'tar -C / -xf - && chown -R <user> <paths>'`. Before
extracting, it removes each extra's previous copy in the VM, so the copy matches the Mac. `tar` keeps
the directory structure and file modes, and there's no extra tool to install. The Mac side is never
written to.
*Rejected:* a read-only remount inside the VM (sudo in the VM can undo it), and `orb push`
(one file at a time, and less control over ownership).

### D9. Provisioning: orbit runs the scripts with `orbctl run`, not cloud-init

After creation, `orbit` pipes `baseline.sh` into `orbctl run -m <name> -u root bash -s` and streams its
output to the terminal. It then pipes `.orbit/setup.sh` into `orbctl run -m <name> -w <workspace>
bash -s`, as the default user. Each step's exit code is checked, and a failure names the script.
After success, `orbit` writes `/var/lib/orbit/provisioned` in the VM. An attach that finds the
marker missing warns that provisioning didn't complete and suggests `orbit rebuild`.
This **supersedes** ADR-0001's `--user-data` point: cloud-init runs asynchronously after
`orb create` returns, so meeting "the first shell opens only once provisioning has finished" and
"say which script failed and show its output" would mean polling `cloud-init status` and parsing its
logs.
*Rejected:* cloud-init user data (asynchronous, and failures are buried in logs).

### D10. Testing seams

- `internal/orb`: a `Client` interface with List (machines with their state and mounts), Create, Start, Stop, Delete, Run (with
  stdin, stdout and stderr) and ExecShell. The real implementation shells out to `orbctl`.
- `internal/keychain`: a `Store` interface with Get, Set and Clear. The real implementation is
  `security`.
- `internal/workspace`: resolution, overlap, naming and orphan logic, as pure functions over
  `[]Machine` plus a stat function.
- `internal/config`: loading and merging the TOML config.

Unit tests cover every spec scenario that can be expressed against these fakes. Real-VM checks live
in `integration/` behind the build tag `integration` (`go test -tags integration ./integration/...`).
They're run by hand and are not part of the gates, because they need OrbStack and take minutes.

## Risks / Trade-offs

- **[`ORBENV` might not reach isolated machines, or `orbctl run` with no command might not open a
  login shell]** → Spike task 1.2 checks both before attach is built. Fallback: exec
  `orbctl run ... env CLAUDE_CODE_OAUTH_TOKEN=… bash -l`. That puts the token in argv inside the
  VM, so it would need an update to the spec's wording and Emmz's agreement.
- **[`security -i` might not accept a token on stdin the way D6 assumes]** → Spike task 1.3. Fallback:
  cgo `go-keychain`.
- **[Isolation is a shared kernel, not a full VM boundary]** → This was accepted in discovery and
  ADR-0001. It's documented in the README.
- **[Full git push access, skipped permissions and an open network]** → Accepted risk (discovery open
  question 1). Scoped credentials are *Later*.
- **[The Mac's SSH agent is empty after every reboot]** → Push from a VM fails until a key is loaded.
  On create and attach, `orbit` runs `ssh-add -l`. Exit 1 (no identities) or 2 (no agent) produces
  a warning naming `ssh-add --apple-use-keychain`, and `orbit` continues. `make deps` also flags it,
  and the README documents `ssh-add --apple-load-keychain` in `~/.zshrc` to reload the key at login.
- **[Workspace identified by the first mount entry]** → If someone edits mounts by hand in OrbStack,
  resolution could break. `orbit` treats a managed machine with no mounts as orphaned rather than
  crashing.
- **[Read-only copies go stale]** → `orbit sync`, plus a re-copy on rebuild.
- **[Hash-derived names don't survive a directory move]** → That's intended: the VM shows as
  orphaned. Moving a workspace means rebuilding it.

## Migration Plan

No existing users or data. Install with `go install ./cmd/orbit`, or build and copy the binary onto
the PATH. Rollback is `orbit destroy` for each workspace (or deleting the `orbit-*` machines), then
removing the binary and `~/.config/orbit`.

## Open Questions

- Default CPU, memory and disk limits. For now, OrbStack's shared defaults apply when config is unset.
  These can be tuned later without changing the specs.
- A number for "fast attach". It can be measured once attach works, and doesn't change the approach.
