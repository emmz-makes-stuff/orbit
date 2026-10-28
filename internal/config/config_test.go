package config

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// newHome returns a canonical temp directory to stand in for the Mac home directory.
func newHome(t *testing.T) string {
	t.Helper()
	return Canonical(t.TempDir())
}

// sortExtras orders expectations the way ExtrasFor does, since the temp home's location decides
// where home-relative paths sort against absolute ones.
func sortExtras(es []Extra) {
	sort.Slice(es, func(i, j int) bool { return es[i].Path < es[j].Path })
}

func writeConfig(t *testing.T, home, body string) {
	t.Helper()
	dir := Dir(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	home := newHome(t)
	cfg, err := Load(Dir(home), home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Distro != DefaultDistro {
		t.Errorf("Distro = %q, want %q", cfg.Distro, DefaultDistro)
	}
	if got := cfg.ExtrasFor(filepath.Join(home, "ws")); len(got) != 0 {
		t.Errorf("ExtrasFor = %v, want none", got)
	}
}

func TestLoadFull(t *testing.T) {
	home := newHome(t)
	writeConfig(t, home, `
distro = "ubuntu:24.04"
cpus = 4
memory = "8G"
disk = "64G"

[[extras]]
path = "~/.gitconfig"

[[extras]]
path = "/opt/shared"
mode = "rw"

[[workspaces."~/github/emmz".extras]]
path = "~/notes/emmz"
mode = "rw"
`)
	cfg, err := Load(Dir(home), home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Distro != "ubuntu:24.04" || cfg.CPUs != 4 || cfg.Memory != "8G" || cfg.Disk != "64G" {
		t.Errorf("settings = %+v", cfg)
	}

	ws := filepath.Join(home, "github", "emmz")
	want := []Extra{
		{Path: filepath.Join(home, ".gitconfig"), Mode: ModeRO},
		{Path: filepath.Join(home, "notes", "emmz"), Mode: ModeRW},
		{Path: "/opt/shared", Mode: ModeRW},
	}
	sortExtras(want)
	if got := cfg.ExtrasFor(ws); !reflect.DeepEqual(got, want) {
		t.Errorf("ExtrasFor(workspace) =\n  %v\nwant\n  %v", got, want)
	}

	other := filepath.Join(home, "github", "other")
	wantOther := []Extra{
		{Path: filepath.Join(home, ".gitconfig"), Mode: ModeRO},
		{Path: "/opt/shared", Mode: ModeRW},
	}
	sortExtras(wantOther)
	if got := cfg.ExtrasFor(other); !reflect.DeepEqual(got, wantOther) {
		t.Errorf("ExtrasFor(other) = %v, want %v", got, wantOther)
	}
}

func TestWorkspaceModeOverridesGlobal(t *testing.T) {
	home := newHome(t)
	writeConfig(t, home, `
[[extras]]
path = "~/shared"

[[workspaces."~/ws".extras]]
path = "~/shared"
mode = "rw"
`)
	cfg, err := Load(Dir(home), home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []Extra{{Path: filepath.Join(home, "shared"), Mode: ModeRW}}
	if got := cfg.ExtrasFor(filepath.Join(home, "ws")); !reflect.DeepEqual(got, want) {
		t.Errorf("ExtrasFor = %v, want %v", got, want)
	}
}

func TestWorkspaceKeysAreCanonical(t *testing.T) {
	home := newHome(t)
	real := filepath.Join(home, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(home, "link")); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, home, `
[[workspaces."~/link/".extras]]
path = "~/x"
`)
	cfg, err := Load(Dir(home), home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.ExtrasFor(real); len(got) != 1 {
		t.Errorf("ExtrasFor(real) = %v, want the extra declared via the symlink", got)
	}
}

func TestSymlinkedExtraKeepsDeclaredPath(t *testing.T) {
	home := newHome(t)
	target := filepath.Join(home, "dotfiles", "gitconfig")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(home, ".gitconfig")); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, home, "[[extras]]\npath = \"~/.gitconfig\"\n")
	cfg, err := Load(Dir(home), home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []Extra{{Path: filepath.Join(home, ".gitconfig"), Mode: ModeRO}}
	if got := cfg.ExtrasFor(filepath.Join(home, "ws")); !reflect.DeepEqual(got, want) {
		t.Errorf("ExtrasFor = %v, want %v", got, want)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name, body, wantErr string
	}{
		{"bad mode", "[[extras]]\npath = \"~/a\"\nmode = \"ro-ish\"\n", "mode must be"},
		{"missing path", "[[extras]]\nmode = \"rw\"\n", "path is required"},
		{"relative path", "[[extras]]\npath = \"a/b\"\n", "must be absolute"},
		{"other user home", "[[extras]]\npath = \"~bob/a\"\n", "only ~ and ~/"},
		{"colon in extra", "[[extras]]\npath = \"~/a:b\"\n", "':' or ','"},
		{"comma in extra", "[[extras]]\npath = \"~/a,b\"\n", "':' or ','"},
		{"colon in workspace", "[[workspaces.\"~/w:x\".extras]]\npath = \"~/a\"\n", "':' or ','"},
		{"unknown key", "distr = \"ubuntu\"\n", "unknown keys: distr"},
		{"unknown extra key", "[[extras]]\npath = \"~/a\"\nmod = \"rw\"\n", "unknown keys: extras.mod"},
		{"negative cpus", "cpus = -1\n", "must not be negative"},
		{"invalid toml", "distro = \n", "toml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := newHome(t)
			writeConfig(t, home, tc.body)
			_, err := Load(Dir(home), home)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Load error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestExpandPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"~", "/home/e"},
		{"~/a/b/", "/home/e/a/b"},
		{"/abs/../x", "/x"},
	}
	for _, tc := range cases {
		got, err := ExpandPath(tc.in, "/home/e")
		if err != nil || got != tc.want {
			t.Errorf("ExpandPath(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

// The workspace is writeable from inside the VM, so nothing in it may add access. Load only ever
// reads ~/.config/orbit; a config file dropped into a workspace is ignored.
func TestWorkspaceConfigHasNoEffect(t *testing.T) {
	home := newHome(t)
	ws := filepath.Join(home, "github", "emmz")
	if err := os.MkdirAll(filepath.Join(ws, ".orbit"), 0o755); err != nil {
		t.Fatal(err)
	}
	planted := "[[extras]]\npath = \"~/.ssh\"\nmode = \"rw\"\n"
	for _, name := range []string{"config.toml", "orbit.toml"} {
		if err := os.WriteFile(filepath.Join(ws, ".orbit", name), []byte(planted), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(ws, "orbit.toml"), []byte(planted), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(ws) // even running from inside the workspace

	cfg, err := Load(Dir(home), home)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, e := range cfg.ExtrasFor(ws) {
		if strings.HasSuffix(e.Path, ".ssh") {
			t.Fatalf("workspace-planted extra %q was loaded", e.Path)
		}
	}
}

func TestDirFromEnv(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }

	if got, err := DirFromEnv("/Users/e", getenv); err != nil || got != "/Users/e/.config/orbit" {
		t.Errorf("unset: %q, %v", got, err)
	}
	env[DirEnv] = "/tmp/orbit-test-config"
	if got, err := DirFromEnv("/Users/e", getenv); err != nil || got != "/tmp/orbit-test-config" {
		t.Errorf("absolute: %q, %v", got, err)
	}
	env[DirEnv] = "rel/dir"
	got, err := DirFromEnv("/Users/e", getenv)
	if err != nil || !filepath.IsAbs(got) || !strings.HasSuffix(got, "/rel/dir") {
		t.Errorf("relative: %q, %v; want it made absolute", got, err)
	}
}

func TestLoadFromOtherDir(t *testing.T) {
	home := newHome(t)
	dir := filepath.Join(home, "elsewhere")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(File(dir), []byte("distro = \"alpine\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir, home)
	if err != nil || cfg.Distro != "alpine" {
		t.Errorf("Load(other dir) = %+v, %v", cfg, err)
	}
}
