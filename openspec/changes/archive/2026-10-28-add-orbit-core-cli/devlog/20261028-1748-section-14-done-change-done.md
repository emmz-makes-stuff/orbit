# Section 14 done, and the change is done again after the code-review fixes

<!-- Section: 14 · Tasks: 14.1–14.5 · Base: d893a95 -->

§14:
- **14.1:** `orb.output` no longer wraps an `*ExitError` when orbctl says nothing. It now reports
  "orbctl <cmd>: exited N without saying why". `cli.Run` drops the exit-code passthrough: nothing
  real ever reached it, because a shell's code passes through `exec`. Every error now prints
  `orbit: …` and exits 1. Tests: `TestSilentOrbctlFailureIsNotAnExitError` and
  `TestRunPrintsEveryError` (which replaces `TestRunMapsExitErrors`).
- **14.2:** only exit 1 from the marker check means "never finished provisioning". The test table
  adds an exit-255 case.
- **14.3:** `runRebuild` sets `p.name = m.Name`. Test: `TestRebuildKeepsTheVMName`.
- **14.4:** the code landed in §12 (`vmUser`). The test (`TestDefaultUserWithoutHomeIsAClearError`)
  was added here.
- **14.5:** `confirmedCmd` wraps `targetCmd` with a closure that carries `--yes`.

Integration test on real OrbStack: `--- PASS: TestOrbitEndToEnd (26.18s)`. Afterwards only Emmz's
`ubuntu` machine remained, and the `~/.orbit-integration-*` scratch directory was gone.

Gates: `GATES_EXIT:0`. `go vet -tags integration ./integration/` is clean.

**Change done.** All 14 sections are ticked:
- §1–§9 are the build.
- §10–§11 are fixes from the whole-change self-review.
- §12–§14 are fixes from the `code-review` skill (`20261028-1725-code-review-findings.md`).

Parked, unchanged from `20261028-1702-section-11-done-change-done.md`:
- The GitHub default branch is `scratch/push-check`.
- From DISCOVERY "later": publishing, scoped git credentials, other agents.

## Next

- Offer `/opsx:archive` and wait for a yes. A re-review of §12–§14 is optional, if Emmz wants one.
