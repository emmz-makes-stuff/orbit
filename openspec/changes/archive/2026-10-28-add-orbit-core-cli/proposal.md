# Proposal

## Why

Emmz wants to run Claude in an OrbStack VM per workspace, where it can see only the workspace and can
run unattended with permissions skipped. Today each VM has to be created, mounted, provisioned and
re-entered by hand (see `docs/DISCOVERY.md`). This change builds the first working `orbit`: one
command that takes the current directory to a ready shell in that workspace's VM.

## What Changes

- New `orbit` CLI (Go, ADR-0002). Running bare `orbit` resolves the current directory to its
  workspace VM, creating it if needed, then opens a shell in the current directory.
- Workspace resolution: an exact match or the nearest ancestor reuses that VM. A directory that is an
  ancestor of an existing workspace is refused, so two VMs never mount the same files.
- VMs are OrbStack **isolated** machines (ADR-0001). The workspace is mounted writeable. Allowlisted
  extras are either mounted writeable or copied in read-only. SSH agent forwarding gives git push. The
  network is open.
- Provisioning through cloud-init, from the Mac-only `~/.config/orbit/baseline.sh` plus the
  workspace's `.orbit/setup.sh`.
- Claude auth: `orbit auth` stores a `claude setup-token` token in the macOS Keychain, and every
  `orbit` shell receives it as `CLAUDE_CODE_OAUTH_TOKEN`.
- Lifecycle: `orbit list` (orphaned VMs flagged), `orbit stop`, `orbit start`, `orbit destroy`,
  `orbit rebuild`.
- Mac-only config `~/.config/orbit/config.toml` (ADR-0003). OrbStack is the only record of which VM
  belongs to which workspace.

## Capabilities

### New Capabilities

- `workspace-sessions`: resolving a directory to its workspace VM (exact, ancestor or refused
  overlap), creating it on first use, starting it if stopped, and opening shells in it, including
  concurrent ones.
- `workspace-lifecycle`: listing, stopping, starting, destroying and rebuilding workspace VMs,
  including orphan detection.
- `vm-access`: what a workspace VM can reach on the Mac, namely isolation, the workspace mount,
  allowlisted extras (writeable mounts or read-only copies) and SSH agent forwarding, and that
  configuration inside the workspace cannot widen it.
- `vm-provisioning`: applying the common baseline and the per-workspace setup to new and rebuilt VMs.
- `claude-auth`: storing, clearing and injecting the Claude token.

### Modified Capabilities

None. This is a greenfield project.

## Impact

- New Go module at the repo root, with `orbit` as its binary.
- Runtime dependencies (see `docs/dependencies.md`): OrbStack ≥ 2.2 (`orbctl`), macOS `security`,
  and an SSH agent with keys loaded for push.
- Creates and modifies files only in `~/.config/orbit/` (read), the Keychain (one generic-password
  item) and OrbStack machines named `orbit-*`. It never touches other machines.
