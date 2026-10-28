# Code review: findings, verdicts and Emmz's rulings

<!-- Base: ad3ebc0 -->

The `code-review` skill ran at medium on `main...change/add-orbit-core-cli`. Each finding checked:

- **New, found while checking #7, the biggest:** in an isolated VM the default user's `$HOME` is
  `/home/<user>`, and `/Users` doesn't exist. Verified on a scratch VM, since deleted. Extras go to
  their Mac path, so `~/.gitconfig` lands at `/Users/<user>/.gitconfig` and git never sees it. The
  integration test missed this because it reads extras by absolute path.
- **#1, data loss:** `sync` checks the current config, not the VM's real mounts. Change an extra from
  rw to ro, or add ro inside a mounted rw, then sync, and the `rm -rf` goes through the live mount.
- **#2:** `orb.output` wraps `*ExitError` with `%w` when orbctl prints nothing. `cli.Run` then exits
  with that code and no message. The passthrough in `cli.Run` catches nothing else: the shell's exit
  code passes through `exec`.
- **#3:** a symlink inside an ro extra can redirect a nested ro extra's `rm`, tar and `chown` into the
  workspace mount. My §10 claim that ro-inside-ro "can't touch the Mac" was wrong.
- **#4:** `tar.FileInfoHeader` rejects sockets, so e.g. `~/.gnupg` fails the whole copy.
- **#5:** a setup.sh `Stat` error other than not-exist skips setup silently, and the VM is still
  marked provisioned.
- **#6:** any `ExitError` from the marker check reads as "never provisioned". Narrow it to exit 1.
- **#7:** parent directories that tar creates are root-owned. Fold this into the home mapping.
- **#8:** `rebuild` recreates under `workspace.Name(ws)`, not `m.Name`. Only matters for hand-made names.
- **#9:** an empty `id -un` produces the error `<nil>`.
- **#10:** `confirmedCmd` duplicates `targetCmd`'s RunE.

Emmz's rulings:
- **Map `~` to the VM home.** Extras under the Mac home appear at the same path relative to the VM
  user's home. Other extras, and the workspace, keep their exact Mac path. Rejected: moving the VM
  user's home to `/Users/<user>`, which fights OrbStack's defaults.
- **Refuse all nested extras, whatever their modes.** This reverses §10's ro-in-ro allowance.

Fix sections 12–14 are proposed in `tasks.md` and wait on Emmz's agreement.

## Next

- Waiting on Emmz to agree sections 12–14. Then build them with the section loop, starting at 12.1.
- After §12, rerun the integration test with an assertion that `~/.gitconfig` is visible as `~` in
  the VM. A run from Claude reads the Keychain token, which may raise a macOS access prompt for Emmz.
