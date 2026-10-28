# Spec Delta

## Purpose

Defines exactly what a workspace VM can reach on the Mac: the workspace, the extras Emmz has
allowlisted on the Mac, and her SSH agent. Nothing inside the workspace can widen that.

## ADDED Requirements

### Requirement: Isolated from the Mac
Every workspace VM SHALL be created isolated. It SHALL NOT see the Mac's filesystem beyond its mounts,
SHALL NOT reach the Mac over the network, and SHALL NOT run commands on the Mac. Outbound internet
access SHALL be open.

#### Scenario: Home directory not visible
- **WHEN** a shell in a workspace VM for `~/github/emmz` lists `~/Documents` or `/mnt/mac`
- **THEN** neither exists in the VM

#### Scenario: Internet reachable
- **WHEN** a shell in the VM fetches a public HTTPS URL
- **THEN** the request succeeds

### Requirement: Writeable workspace mount
The workspace directory SHALL be mounted writeable at the same absolute path as on the Mac.

#### Scenario: Edit visible on the Mac
- **WHEN** a file is written under the workspace from inside the VM
- **THEN** the same file, with the same contents, is present at that path on the Mac

### Requirement: Allowlisted extras
Extras SHALL be declared only in the Mac-only config, globally or per workspace. Each one is a Mac
path, given as either a read-only copy (the default) or a writeable mount. An extra under the Mac
home directory appears at the same path relative to the VM user's home, so tools find it at `~`. Any
other extra appears at the same absolute path. The VM SHALL get nothing else from the Mac.

#### Scenario: Extra under the home directory
- **WHEN** `~/.gitconfig` is declared as an extra and the VM user's home is `/home/emmz`
- **THEN** it appears in the VM at `/home/emmz/.gitconfig`, so git in the VM uses it, and any directories created above it there belong to the VM user

#### Scenario: VM home is not where expected
- **WHEN** a new VM's default user turns out to have a home other than `/home/<Mac user>`, and extras under the Mac home were declared
- **THEN** provisioning fails, naming both directories, and the VM is kept for inspection

#### Scenario: Writeable extra
- **WHEN** an extra is declared with mode `rw`
- **THEN** it is mounted in the VM, and changes made from the VM appear on the Mac

#### Scenario: Read-only extra
- **WHEN** an extra is declared without a mode
- **THEN** its contents are copied into the VM at the same path, and changes made in the VM never reach the Mac

#### Scenario: Per-workspace extra
- **WHEN** an extra is declared only for `~/github/emmz`
- **THEN** the `~/github/emmz` VM has it and other workspace VMs do not

#### Scenario: Overlapping extras
- **WHEN** one extra lies within another and at least one of the two is `rw`, such as `~/.config` with `~/.config/gh` as `rw`
- **THEN** create, rebuild and sync exit non-zero, name both extras, and create or change nothing, because replacing the read-only copy would reach through the mount to the Mac

#### Scenario: Missing extra
- **WHEN** a declared extra does not exist on the Mac at create or rebuild time
- **THEN** the command exits non-zero, names the extra, and no VM is left half-created

### Requirement: Refresh read-only copies
`orbit sync` SHALL re-copy the target workspace's read-only extras from the Mac into the VM, replacing
the previous copies. Copies SHALL also be made on create and rebuild.

#### Scenario: Sync after the Mac copy changes
- **WHEN** a read-only extra is changed on the Mac and `orbit sync` runs
- **THEN** the VM's copy matches the Mac's

### Requirement: Workspace content cannot widen access
No file inside a workspace SHALL be able to add mounts, extras, SSH forwarding or any other access to
the Mac. The only workspace file `orbit` reads is the setup script, which runs inside the VM.

#### Scenario: Config dropped into the workspace
- **WHEN** a file such as `.orbit/config.toml` declaring `~/.ssh` as an extra is created in the workspace and the VM is rebuilt
- **THEN** `~/.ssh` is not present in the new VM

#### Scenario: Config directory override
- **WHEN** `ORBIT_CONFIG_DIR` is set in the Mac environment that runs `orbit`
- **THEN** `config.toml` and `baseline.sh` are read from that directory instead of `~/.config/orbit`, and nothing in a workspace or a VM can set it

### Requirement: SSH agent forwarding for git
Every workspace VM SHALL have the Mac's SSH agent forwarded, so that git over SSH can authenticate
with the keys loaded on the Mac. Private key files SHALL NOT be copied into the VM.

#### Scenario: Push from the VM
- **WHEN** the Mac's agent holds a key with access to a remote and `git push` runs in the VM
- **THEN** the push authenticates through the forwarded agent

#### Scenario: No key files in the VM
- **WHEN** the VM's filesystem is searched for the Mac's private key files
- **THEN** none are present

### Requirement: Warn when the SSH agent is empty
On create and on attach, `orbit` SHALL check whether the Mac's SSH agent holds any keys. If it holds
none, or there is no agent, `orbit` SHALL print a warning that names `ssh-add --apple-use-keychain`
and then continue as normal.

#### Scenario: Empty agent
- **WHEN** `ssh-add -l` on the Mac reports no identities and `orbit` attaches
- **THEN** a warning naming `ssh-add --apple-use-keychain` is printed and the shell opens

#### Scenario: Keys loaded
- **WHEN** the Mac's agent holds at least one key and `orbit` attaches
- **THEN** no SSH agent warning is printed
