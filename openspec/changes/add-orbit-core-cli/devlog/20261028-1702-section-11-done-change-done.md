# Section 11 done, and the change is done

<!-- Section: 11 · Tasks: 11.1–11.3 · Base: 926bbde -->

§11:
- **11.1:** `ORBIT_CONFIG_DIR` is recorded as an "Override" paragraph in design.md D7 (rejected: a
  `--config` flag, and redirecting `$HOME` in tests). There's a new vm-access scenario, "Config
  directory override". Already covered by `TestDirFromEnv` and `TestLoadFromOtherDir`.
- **11.2:** the attach marker check warns "never finished provisioning" only on `*orb.ExitError`. Any
  other failure warns "could not check whether … finished provisioning: <err>" and still attaches.
  Test: `TestAttachMarkerCheckFailureIsNotCalledUnprovisioned`.
- **11.3:** the README has a "Known issues" section on workspaces under `/tmp` and `/var/folders`.

Gates: `GATES_EXIT:0`.

**Change done.** All 11 sections are ticked:
- §1–§9 are the build.
- §10–§11 are fixes from the whole-change review (`20261028-1700-whole-change-review.md`).
- The integration test passed on Emmz's Mac with a token stored, and Emmz confirmed git push from a
  VM through the forwarded agent (see `20261028-1640-section-9-done.md`).

Parked for a later change, or for Emmz:
- **GitHub cleanup:** the remote's default branch is `scratch/push-check`, left by the 9.2 check on an
  empty repo. Push the real branch, switch the default, then delete `scratch/push-check`.
- **From DISCOVERY "later":** publishing, scoped git credentials, other agents.
- The integration test wasn't rerun after §10–§11. The changes are covered by unit tests, and none of
  them touch the paths the test drives, unless a config has overlapping extras.

## Next

- Offer the `code-review` skill on `change/add-orbit-core-cli`. If it finds anything, add fix sections
  to tasks.md (CLAUDE.md "Finishing a change", step 3).
- Once review is done or declined: offer `/opsx:archive` and wait for a yes.
