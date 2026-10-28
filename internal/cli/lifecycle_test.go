package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/emmz-makes-stuff/orbit/internal/config"
	"github.com/emmz-makes-stuff/orbit/internal/orb"
	"github.com/emmz-makes-stuff/orbit/internal/orb/orbtest"
	"github.com/emmz-makes-stuff/orbit/internal/workspace"
)

// withVMs replaces the fixture's OrbStack with one holding these machines.
func (f *fixture) withVMs(ms ...orb.Machine) {
	f.orb = orbtest.New(ms...)
	f.orb.OnRun = f.vm.onRun
}

func vmFor(ws, state string) orb.Machine {
	return orb.Machine{Name: workspace.Name(ws), State: state, Mounts: []orb.Mount{{Source: ws, Destination: ws}}}
}

// --- target selection -------------------------------------------------------------------

func TestTargetImplicitFromSubdirectory(t *testing.T) {
	f := newFixture(t)
	f.withVMs(vmFor(f.ws, orb.StateRunning))
	sub := filepath.Join(f.ws, "orbit")
	mkdir(t, sub)
	f.cd(sub)
	f.mustRun(t, "stop")
	if m, _ := f.orb.Machine(workspace.Name(f.ws)); m.State != orb.StateStopped {
		t.Errorf("state = %q, want stopped", m.State)
	}
}

func TestTargetUnmanagedNameRefused(t *testing.T) {
	f := newFixture(t)
	f.withVMs(orb.Machine{Name: "ubuntu", State: orb.StateRunning})
	if code := f.run("destroy", "--yes", "ubuntu"); code == 0 {
		t.Fatal("want non-zero exit")
	}
	if _, ok := f.orb.Machine("ubuntu"); !ok {
		t.Fatal("unmanaged machine was deleted")
	}
	if !strings.Contains(f.stderr.String(), "not an orbit workspace VM") {
		t.Errorf("stderr = %q", f.stderr.String())
	}
}

func TestTargetUnknownName(t *testing.T) {
	f := newFixture(t)
	if code := f.run("stop", "orbit-nope-000000"); code == 0 {
		t.Fatal("want non-zero exit")
	}
	if !strings.Contains(f.stderr.String(), "no workspace VM named orbit-nope-000000") {
		t.Errorf("stderr = %q", f.stderr.String())
	}
}

func TestTargetNoWorkspaceHere(t *testing.T) {
	f := newFixture(t)
	if code := f.run("stop"); code == 0 {
		t.Fatal("want non-zero exit")
	}
	if !strings.Contains(f.stderr.String(), "no workspace covers "+f.ws) {
		t.Errorf("stderr = %q", f.stderr.String())
	}
	for _, c := range f.orb.Calls {
		if strings.HasPrefix(c, "stop ") {
			t.Errorf("stopped something: %q", f.orb.Calls)
		}
	}
}

// --- list -------------------------------------------------------------------------------

func TestList(t *testing.T) {
	f := newFixture(t)
	gone := filepath.Join(f.home, "moved-away")
	f.withVMs(
		vmFor(f.ws, orb.StateRunning),
		vmFor(gone, orb.StateStopped),
		orb.Machine{Name: "ubuntu", State: orb.StateRunning},
	)
	f.mustRun(t, "list")
	out := f.stdout.String()
	for _, want := range []string{workspace.Name(f.ws), f.ws, "running", workspace.Name(gone), "stopped", "orphaned"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ubuntu") {
		t.Errorf("unmanaged machine listed:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, f.ws) && strings.Contains(line, "orphaned") {
			t.Errorf("existing workspace flagged orphaned: %q", line)
		}
	}
}

func TestListJSON(t *testing.T) {
	f := newFixture(t)
	gone := filepath.Join(f.home, "moved-away")
	f.withVMs(vmFor(f.ws, orb.StateRunning), vmFor(gone, orb.StateStopped))
	f.mustRun(t, "list", "--json")
	var got []listItem
	if err := json.Unmarshal(f.stdout.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, f.stdout.String())
	}
	want := []listItem{
		{Name: workspace.Name(f.ws), Workspace: f.ws, State: orb.StateRunning},
		{Name: workspace.Name(gone), Workspace: gone, State: orb.StateStopped, Orphaned: true},
	}
	if workspace.Name(gone) < workspace.Name(f.ws) {
		want[0], want[1] = want[1], want[0]
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("list --json =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestListEmptyJSONIsArray(t *testing.T) {
	f := newFixture(t)
	f.mustRun(t, "list", "--json")
	if strings.TrimSpace(f.stdout.String()) != "[]" {
		t.Errorf("output = %q, want []", f.stdout.String())
	}
}

// --- stop / start -----------------------------------------------------------------------

func TestStopAndStartAreIdempotent(t *testing.T) {
	f := newFixture(t)
	name := workspace.Name(f.ws)
	f.withVMs(vmFor(f.ws, orb.StateStopped))
	f.mustRun(t, "stop")
	f.mustRun(t, "start")
	f.mustRun(t, "start")
	var ops []string
	for _, c := range f.orb.Calls {
		if c != "list " {
			ops = append(ops, c)
		}
	}
	if want := []string{"start " + name}; !reflect.DeepEqual(ops, want) {
		t.Errorf("ops = %q, want only one real start", ops)
	}
	if !strings.Contains(f.stdout.String(), "already stopped") || !strings.Contains(f.stdout.String(), "already running") {
		t.Errorf("stdout = %q", f.stdout.String())
	}
	if len(f.orb.Shells) != 0 {
		t.Error("start opened a shell")
	}
}

// --- destroy ----------------------------------------------------------------------------

func TestDestroyConfirmed(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(f.ws, "keep.txt"), "mine")
	f.withVMs(vmFor(f.ws, orb.StateRunning))
	f.deps.StdinIsTerminal = func() bool { return true }
	f.deps.Stdin = strings.NewReader("y\n")
	f.mustRun(t, "destroy")
	if _, ok := f.orb.Machine(workspace.Name(f.ws)); ok {
		t.Error("VM still exists")
	}
	if b, err := os.ReadFile(filepath.Join(f.ws, "keep.txt")); err != nil || string(b) != "mine" {
		t.Errorf("workspace file changed: %q %v", b, err)
	}
	if !strings.Contains(f.stderr.String(), "Files on your Mac are not touched") {
		t.Errorf("prompt = %q", f.stderr.String())
	}
}

func TestDestroyDeclined(t *testing.T) {
	f := newFixture(t)
	f.withVMs(vmFor(f.ws, orb.StateRunning))
	f.deps.StdinIsTerminal = func() bool { return true }
	f.deps.Stdin = strings.NewReader("n\n")
	if code := f.run("destroy"); code == 0 {
		t.Fatal("want non-zero exit when declined")
	}
	if _, ok := f.orb.Machine(workspace.Name(f.ws)); !ok {
		t.Error("VM deleted despite the answer")
	}
}

func TestDestroyNonInteractiveNeedsYes(t *testing.T) {
	f := newFixture(t)
	f.withVMs(vmFor(f.ws, orb.StateRunning))
	if code := f.run("destroy"); code == 0 {
		t.Fatal("want non-zero exit without --yes")
	}
	if _, ok := f.orb.Machine(workspace.Name(f.ws)); !ok {
		t.Fatal("VM deleted without confirmation")
	}
	if !strings.Contains(f.stderr.String(), "--yes") {
		t.Errorf("stderr = %q", f.stderr.String())
	}
	f.mustRun(t, "destroy", "--yes")
	if _, ok := f.orb.Machine(workspace.Name(f.ws)); ok {
		t.Error("--yes did not destroy")
	}
}

// --- rebuild ----------------------------------------------------------------------------

func TestRebuildResetsAndReprovisions(t *testing.T) {
	f := newFixture(t)
	writeFile(t, config.BaselinePath(f.deps.ConfigDir), "true\n")
	writeFile(t, filepath.Join(f.ws, setupScript), "true\n")
	f.withVMs(vmFor(f.ws, orb.StateRunning))
	f.mustRun(t, "rebuild", "--yes")

	name := workspace.Name(f.ws)
	var ops []string
	for _, c := range f.orb.Calls {
		if strings.HasPrefix(c, "delete ") || strings.HasPrefix(c, "create ") {
			ops = append(ops, c)
		}
	}
	if want := []string{"delete " + name, "create " + name}; !reflect.DeepEqual(ops, want) {
		t.Errorf("ops = %q, want delete then create", ops)
	}
	if want := []string{"baseline", "setup", "marker"}; !reflect.DeepEqual(f.vm.steps, want) {
		t.Errorf("steps = %q, want full provisioning", f.vm.steps)
	}
	if len(f.orb.Shells) != 0 {
		t.Error("rebuild opened a shell")
	}
}

func TestRebuildPicksUpNewExtra(t *testing.T) {
	f := newFixture(t)
	f.withVMs(vmFor(f.ws, orb.StateRunning))
	notes := filepath.Join(f.home, "notes")
	mkdir(t, notes)
	f.config(t, "[[extras]]\npath = \"~/notes\"\nmode = \"rw\"\n")
	f.mustRun(t, "rebuild", "--yes")
	if got := f.orb.Created[0].Mounts; !reflect.DeepEqual(got, []orb.Mount{{Source: f.ws}, {Source: notes, Destination: f.vm.home + "/notes"}}) {
		t.Errorf("mounts = %v", got)
	}
}

func TestRebuildOrphanRefused(t *testing.T) {
	f := newFixture(t)
	gone := filepath.Join(f.home, "moved-away")
	f.withVMs(vmFor(gone, orb.StateRunning))
	if code := f.run("rebuild", "--yes", workspace.Name(gone)); code == 0 {
		t.Fatal("want non-zero exit")
	}
	if _, ok := f.orb.Machine(workspace.Name(gone)); !ok {
		t.Error("orphan was destroyed")
	}
	if !strings.Contains(f.stderr.String(), "missing") {
		t.Errorf("stderr = %q", f.stderr.String())
	}
}

func TestRebuildChecksPlanBeforeDestroying(t *testing.T) {
	f := newFixture(t)
	f.withVMs(vmFor(f.ws, orb.StateRunning))
	f.config(t, "[[extras]]\npath = \"~/nope\"\n")
	if code := f.run("rebuild", "--yes"); code == 0 {
		t.Fatal("want non-zero exit")
	}
	if _, ok := f.orb.Machine(workspace.Name(f.ws)); !ok {
		t.Error("old VM destroyed although the new one couldn't be built")
	}
}

func TestRebuildNonInteractiveNeedsYes(t *testing.T) {
	f := newFixture(t)
	f.withVMs(vmFor(f.ws, orb.StateRunning))
	if code := f.run("rebuild"); code == 0 {
		t.Fatal("want non-zero exit")
	}
	if len(f.orb.Created) != 0 {
		t.Error("rebuilt without confirmation")
	}
}

// --- sync -------------------------------------------------------------------------------

func TestSyncRecopiesReadOnlyExtras(t *testing.T) {
	f := newFixture(t)
	f.withVMs(vmFor(f.ws, orb.StateStopped))
	cfg := filepath.Join(f.home, ".gitconfig")
	writeFile(t, cfg, "v2\n")
	notes := filepath.Join(f.home, "notes")
	mkdir(t, notes)
	f.config(t, "[[extras]]\npath = \"~/.gitconfig\"\n\n[[extras]]\npath = \"~/notes\"\nmode = \"rw\"\n")

	f.mustRun(t, "sync")

	if want := map[string]string{strings.TrimPrefix(f.vm.home, "/") + "/.gitconfig": "v2\n"}; !reflect.DeepEqual(f.vm.copied, want) {
		t.Errorf("copied = %v, want only the ro extra, current contents", f.vm.copied)
	}
	for _, r := range f.orb.Runs {
		if len(r.Args) == 3 && strings.Contains(r.Args[2], "tar -C /") {
			if i, j := strings.Index(r.Args[2], "rm -rf"), strings.Index(r.Args[2], "tar -C /"); i < 0 || i > j {
				t.Errorf("old copy not removed before extraction: %q", r.Args[2])
			}
		}
	}
	if m, _ := f.orb.Machine(workspace.Name(f.ws)); m.State != orb.StateRunning {
		t.Error("stopped VM was not started for sync")
	}
}

func TestSyncRefusesOverlappingExtras(t *testing.T) {
	f := newFixture(t)
	f.withVMs(vmFor(f.ws, orb.StateRunning))
	mkdir(t, filepath.Join(f.home, ".config", "gh"))
	f.config(t, "[[extras]]\npath = \"~/.config\"\n\n[[extras]]\npath = \"~/.config/gh\"\nmode = \"rw\"\n")
	if code := f.run("sync"); code == 0 {
		t.Fatal("want non-zero exit")
	}
	if !strings.Contains(f.stderr.String(), "nothing was synced") || len(f.vm.steps) != 0 {
		t.Errorf("stderr = %q, steps = %q", f.stderr.String(), f.vm.steps)
	}
}

func TestSyncByNameWithoutWorkspaceMount(t *testing.T) {
	f := newFixture(t)
	f.withVMs(orb.Machine{Name: "orbit-bare-000000", State: orb.StateRunning})
	writeFile(t, filepath.Join(f.home, ".gitconfig"), "g\n")
	f.config(t, "[[extras]]\npath = \"~/.gitconfig\"\n")
	f.mustRun(t, "sync", "orbit-bare-000000")
	if len(f.vm.copied) != 1 {
		t.Errorf("copied = %v", f.vm.copied)
	}
}

func TestSyncNothingToDo(t *testing.T) {
	f := newFixture(t)
	f.withVMs(vmFor(f.ws, orb.StateRunning))
	f.mustRun(t, "sync")
	if !strings.Contains(f.stdout.String(), "no read-only extras") || len(f.vm.steps) != 0 {
		t.Errorf("stdout = %q, steps = %q", f.stdout.String(), f.vm.steps)
	}
}
