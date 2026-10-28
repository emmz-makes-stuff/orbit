# ADR-0003: OrbStack is the record of workspaces; config is split by trust

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

`orbit` must find the VM for the current directory, or for its nearest ancestor. It must also read
global and per-workspace configuration. The workspace is writeable from inside the VM, so anything
stored there can be changed by Claude.

## Decision

**No datastore of orbit's own.** Machines are named `orbit-<slug>`. The workspace path is read back
from the machine's mount config (`orbctl config get machine.<name>.mounts`). To resolve a directory,
`orbit` lists the `orbit-*` machines and picks the one whose workspace mount is the nearest ancestor of
(or equal to) the current directory.

**Configuration is split by trust:**

- **Mac-only:** `~/.config/orbit/config.toml` holds the allowlisted extras (writeable mounts and
  read-only copies), global with per-workspace overrides, along with defaults such as the distro and
  resource limits. The common baseline lives in `~/.config/orbit/baseline.sh`.
- **In the workspace:** only `.orbit/setup.sh`. It runs inside the VM through cloud-init, so it cannot
  widen what the VM can reach.

## Consequences

- The record can't drift from what OrbStack actually has. Deleting a machine in the OrbStack app
  removes it from `orbit` too.
- If a workspace directory is moved or renamed, its machine's mount no longer matches and the VM
  is orphaned. `orbit list` must show that state (an open question carried over from discovery).
- Machine names must stay within OrbStack's naming rules, so the slug is derived and de-duplicated.

## Alternatives rejected

- **A state file in `~/.config/orbit`.** It can carry metadata, but it drifts from OrbStack.
- **All config in the workspace.** Claude could add mounts (e.g. `~/.ssh`) that take effect on the
  next rebuild.
- **All config on the Mac.** Safe, but workspace setup wouldn't be kept with the repos.
