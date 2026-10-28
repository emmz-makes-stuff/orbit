# claude-auth Specification

## Purpose

Lets Claude in every workspace VM use Emmz's login without re-authenticating per VM. The token is kept
on the Mac and handed to each shell, never stored in a VM.

## Requirements

### Requirement: Store the token
`orbit auth` SHALL read a Claude token, from stdin when it is piped or from a hidden prompt otherwise,
and store it in the macOS login Keychain, replacing any token already stored. The token SHALL NOT be
echoed, logged or written to a file.

#### Scenario: Piped token
- **WHEN** `orbit auth` runs with the token piped on stdin
- **THEN** the token is stored in the Keychain and `orbit` confirms without printing it

#### Scenario: Empty input
- **WHEN** `orbit auth` receives empty input
- **THEN** nothing is stored and the exit code is non-zero

### Requirement: Clear the token
`orbit auth --clear` SHALL remove the stored token. It SHALL succeed when no token is stored.

#### Scenario: Clear
- **WHEN** a token is stored and `orbit auth --clear` runs
- **THEN** the Keychain no longer holds it

### Requirement: Inject the token into shells
Every shell `orbit` opens SHALL have `CLAUDE_CODE_OAUTH_TOKEN` set to the stored token, read from the
Keychain at attach time. The token SHALL NOT be written to the VM's disk by `orbit`.

#### Scenario: Claude authenticated in a new VM
- **WHEN** a token is stored and `orbit` attaches to a freshly created VM
- **THEN** `CLAUDE_CODE_OAUTH_TOKEN` in the shell equals the stored token

#### Scenario: Rotation reaches existing VMs
- **WHEN** a new token is stored with `orbit auth` and `orbit` attaches to a VM created earlier
- **THEN** the shell has the new token

#### Scenario: Not on disk
- **WHEN** the VM's filesystem is searched for the token after a session
- **THEN** `orbit` has not written it to any file

### Requirement: Missing token warns, does not block
If no token is stored, `orbit` SHALL still open the shell, without setting `CLAUDE_CODE_OAUTH_TOKEN`,
and print a warning that names `orbit auth`.

#### Scenario: No token stored
- **WHEN** no token is stored and `orbit` attaches
- **THEN** the shell opens, `CLAUDE_CODE_OAUTH_TOKEN` is unset, and a warning mentions `orbit auth`
