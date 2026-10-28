# Spec Delta

## Purpose

Takes Emmz from any directory to a shell inside the right workspace VM with one command, creating or
starting the VM as needed and never letting two VMs cover the same files.

## ADDED Requirements

### Requirement: Resolve the current directory to a workspace
When `orbit` runs, it SHALL resolve the current directory, with symlinks resolved, to the workspace
VM whose workspace directory equals it or is its nearest ancestor. Only machines managed by `orbit`
SHALL be considered.

#### Scenario: Exact match
- **WHEN** a workspace VM exists for `~/github/emmz` and `orbit` runs in `~/github/emmz`
- **THEN** that VM is selected

#### Scenario: Nearest ancestor
- **WHEN** a workspace VM exists for `~/github/emmz` and `orbit` runs in `~/github/emmz/orbit/docs`
- **THEN** the `~/github/emmz` VM is selected

#### Scenario: Unmanaged machines ignored
- **WHEN** a non-orbit OrbStack machine exists that shares `~/github/emmz`
- **THEN** `orbit` does not select it, and does not count it as an overlap

### Requirement: Create a workspace VM on first use
When no workspace covers the current directory and no workspace lies beneath it, `orbit` SHALL create
a new workspace VM whose workspace directory is the current directory, then attach to it.

#### Scenario: First run in a directory
- **WHEN** `orbit` runs in `~/github/emmz` and no workspace covers or lies beneath it
- **THEN** a workspace VM for `~/github/emmz` is created, provisioned, and a shell opens in `~/github/emmz`

### Requirement: Refuse overlapping workspaces
`orbit` SHALL refuse to create a workspace VM for a directory that is a strict ancestor of an
existing workspace. It SHALL exit non-zero, name each such workspace and its VM, and suggest
destroying them or running from inside one.

#### Scenario: Parent of an existing workspace
- **WHEN** a workspace VM exists for `~/github/emmz/orbit` and `orbit` runs in `~/github/emmz`
- **THEN** no VM is created, the error names `~/github/emmz/orbit` and its VM, and the exit code is non-zero

### Requirement: Attach opens a shell in the current directory
Attaching SHALL open an interactive login shell inside the workspace VM, as the default user, with the
working directory set to the current directory (same path as on the Mac). `orbit` SHALL NOT start
Claude, and SHALL exit with the shell's exit code.

#### Scenario: Shell starts in a subdirectory
- **WHEN** `orbit` runs in `~/github/emmz/orbit` and attaches to the `~/github/emmz` VM
- **THEN** the shell's working directory is `~/github/emmz/orbit`

#### Scenario: Shell exit code propagates
- **WHEN** the shell exits with code 3
- **THEN** `orbit` exits with code 3

### Requirement: Start a stopped VM on attach
If the resolved workspace VM is stopped, `orbit` SHALL start it before opening the shell, with no
extra prompt.

#### Scenario: Attach to a stopped VM
- **WHEN** the resolved VM is stopped and `orbit` runs
- **THEN** the VM is started and the shell opens

### Requirement: Concurrent shells
Several `orbit` shells SHALL be able to attach to the same workspace VM at once, without disturbing
each other.

#### Scenario: Two terminals, one workspace
- **WHEN** `orbit` is already attached in `~/github/emmz/a` and a second `orbit` runs in `~/github/emmz/b`
- **THEN** a second shell opens in `~/github/emmz/b` and the first shell is unaffected

### Requirement: OrbStack unavailable
If OrbStack is not installed, or cannot be started, `orbit` SHALL exit non-zero with a message that
says which, and how to fix it.

#### Scenario: OrbStack not installed
- **WHEN** `orbctl` is not on the PATH
- **THEN** `orbit` exits non-zero and says OrbStack is required
