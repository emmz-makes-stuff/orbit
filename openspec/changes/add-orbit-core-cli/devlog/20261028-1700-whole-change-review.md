# Whole-change review: one data-loss bug, plus small fixes

<!-- Base: 9028b9b..3f3a404 -->

I read all of the production code in `internal/` and `cmd/` against the five specs, `design.md` and
the ADRs. Most of it holds together: no leftover scaffolding, no duplicated abstractions apart from
item 2, and the contracts are consistent from section to section. Findings:

1. **Data loss: overlapping extras (must fix).** `copyExtras` runs `rm -rf -- <ro paths>` as root
   in the VM before it untars. Nothing stops a read-only extra from overlapping a writeable one:
   - **ro is the parent of rw** (e.g. ro `~/.config`, rw `~/.config/gh`): the rm goes through the
     virtiofs mount and **deletes the Mac's files**. Checked on real OrbStack: a Mac file inside a
     mount under the rm'd parent was gone, and only then did `rm` fail with "Device or resource
     busy" on the mountpoint.
   - **ro is inside rw** (rw `~/notes`, ro `~/notes/x`): the rm deletes the Mac's `x`, and the tar
     then writes it back through the mount. That breaks the vm-access rule that ro changes "never
     reach the Mac".
   - Hits create, rebuild and sync. `planFor` checks extras only against the workspace, not against
     each other.
2. **Duplication:** `runSync` repeats `planFor`'s ro-extras filter and existence check. The fix for
   item 1 has to apply in both places, so they should share one helper.
3. **Swallowed error:** `copyExtras` discards `writeTar`'s error, which only goes to
   `pw.CloseWithError`. A Mac-side read failure, such as an unreadable file in an extra, shows up as
   tar's in-VM complaint without the real cause. If the stream happens to end at a header boundary,
   tar may even report success after a partial copy.
4. **Drift:** `ORBIT_CONFIG_DIR` (added in §9 for the integration test) is in the code and the README,
   but not in `design.md ## Decisions` or any spec.
5. **Nit:** the provisioned-marker check on attach treats any `Run` error as "never finished
   provisioning". An OrbStack failure there gives a misleading warning. It should warn so only on
   `*orb.ExitError`.
6. **Deferred nit:** a README known-issue line about workspaces under `/tmp` or `/var/folders`
   (OrbStack's stale-delete behaviour, see the stopped §9 note).

Fix sections are proposed to Emmz but not yet in `tasks.md`. For item 1, the open product question is
whether to refuse overlapping extras (recommended, like the workspace-overlap refusal) or to support
nesting by leaving rw paths out of the ro copy.

## Next

- Waiting on Emmz: agree the fix sections and the overlap behaviour. Then add them to `tasks.md` as
  `## 10. Review fixes: …` and build them with the section loop.
- After that: the change-done note, the offer of code-review, and the offer of `/opsx:archive`.
