# ADR-0001: Workspace VMs are OrbStack isolated machines

- **Status:** Accepted
- **Date:** 2026-10-28

## Context

Discovery requires that Claude see only the workspace (plus allowlisted extras), that it can push to
git with Emmz's normal access, and that every VM is provisioned from a common baseline plus
per-workspace setup. OrbStack is a hard constraint.

A normal OrbStack machine shares the whole Mac filesystem (`/mnt/mac`, `/Users`), so it fails the
isolation requirement.

## Decision

Every workspace VM is created as an **isolated** OrbStack machine (OrbStack ≥ 2.2):

- `--isolated`: no Mac filesystem, no access to the Mac over the network, no `mac` commands;
- `--mount <workspace>`: the workspace, writeable, at the same path inside the VM, plus
  allowlisted writeable extras;
- `--forward-ssh-agent`: git push with Emmz's normal access, without copying keys;
- provisioning (the baseline plus per-workspace setup) is run by `orbit` itself through `orbctl run`
  after creation, not through cloud-init's `--user-data`, so it can wait on each script, stream its
  output and report which one failed (see `design.md` D9 of `add-orbit-core-cli`).

The network is left open (`isolate_network` stays off), per discovery.

## Consequences

- OrbStack's security model applies. Isolated machines share one kernel and are "not a full security
  boundary". That is suitable for "AI agents that aren't trying to break out", which matches the risk
  Emmz accepted, but not for hostile code.
- OrbStack mounts have no read-only option, so read-only extras are copied in, not mounted (see
  `design.md`).
- Changing mounts on an existing machine needs `orb config set machine.<name>.mounts` and a restart.

## Alternatives rejected

- **A normal (non-isolated) OrbStack machine.** It shares the whole Mac filesystem.
- **OrbStack Docker containers with bind mounts.** They allow exact mounts, but discovery asks for a
  full VM with its own toolchain and services, not a container.
