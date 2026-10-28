# Orbit — Discovery

> Requirements only: what and why. Tech decisions are made later, in `/emmz:architecture`.

## Problem & users

Emmz uses OrbStack and wants to run Claude inside a VM rather than directly on her Mac. Today,
creating a VM for a working directory and getting back into it is a manual job.

`orbit` is "a CLI wrapper" around OrbStack that does this for her.

**Why a VM**, which is all four of these:

- **Isolation / safety.** Claude should only be able to touch the mounted directory, not the rest of
  the Mac (home directory, credentials, other projects).
- **Unattended runs.** Claude can run with permissions skipped or fewer prompts, because the blast
  radius is contained.
- **Clean environment.** Each workspace gets its own Linux toolchain, so installed tools don't pollute
  the Mac or other workspaces.
- **Parallel workspaces.** Several Claude sessions can run in separate workspaces at the same time
  without stepping on each other.

**Users:** Emmz first ("me, shared later"). It's built for her setup, but it should be publishable for
other OrbStack users eventually.

## Outcomes & behaviours

- **One command per directory.** Run `orbit` in a directory:
  - if no VM has been created for that directory yet, it **creates a new VM** "for running Claude in",
    with the current directory **mounted as writeable**;
  - if a VM has "already been created for the current directory", it **attaches to the existing VM**.
- **Multi-repo workspaces.** The directory may be a parent "that contains multiple repo clones, so
  they'd all be mounted into the same VM". One VM per directory, not one per repo.
- **Attach opens a shell** in the mounted directory. Emmz starts Claude herself; `orbit` does not
  launch Claude.
- **Subdirectories reuse the parent's VM.** If a VM exists for `~/github/emmz` and `orbit` runs in
  `~/github/emmz/orbit`, it attaches to the `~/github/emmz` VM and the shell starts in the `orbit`
  subdirectory.
- **Filesystem visibility is the workspace plus an allowlist.** The VM sees the workspace directory
  (writeable), plus any specific things Emmz opts into (e.g. a shared config directory). Those extras
  are read-only unless stated otherwise. Nothing else on the Mac is visible.
- **Claude auth comes along.** `orbit` makes Emmz's Claude login available in the VM, so she doesn't
  re-authenticate in every new VM.
- **Git push works from inside the VM, with Emmz's normal access.** Claude, or Emmz, can push to the
  same remotes she can push to from the Mac. She accepts the risk that comes with that.
- **Network is open.** Outbound internet access is unrestricted. Isolation is about the Mac's
  filesystem, not the network.
- **Provisioning is a common base plus per-workspace setup.** Emmz defines a dev baseline once
  (shell, editors, common runtimes), and every new VM gets it. Each workspace can then declare
  anything extra its VM needs.
- **Lifecycle commands** besides create/attach:
  - **list**: orbit's VMs and the directory each one belongs to;
  - **stop / start**: free resources while keeping the VM's state;
  - **destroy**: delete a workspace's VM, leaving the files on the Mac untouched;
  - **rebuild**: destroy and recreate a workspace's VM from scratch.

## Scope

### In

- `orbit` in a directory either creates that directory's VM or attaches to it, opening a shell in the
  directory.
- Multi-repo workspaces: a parent directory and everything under it share one VM.
- Subdirectories reuse an ancestor's VM.
- A writeable workspace mount, plus allowlisted extras that are read-only by default. Nothing else on
  the Mac is visible.
- Claude auth and git push access with Emmz's normal permissions, carried into the VM.
- Provisioning from a common dev baseline plus per-workspace setup.
- Lifecycle commands: list, stop/start, destroy, rebuild.

### Explicitly out

- **VM providers other than OrbStack**, such as Docker, Lima, UTM or cloud VMs.
- **Hosts other than macOS.**
- **Launching Claude.** `orbit` opens a shell and never starts or manages Claude sessions itself.
- **Network restrictions**, not even as an option.

### Later

- **Publishing for others**: packaging, install instructions and docs for other OrbStack users.
- **Scoped git credentials**: an option to limit push access per workspace, for people less relaxed
  about the risk than Emmz.
- **Other agents** besides Claude (Codex, etc.), with the same auth-carrying.

## Hard constraints

- **OrbStack** is the VM provider. Source: Emmz's existing tooling ("i use orbstack"); `orbit` wraps it.
- **A command-line tool**, invoked as `orbit`. Source: Emmz's stated product shape ("a CLI wrapper
  called `orbit`").

## Success criteria

- **Daily driver.** Emmz runs Claude through `orbit` by default instead of on the Mac.
- **Fast attach.** Getting back into an existing workspace VM is near-instant: one command, no
  thought.
- **Trust to skip permissions.** Emmz is comfortable running Claude with permissions skipped inside an
  `orbit` VM.
- **Zero-touch new VM.** A new workspace VM is ready to use, with auth, tools and push all working,
  without any manual setup.

## Open questions & ambiguities

1. **Accepted risk, named plainly.** Skipped permissions, Emmz's full git push access and open
   network together mean an unattended Claude can push to any repo she can reach, and send data
   anywhere. Emmz accepts this for herself. It matters again when `orbit` is shared (*Later*), which
   is why scoped git credentials are on the *Later* list.
2. **Overlapping workspaces, opposite case.** If a VM already exists for `~/github/emmz/orbit` and
   `orbit` runs in `~/github/emmz`, should it create a new parent VM (two VMs covering the same
   files), refuse, or something else?
3. **Moved or renamed directories.** What happens to a workspace's VM when its directory is moved or
   renamed on the Mac?
4. **Allowlisted extras.** Are they declared once for every VM, per workspace, or both? Which ones are
   needed on day one, if any?
5. **Claude auth freshness.** Is it a one-off copy when the VM is created, or kept in step with the
   Mac's login (e.g. after re-authenticating)?
6. **What a rebuild loses.** Anything kept in the VM rather than in the workspace (shell history, VM-side
   Claude history or config, installed extras) is gone after a rebuild. Is that expected, or should
   some of it survive?
7. **Attaching to a stopped VM.** Should `orbit` start it automatically as part of "fast attach"? The
   assumption is yes.
8. **Concurrent shells.** Several shells attached to one workspace VM at once (e.g. two Claude
   sessions in different repos of the same workspace) is assumed to be fine. Confirm.
9. **"Fast attach" target.** "Near-instant" has no number yet.
10. **Snapshots** (snapshot/restore of a VM's state) were offered as *Later* and not picked. They're
   not in scope for now.
