# Stopped: §9 needs Emmz (token run of 9.1, and 9.2 push)

<!-- Section: 9 · Tasks: 9.1–9.2 · Base: bddf9b5 -->

Uncommitted, to land in the §9 commit:

- `ORBIT_CONFIG_DIR` env override: `internal/config/config.go`, `internal/cli/cli.go`, tests, and the
  README. The integration test needs it so it can use a scratch config without touching
  `~/.config/orbit`.
- `integration/orbit_test.go` (`//go:build integration`). Set `ORBIT_INTEGRATION_KEEP=1` to keep a
  failing run's VM for inspection.

9.1 result: `go test -tags integration -count=1 -v ./integration/...` passed on real OrbStack:
`--- PASS: TestOrbitEndToEnd (24.06s)`. But **no Claude token was stored in the Keychain**, so the run
took the "no token, warning names `orbit auth`" branch. The token-in-env and not-on-disk assertions,
which 9.1 requires, never ran. 9.1 stays unticked until a run with a token stored.

Gates: `GATES_EXIT:0`, and `go vet -tags integration ./integration/` is clean.

Gotcha found and fixed in the test, not in orbit: under `/var/folders` (where `t.TempDir` lives),
OrbStack's file sharing won't let a VM create a name that the VM has looked up and the Mac then
deleted. You get `Directory nonexistent`/ENOENT until the Mac recreates the file. The stale entry even
survives into a brand-new VM, so it's cached on OrbStack's Mac side, by path. Under `/Users` the same
sequence clears in about a second. Step 5 used to delete `setup-ran.txt` on the Mac before
`rebuild`, which hit this every time. Now setup.sh appends one line per run, and the test expects
the line twice after rebuild. Real workspaces are under `~`, so orbit itself is unaffected. This might be
worth a README known-issue line if workspaces under `/tmp` are ever expected; not done.

Scratch VMs and repro directories from the investigation are deleted. Only Emmz's `ubuntu` machine
remains.

## Next

- Waiting on Emmz:
  1. Store a token (`claude setup-token`, then `orbit auth`) and rerun the 9.1 test. A pass with
     `TOKEN_HASH` matching means 9.1 can be ticked.
  2. 9.2: push from a VM to a scratch remote, and report the result.
- Then: tick 9.1–9.2, write a section-9-done note that records the push result, and make one §9 commit.
  Then the handoff, the whole-change review, the offer of code-review, and the offer of `/opsx:archive`.
