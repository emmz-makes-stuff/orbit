# workspace-lifecycle Specification

## Purpose

Lets Emmz see, pause, remove and rebuild her workspace VMs, without ever touching the workspace files
on the Mac or any OrbStack machine that `orbit` didn't create.

## Requirements

### Requirement: Target selection for lifecycle commands
`orbit stop`, `start`, `destroy` and `rebuild` SHALL act on the VM named as an argument. With no
argument, they act on the workspace resolved from the current directory. Naming an unmanaged machine,
or resolving no workspace, SHALL exit non-zero without changing anything.

#### Scenario: Implicit target
- **WHEN** `orbit stop` runs in `~/github/emmz/orbit` and a workspace exists for `~/github/emmz`
- **THEN** the `~/github/emmz` VM is stopped

#### Scenario: Unmanaged machine named
- **WHEN** `orbit destroy ubuntu` runs and `ubuntu` is not an orbit-managed machine
- **THEN** nothing is deleted and the exit code is non-zero

#### Scenario: No workspace here
- **WHEN** `orbit stop` runs with no argument in a directory that no workspace covers
- **THEN** the exit code is non-zero and the message says no workspace covers this directory

### Requirement: List workspaces
`orbit list` SHALL show every orbit-managed VM with its name, workspace directory and state (running
or stopped), and flag a VM as **orphaned** when its workspace directory no longer exists on the Mac.

#### Scenario: Running and stopped
- **WHEN** two workspace VMs exist, one running and one stopped
- **THEN** both are listed with their workspace directories and states

#### Scenario: Moved workspace directory
- **WHEN** a workspace directory has been moved or deleted on the Mac
- **THEN** `orbit list` marks that VM as orphaned

#### Scenario: Machine-readable output
- **WHEN** `orbit list --json` runs
- **THEN** the output is a JSON array with name, workspace, state and orphaned for each VM

### Requirement: Stop and start
`orbit stop` SHALL stop the target VM, keeping its state. `orbit start` SHALL start it without opening
a shell. Both SHALL succeed, without error, when the VM is already in the requested state.

#### Scenario: Stop keeps state
- **WHEN** a file is created in the VM's home directory, then the VM is stopped and started
- **THEN** the file still exists

#### Scenario: Already stopped
- **WHEN** `orbit stop` runs on a stopped VM
- **THEN** it exits zero

### Requirement: Destroy
`orbit destroy` SHALL delete the target VM after confirmation, and SHALL NOT modify any files on the
Mac. Confirmation SHALL be skipped with `--yes`. Without a terminal and without `--yes`, it SHALL
refuse and exit non-zero.

#### Scenario: Confirmed destroy
- **WHEN** `orbit destroy` runs and Emmz confirms
- **THEN** the VM is deleted and the workspace directory's contents are unchanged

#### Scenario: Non-interactive without --yes
- **WHEN** `orbit destroy` runs with stdin not a terminal and no `--yes`
- **THEN** nothing is deleted and the exit code is non-zero

### Requirement: Rebuild
`orbit rebuild` SHALL destroy the target VM and create a new one for the same workspace directory,
with the current configuration, extras and provisioning. Anything that existed only in the old VM
SHALL be lost. It confirms the same way as destroy.

#### Scenario: Rebuild resets VM-only state
- **WHEN** a package was installed by hand in the VM and `orbit rebuild --yes` runs
- **THEN** the new VM does not have that package, and it does have the baseline and the workspace setup applied

#### Scenario: Rebuild picks up config changes
- **WHEN** an extra is added in `~/.config/orbit/config.toml` and `orbit rebuild --yes` runs
- **THEN** the new VM has that extra

#### Scenario: Rebuild of an orphan
- **WHEN** `orbit rebuild` targets an orphaned VM
- **THEN** it exits non-zero without destroying the VM, and says the workspace directory is missing
