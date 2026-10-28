# Decision: refuse the home directory and its ancestors as a workspace

<!-- Section: 6 · Tasks: 6.4 · Base: 18605e5 -->

Answers the question in `20261028-1535-section-6-done.md`. Emmz chose **"Refuse ~ and /"**.

- New requirement in `specs/workspace-sessions`: "Refuse the home directory as a workspace", with
  scenarios for `~`, `/`/`/Users`, and an allowed subdirectory. New task 6.4.
- Implemented in `planFor`: if `workspace.Within(Canonical(home), ws)`, refuse, with a message saying
  it would expose everything in `~`, `~/.ssh` included. I widened it from "`~` and `/`" to **any
  ancestor of home**, because `/Users` exposes `~` just as much. That's in the spirit of her answer
  and noted here.
- Only the create path checks this. A workspace like that can't exist to be attached to, unless it was
  made by hand in OrbStack, and then resolution ignores it as long as its name lacks the `orbit-`
  prefix.
- Test `TestHomeAndAncestorsRefused`. Gates: `BUILD_EXIT:0` `TEST_EXIT:0` `FORMAT_EXIT:0`
  `LINT_EXIT:0` `VALIDATE_EXIT:0` `GATES_EXIT:0`.

## Next

- §7, task 7.1: target selection for stop/start/destroy/rebuild/sync. An explicit name must be
  managed. Otherwise resolve the current directory, and fail if no workspace covers it. Then 7.2–7.5.
  `rebuild` = destroy + `session.create`. `sync` = `copyExtras` with the current `ro` extras.
- §6 is not yet exercised against real OrbStack. That happens in §9.
