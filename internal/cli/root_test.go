package cli

import (
	"archive/tar"
	"errors"
	"io"
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

// vm simulates what runs inside a machine, by recognising the commands orbit sends.
type vm struct {
	user        string
	provisioned bool
	setupExit   int
	baseExit    int
	// steps records provisioning in order: "copy", "baseline", "setup", "marker".
	steps  []string
	copied map[string]string // tar entry name -> contents ("<dir>" or "-> target" for links)
	// baselineInput is what was piped to the baseline step.
	baselineInput string
}

func (v *vm) onRun(r orbtest.Run) error {
	args := r.Args
	switch {
	case reflect.DeepEqual(args, []string{"id", "-un"}):
		_, _ = io.WriteString(r.Stdout, v.user+"\n")
	case len(args) == 3 && args[0] == "test" && args[2] == provisionedMarker:
		if !v.provisioned {
			return &orb.ExitError{Code: 1}
		}
	case len(args) >= 3 && args[0] == "sh" && strings.Contains(args[2], "tar -C / -xpf -"):
		v.steps = append(v.steps, "copy")
		v.copied = readTar(r.Input)
	case len(args) == 3 && args[0] == "sh" && strings.Contains(args[2], "bash -s"):
		v.steps = append(v.steps, "baseline")
		v.baselineInput = r.Input
		if v.baseExit != 0 {
			return &orb.ExitError{Code: v.baseExit}
		}
	case len(args) == 4 && args[0] == "sh" && strings.HasSuffix(args[3], setupScript):
		v.steps = append(v.steps, "setup")
		if v.setupExit != 0 {
			return &orb.ExitError{Code: v.setupExit}
		}
	case len(args) == 3 && args[0] == "sh" && strings.Contains(args[2], "touch "+provisionedMarker):
		v.steps = append(v.steps, "marker")
		v.provisioned = true
	default:
		return errors.New("vm: unexpected command " + strings.Join(args, " "))
	}
	return nil
}

func readTar(data string) map[string]string {
	out := map[string]string{}
	tr := tar.NewReader(strings.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err != nil {
			return out
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			out[hdr.Name] = "<dir>"
		case tar.TypeSymlink:
			out[hdr.Name] = "-> " + hdr.Linkname
		default:
			b, _ := io.ReadAll(tr)
			out[hdr.Name] = string(b)
		}
	}
}

// fixture is a harness with a canonical temp home, a workspace directory and a simulated VM.
type fixture struct {
	*harness
	home, ws string
	vm       *vm
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	h := newHarness(t)
	home := config.Canonical(t.TempDir())
	ws := filepath.Join(home, "github", "emmz")
	mkdir(t, ws)
	f := &fixture{harness: h, home: home, ws: ws, vm: &vm{user: "emmz"}}
	h.deps.Home = home
	h.deps.Getwd = func() (string, error) { return ws, nil }
	h.orb.OnRun = f.vm.onRun
	return f
}

func (f *fixture) cd(dir string) { f.deps.Getwd = func() (string, error) { return dir, nil } }

func (f *fixture) config(t *testing.T, body string) {
	t.Helper()
	writeFile(t, filepath.Join(config.Dir(f.home), "config.toml"), body)
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) mustRun(t *testing.T, args ...string) {
	t.Helper()
	if code := f.run(args...); code != 0 {
		t.Fatalf("orbit %v: exit %d\nstderr: %s", args, code, f.stderr.String())
	}
}

func envValue(env []string, key string) (string, bool) {
	for _, kv := range env {
		if k, v, _ := strings.Cut(kv, "="); k == key {
			return v, true
		}
	}
	return "", false
}

// --- workspace-sessions -----------------------------------------------------------------

func TestFirstRunCreatesProvisionsAndAttaches(t *testing.T) {
	f := newFixture(t)
	writeFile(t, config.BaselinePath(f.home), "apt-get install -y ripgrep\n")
	writeFile(t, filepath.Join(f.ws, setupScript), "make deps\n")

	f.mustRun(t)

	name := workspace.Name(f.ws)
	if len(f.orb.Created) != 1 || f.orb.Created[0].Name != name {
		t.Fatalf("created = %+v, want one VM named %s", f.orb.Created, name)
	}
	if got := f.orb.Created[0].Mounts; !reflect.DeepEqual(got, []string{f.ws}) {
		t.Errorf("mounts = %q, want just the workspace", got)
	}
	if want := []string{"baseline", "setup", "marker"}; !reflect.DeepEqual(f.vm.steps, want) {
		t.Errorf("provisioning steps = %q, want %q", f.vm.steps, want)
	}
	if f.vm.baselineInput != "apt-get install -y ripgrep\n" {
		t.Errorf("baseline input = %q", f.vm.baselineInput)
	}
	if len(f.orb.Shells) != 1 || f.orb.Shells[0].Machine != name || f.orb.Shells[0].Workdir != f.ws {
		t.Errorf("shells = %+v, want one in %s", f.orb.Shells, f.ws)
	}
	// Provisioning finished before the shell opened.
	calls := strings.Join(f.orb.Calls, ",")
	if strings.LastIndex(calls, "run ") > strings.Index(calls, "shell ") {
		t.Errorf("a run came after the shell: %s", calls)
	}
}

func TestSetupRunsAsUserInWorkspace(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(f.ws, setupScript), "true\n")
	f.mustRun(t)
	for _, r := range f.orb.Runs {
		if len(r.Args) == 4 && strings.HasSuffix(r.Args[3], setupScript) {
			if r.User != "" || r.Workdir != f.ws || r.Args[3] != filepath.Join(f.ws, setupScript) {
				t.Errorf("setup run = user %q, workdir %q, script %q", r.User, r.Workdir, r.Args[3])
			}
			return
		}
	}
	t.Fatal("setup.sh never ran")
}

func TestNoBaselineNoSetup(t *testing.T) {
	f := newFixture(t)
	f.mustRun(t)
	if want := []string{"marker"}; !reflect.DeepEqual(f.vm.steps, want) {
		t.Errorf("steps = %q, want %q", f.vm.steps, want)
	}
	if len(f.orb.Shells) != 1 {
		t.Errorf("shell not opened")
	}
}

func TestAttachToExistingFromSubdirectory(t *testing.T) {
	f := newFixture(t)
	name := workspace.Name(f.ws)
	f.orb = orbtest.New(orb.Machine{Name: name, State: orb.StateRunning, Mounts: []orb.Mount{{Source: f.ws}}})
	f.orb.OnRun = f.vm.onRun
	f.vm.provisioned = true
	sub := filepath.Join(f.ws, "orbit", "docs")
	mkdir(t, sub)
	f.cd(sub)

	f.mustRun(t)

	if len(f.orb.Created) != 0 {
		t.Errorf("created %+v, want reuse", f.orb.Created)
	}
	if len(f.orb.Shells) != 1 || f.orb.Shells[0].Workdir != sub {
		t.Errorf("shells = %+v, want one in %s", f.orb.Shells, sub)
	}
	if len(f.vm.steps) != 0 {
		t.Errorf("provisioning ran again on attach: %q", f.vm.steps)
	}
	if strings.Contains(f.stderr.String(), "never finished provisioning") {
		t.Errorf("spurious provisioning warning: %s", f.stderr.String())
	}
}

func TestSymlinkedDirectoryResolvesToWorkspace(t *testing.T) {
	f := newFixture(t)
	name := workspace.Name(f.ws)
	f.orb = orbtest.New(orb.Machine{Name: name, State: orb.StateRunning, Mounts: []orb.Mount{{Source: f.ws}}})
	f.orb.OnRun = f.vm.onRun
	f.vm.provisioned = true
	link := filepath.Join(f.home, "ws-link")
	if err := os.Symlink(f.ws, link); err != nil {
		t.Fatal(err)
	}
	f.cd(link)
	f.mustRun(t)
	if len(f.orb.Created) != 0 || f.orb.Shells[0].Workdir != f.ws {
		t.Errorf("created %+v, shells %+v; want attach in %s", f.orb.Created, f.orb.Shells, f.ws)
	}
}

func TestAttachStartsStoppedVM(t *testing.T) {
	f := newFixture(t)
	name := workspace.Name(f.ws)
	f.orb = orbtest.New(orb.Machine{Name: name, State: orb.StateStopped, Mounts: []orb.Mount{{Source: f.ws}}})
	f.orb.OnRun = f.vm.onRun
	f.vm.provisioned = true

	f.mustRun(t)

	if f.orb.Calls[1] != "start "+name {
		t.Errorf("calls = %q, want start before anything else", f.orb.Calls)
	}
	if m, _ := f.orb.Machine(name); m.State != orb.StateRunning {
		t.Errorf("state = %q", m.State)
	}
}

func TestAttachWarnsWhenNeverProvisioned(t *testing.T) {
	f := newFixture(t)
	name := workspace.Name(f.ws)
	f.orb = orbtest.New(orb.Machine{Name: name, State: orb.StateRunning, Mounts: []orb.Mount{{Source: f.ws}}})
	f.orb.OnRun = f.vm.onRun

	f.mustRun(t)

	if !strings.Contains(f.stderr.String(), "never finished provisioning") ||
		!strings.Contains(f.stderr.String(), "orbit rebuild") {
		t.Errorf("stderr = %q", f.stderr.String())
	}
	if len(f.orb.Shells) != 1 {
		t.Error("shell not opened")
	}
}

func TestOverlapRefused(t *testing.T) {
	f := newFixture(t)
	child := filepath.Join(f.ws, "orbit")
	childName := workspace.Name(child)
	f.orb = orbtest.New(orb.Machine{Name: childName, State: orb.StateRunning, Mounts: []orb.Mount{{Source: child}}})

	if code := f.run(); code == 0 {
		t.Fatal("want non-zero exit")
	}
	msg := f.stderr.String()
	for _, want := range []string{child, childName, "orbit destroy", "inside one of them"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
	if len(f.orb.Created) != 0 || len(f.orb.Shells) != 0 {
		t.Error("created or attached despite the overlap")
	}
}

func TestOrbStackNotInstalled(t *testing.T) {
	f := newFixture(t)
	f.deps.Orb = func() (orb.Client, error) { return nil, orb.ErrNotInstalled }
	if code := f.run(); code == 0 {
		t.Fatal("want non-zero exit")
	}
	if !strings.Contains(f.stderr.String(), "OrbStack is required") {
		t.Errorf("stderr = %q", f.stderr.String())
	}
}

func TestOrbStackUnreachable(t *testing.T) {
	f := newFixture(t)
	f.orb.Errs["list"] = errors.New("orbctl list: failed to start VM")
	if code := f.run(); code == 0 {
		t.Fatal("want non-zero exit")
	}
	if !strings.Contains(f.stderr.String(), "could not reach OrbStack: orbctl list: failed to start VM") {
		t.Errorf("stderr = %q", f.stderr.String())
	}
}

// --- vm-access --------------------------------------------------------------------------

func TestExtras(t *testing.T) {
	f := newFixture(t)
	rw := filepath.Join(f.home, "notes")
	mkdir(t, rw)
	writeFile(t, filepath.Join(f.home, "dotfiles", "gitconfig"), "[user]\n")
	if err := os.Symlink(filepath.Join(f.home, "dotfiles", "gitconfig"), filepath.Join(f.home, ".gitconfig")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(f.home, ".config", "tool", "a.toml"), "a=1\n")
	if err := os.Symlink("a.toml", filepath.Join(f.home, ".config", "tool", "link.toml")); err != nil {
		t.Fatal(err)
	}
	f.config(t, `
[[extras]]
path = "~/.gitconfig"

[[extras]]
path = "~/.config/tool"

[[workspaces."~/github/emmz".extras]]
path = "~/notes"
mode = "rw"
`)
	f.mustRun(t)

	if got, want := f.orb.Created[0].Mounts, []string{f.ws, rw}; !reflect.DeepEqual(got, want) {
		t.Errorf("mounts = %q, want workspace first, then rw extras: %q", got, want)
	}
	rel := func(p string) string { return strings.TrimPrefix(p, "/") }
	want := map[string]string{
		rel(filepath.Join(f.home, ".gitconfig")):                   "[user]\n", // symlink dereferenced
		rel(filepath.Join(f.home, ".config", "tool")) + "/":        "<dir>",
		rel(filepath.Join(f.home, ".config", "tool", "a.toml")):    "a=1\n",
		rel(filepath.Join(f.home, ".config", "tool", "link.toml")): "-> a.toml", // inner link kept
	}
	if !reflect.DeepEqual(f.vm.copied, want) {
		t.Errorf("copied =\n  %v\nwant\n  %v", f.vm.copied, want)
	}
	if f.vm.steps[0] != "copy" {
		t.Errorf("steps = %q, want copy first", f.vm.steps)
	}
	for _, r := range f.orb.Runs {
		if len(r.Args) == 3 && strings.Contains(r.Args[2], "tar -C /") {
			if r.User != "root" || !strings.Contains(r.Args[2], "chown -R -h 'emmz'") ||
				!strings.Contains(r.Args[2], "rm -rf -- ") {
				t.Errorf("copy run = user %q script %q", r.User, r.Args[2])
			}
		}
	}
}

func TestMissingExtraCreatesNothing(t *testing.T) {
	f := newFixture(t)
	f.config(t, "[[extras]]\npath = \"~/nope\"\n")
	if code := f.run(); code == 0 {
		t.Fatal("want non-zero exit")
	}
	if !strings.Contains(f.stderr.String(), filepath.Join(f.home, "nope")) {
		t.Errorf("stderr doesn't name the extra: %q", f.stderr.String())
	}
	if len(f.orb.Created) != 0 {
		t.Error("a VM was created")
	}
}

func TestExtraOverlappingWorkspaceRefused(t *testing.T) {
	f := newFixture(t)
	f.config(t, "[[extras]]\npath = \"~/github\"\nmode = \"rw\"\n")
	if code := f.run(); code == 0 {
		t.Fatal("want non-zero exit")
	}
	if !strings.Contains(f.stderr.String(), "overlaps the workspace") || len(f.orb.Created) != 0 {
		t.Errorf("stderr = %q, created = %d", f.stderr.String(), len(f.orb.Created))
	}
}

func TestHomeAndAncestorsRefused(t *testing.T) {
	f := newFixture(t)
	for _, dir := range []string{f.home, filepath.Dir(f.home), "/"} {
		f.stderr.Reset()
		f.cd(dir)
		if code := f.run(); code == 0 {
			t.Errorf("orbit in %s: exit 0, want refusal", dir)
		}
		if !strings.Contains(f.stderr.String(), "expose everything in your home") {
			t.Errorf("orbit in %s: stderr = %q", dir, f.stderr.String())
		}
	}
	if len(f.orb.Created) != 0 {
		t.Errorf("created %d VMs", len(f.orb.Created))
	}
	// A subdirectory of home is fine (the fixture's workspace is ~/github/emmz).
	f.cd(f.ws)
	f.mustRun(t)
	if len(f.orb.Created) != 1 {
		t.Error("subdirectory of home was not created")
	}
}

func TestUnmountableWorkspaceRefused(t *testing.T) {
	f := newFixture(t)
	odd := filepath.Join(f.home, "a:b")
	mkdir(t, odd)
	f.cd(odd)
	if code := f.run(); code == 0 || len(f.orb.Created) != 0 {
		t.Fatalf("exit %d, created %d; want refusal", code, len(f.orb.Created))
	}
}

func TestSSHAgentWarning(t *testing.T) {
	for _, hasKeys := range []bool{false, true} {
		f := newFixture(t)
		f.deps.SSHAgentHasKeys = func() bool { return hasKeys }
		f.mustRun(t)
		warned := strings.Contains(f.stderr.String(), "ssh-add --apple-use-keychain")
		if warned == hasKeys {
			t.Errorf("hasKeys=%v: warned=%v, stderr %q", hasKeys, warned, f.stderr.String())
		}
		if len(f.orb.Shells) != 1 {
			t.Errorf("hasKeys=%v: shell not opened", hasKeys)
		}
	}
}

// --- vm-provisioning --------------------------------------------------------------------

func TestSetupFailureKeepsVMAndNamesScript(t *testing.T) {
	f := newFixture(t)
	writeFile(t, filepath.Join(f.ws, setupScript), "exit 1\n")
	f.vm.setupExit = 1

	if code := f.run(); code == 0 {
		t.Fatal("want non-zero exit")
	}
	msg := f.stderr.String()
	if !strings.Contains(msg, setupScript+" exited 1") || !strings.Contains(msg, "orbit rebuild") {
		t.Errorf("stderr = %q", msg)
	}
	if _, ok := f.orb.Machine(workspace.Name(f.ws)); !ok {
		t.Error("VM was not kept")
	}
	if len(f.orb.Shells) != 0 || f.vm.provisioned {
		t.Error("attached or marked provisioned after a failure")
	}
}

func TestBaselineFailureStopsBeforeSetup(t *testing.T) {
	f := newFixture(t)
	writeFile(t, config.BaselinePath(f.home), "exit 7\n")
	writeFile(t, filepath.Join(f.ws, setupScript), "true\n")
	f.vm.baseExit = 7
	if code := f.run(); code != 1 {
		t.Fatalf("exit %d, want 1 (orbit's own failure, not the script's code)", code)
	}
	if !strings.Contains(f.stderr.String(), "baseline.sh exited 7") {
		t.Errorf("stderr = %q", f.stderr.String())
	}
	if want := []string{"baseline"}; !reflect.DeepEqual(f.vm.steps, want) {
		t.Errorf("steps = %q, want setup skipped", f.vm.steps)
	}
}

// --- claude-auth ------------------------------------------------------------------------

func TestTokenInjected(t *testing.T) {
	f := newFixture(t)
	f.keys.token, f.keys.stored = token, true
	f.mustRun(t)
	env := f.orb.Shells[0].Env
	if v, _ := envValue(env, tokenEnv); v != token {
		t.Errorf("%s = %q, want the stored token", tokenEnv, v)
	}
	if v, _ := envValue(env, "ORBENV"); v != tokenEnv {
		t.Errorf("ORBENV = %q, want %s", v, tokenEnv)
	}
	if v, _ := envValue(env, "HOME"); v != "/Users/e" {
		t.Error("the rest of the environment was dropped")
	}
	if strings.Contains(f.stderr.String(), "orbit auth") {
		t.Errorf("warned despite a stored token: %q", f.stderr.String())
	}
}

func TestNoTokenWarnsAndAttaches(t *testing.T) {
	f := newFixture(t)
	f.mustRun(t)
	if _, set := envValue(f.orb.Shells[0].Env, tokenEnv); set {
		t.Errorf("%s set without a stored token", tokenEnv)
	}
	if !strings.Contains(f.stderr.String(), "orbit auth") {
		t.Errorf("stderr = %q, want a warning naming orbit auth", f.stderr.String())
	}
}

func TestTokenNeverOnACommandLine(t *testing.T) {
	f := newFixture(t)
	f.keys.token, f.keys.stored = token, true
	writeFile(t, config.BaselinePath(f.home), "true\n")
	f.mustRun(t)
	for _, r := range f.orb.Runs {
		if strings.Contains(strings.Join(r.Args, " "), token) || strings.Contains(r.Input, token) {
			t.Errorf("token passed into the VM by a run: %q", r.Args)
		}
	}
}
