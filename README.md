# orbit

Run Claude in an isolated [OrbStack](https://orbstack.dev) VM, one VM per workspace directory.

```console
$ cd ~/github/emmz
$ orbit
orbit: creating orbit-emmz-3fa9c1 for /Users/emmz/github/emmz
orbit: provisioning: running /Users/emmz/.config/orbit/baseline.sh
…
emmz@orbit-emmz-3fa9c1:~/github/emmz$ claude --dangerously-skip-permissions
```

Run `orbit` in a directory, and you get a shell in that directory's VM:

- If no VM covers the directory yet, orbit creates one. The VM can see only this directory
  (writeable) and the extras you allowlist. orbit provisions it, then opens the shell.
- If a VM already exists for the directory, or for a parent of it, orbit attaches to that VM and
  starts the shell in the current directory. A folder of several repo clones shares one VM.
- If the VM is stopped, orbit starts it first. You can have several shells in one VM at once.

orbit never starts Claude itself. You get a shell, and run `claude` yourself.

## Requirements

- macOS with **OrbStack ≥ 2.2** (`brew install orbstack`, then open OrbStack once).
- **Go ≥ 1.27**, to build orbit.
- **Claude Code** on the Mac, to create a token with `claude setup-token`.
- An **SSH agent with your key loaded**, if you want to `git push` from inside a VM (see below).

## Install

From a clone of this repository:

```sh
go install ./cmd/orbit
```

That puts `orbit` in `$(go env GOPATH)/bin` (usually `~/go/bin`). Make sure that directory is on your
`PATH`.

## One-time setup

### Claude login

Create a long-lived token on your Mac and hand it to orbit:

```sh
claude setup-token          # prints a token
orbit auth                  # paste it at the hidden prompt
# or: pbpaste | orbit auth
```

orbit stores the token in your macOS login Keychain. Every shell orbit opens gets it as
`CLAUDE_CODE_OAUTH_TOKEN`, through OrbStack's environment forwarding (`ORBENV`). It is never written
to a VM's disk, and never appears on a command line.

- **Rotating:** run `orbit auth` again. Every VM, old or new, gets the new token on its next attach.
- **Removing:** run `orbit auth --clear`.
- **No token stored:** orbit warns and opens the shell anyway.

### git push from the VM

Each VM gets your Mac's SSH agent forwarded, so `git push` uses the keys loaded on your Mac. No key
files are copied in. The agent is empty after every reboot, so load your key once:

```sh
ssh-add --apple-use-keychain ~/.ssh/id_ed25519   # or whichever key you use
```

To have it loaded again automatically when you log in, add this to `~/.zshrc`:

```sh
ssh-add --apple-load-keychain 2>/dev/null
```

When the agent has no keys, orbit warns you on create and on attach.

## Configuration

orbit reads its configuration **only from `~/.config/orbit/`, on the Mac**. A workspace is writeable
from inside its VM, so nothing in a workspace can change what the VM is allowed to reach.

### `~/.config/orbit/config.toml`

Every setting is optional, and a missing file is fine.

```toml
distro = "ubuntu"          # OrbStack image, e.g. "ubuntu:24.04"; default "ubuntu"
cpus = 4                   # resource limits; unset means OrbStack's shared defaults
memory = "8G"
disk = "64G"

# Extras: Mac paths a VM may see, at the same path. Global ones go to every workspace VM.
[[extras]]
path = "~/.gitconfig"      # mode defaults to "ro": copied in, changes in the VM never reach the Mac

[[extras]]
path = "~/.config/gh"
mode = "ro"

# Per-workspace extras, added to the global ones (a workspace's mode wins for the same path).
[[workspaces."~/github/emmz".extras]]
path = "~/notes/emmz"
mode = "rw"                # mounted writeable: changes in the VM appear on the Mac
```

- Paths start with `~/` or are absolute. They can't contain `:` or `,`.
- An extra can't overlap the workspace itself.
- Unknown keys are an error, so a typo like `mod = "rw"` doesn't quietly fall back to read-only.
- A symlinked extra, such as a dotfile linked from a dotfiles repo, appears at the path you declared,
  with its target's contents.
- **Read-only extras are copies.** They're made when the VM is created or rebuilt. Run `orbit sync`
  after changing the originals.
- **Writeable extras are mounts.** Adding or removing one takes effect on `orbit rebuild`.
- If a declared extra doesn't exist, orbit refuses to create or rebuild, and creates nothing.

### Provisioning

When a VM is created or rebuilt, orbit:

1. copies in the read-only extras;
2. runs **`~/.config/orbit/baseline.sh`** from your Mac as root, piped into `bash` (or `sh`), with
   `ORBIT_USER` set to the VM's default user (the same name as your Mac user). This is your common
   dev baseline: shell, editors, runtimes, Claude Code;
3. runs **`<workspace>/.orbit/setup.sh`** as your user, from the workspace directory. This is whatever
   this workspace needs on top of the baseline. It runs from the mounted workspace, so it can use
   files next to it.

Either script can be absent. Their output streams to your terminal. The first shell opens only after
both have finished.

If a script fails, orbit names it, exits non-zero, and **keeps the VM** so you can look around
(`orbctl run -m <name>`). Then run `orbit rebuild` to start over, or `orbit destroy` to remove it.
Scripts run only on create and rebuild, never on attach or start.

An example `baseline.sh` for Ubuntu:

```sh
set -eu
apt-get update
apt-get install -y git curl ripgrep build-essential
# Per-user tools go in as your user, not root:
runuser -l "$ORBIT_USER" -c 'curl -fsSL https://claude.ai/install.sh | bash'
```

## Commands

| Command | What it does |
|---|---|
| `orbit` | Create the current directory's VM if needed, then open a shell in the current directory |
| `orbit auth` | Store the Claude token from stdin or a hidden prompt. `--clear` removes it |
| `orbit list` | Show workspace VMs, their directories and states. `--json` for scripts. Orphans are flagged |
| `orbit start [name]` | Start a VM without opening a shell |
| `orbit stop [name]` | Stop a VM, keeping its state |
| `orbit sync [name]` | Re-copy read-only extras from the Mac into the VM |
| `orbit rebuild [name]` | Destroy and recreate the VM from the current config. Asks first, or `--yes` |
| `orbit destroy [name]` | Delete the VM. Files on your Mac are untouched. Asks first, or `--yes` |

With no `name`, a command acts on the workspace that covers the current directory. A `name` must be
one of orbit's own VMs (`orbit-…`). orbit never touches other OrbStack machines.

- **Workspaces can't overlap.** If `~/github/emmz/orbit` already has a VM, running `orbit` in
  `~/github/emmz` is refused, so two VMs never mount the same files. Run it from inside the existing
  workspace, or `orbit destroy` that VM first.
- **Your home directory can't be a workspace.** Neither can any directory above it (`~`, `/Users`,
  `/`), because that would expose everything in `~`, `~/.ssh` included.
- **Moved directories leave orphans.** VMs are named from their directory's path. If you move or
  rename a workspace directory, its VM shows as orphaned in `orbit list`. Remove it with
  `orbit destroy <name>`, then run `orbit` in the new location.

## Security model

What isolation gives you:

- Each VM is an OrbStack **isolated machine**. It has no `/mnt/mac`, can't reach the Mac over the
  network, and can't run `mac` commands.
- It sees only the workspace directory and the extras you've allowlisted.

What it doesn't, by design:

- **Isolation is not a full VM boundary.** All OrbStack machines share one Linux kernel.
  OrbStack describes isolated machines as fine for "AI agents that aren't trying to break out", not
  for malware.
- **git push uses your normal access.** The forwarded agent can authenticate as you to any remote
  your keys can reach. Scoped credentials are a possible later addition.
- **The network is open.** A VM can reach the internet freely. Isolation is about your Mac's files,
  not the network.

Within those limits, running `claude --dangerously-skip-permissions` in an orbit VM puts only the
workspace and your allowlisted extras at risk, plus whatever your SSH keys and the internet can reach.

## Development

```sh
make deps    # check every tool and login in docs/dependencies.md
make gates   # build, test, gofmt check, go vet + staticcheck, openspec validate
```

See `docs/DISCOVERY.md` for requirements, `docs/adrs/` for the foundational decisions, and
`openspec/` for specs and changes.
