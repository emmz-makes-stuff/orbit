# Spec Delta

## Purpose

Makes every new or rebuilt workspace VM ready to use without manual setup. Each one gets Emmz's
common dev baseline and then whatever the workspace itself declares.

## ADDED Requirements

### Requirement: Apply the common baseline
When a workspace VM is created or rebuilt, `orbit` SHALL run `~/.config/orbit/baseline.sh` from the
Mac inside the new VM, before any workspace setup. If the file is absent, the step SHALL be skipped
without error.

#### Scenario: Baseline present
- **WHEN** `baseline.sh` installs a package and a new workspace VM is created
- **THEN** the package is installed in the VM before the first shell opens

#### Scenario: No baseline
- **WHEN** `~/.config/orbit/baseline.sh` does not exist
- **THEN** the VM is created and attached normally

### Requirement: Apply the workspace setup
After the baseline, `orbit` SHALL run the workspace's `.orbit/setup.sh` inside the VM, as the default
user, with the workspace directory as its working directory. If the file is absent, the step SHALL be
skipped without error.

#### Scenario: Workspace setup present
- **WHEN** `<workspace>/.orbit/setup.sh` exists and the VM is created
- **THEN** it runs after the baseline, in the workspace directory

### Requirement: Provisioning completes before attach
The first shell SHALL open only once provisioning has finished. `orbit` SHALL show progress while it
runs.

#### Scenario: Wait for provisioning
- **WHEN** a new workspace VM is being provisioned
- **THEN** `orbit` shows that provisioning is in progress, and opens the shell only after it finishes

### Requirement: Provisioning failure is visible
If the baseline or the workspace setup exits non-zero, `orbit` SHALL exit non-zero, say which script
failed, and show its output. The VM SHALL be kept, so it can be inspected, until destroyed or rebuilt.

#### Scenario: Setup script fails
- **WHEN** `.orbit/setup.sh` exits with code 1 during creation
- **THEN** `orbit` exits non-zero, names `.orbit/setup.sh`, shows its output, and the VM still appears in `orbit list`

### Requirement: Provisioning runs once per VM
The baseline and the workspace setup SHALL run only when a VM is created or rebuilt. They SHALL NOT
run on attach or start.

#### Scenario: Re-attach
- **WHEN** `orbit` attaches to an existing, already-provisioned VM
- **THEN** neither script runs again
