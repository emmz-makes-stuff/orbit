//go:build integration

// Package integration drives the real orbit binary against real OrbStack. It creates and deletes
// one scratch workspace VM and takes a few minutes. Run by hand, not as a gate:
//
//	go test -tags integration -v ./integration/...
//
// It reads, but never writes, the Claude token in your Keychain.
package integration

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emmz-makes-stuff/orbit/internal/config"
	"github.com/emmz-makes-stuff/orbit/internal/workspace"
)

type env struct {
	t         *testing.T
	bin       string
	ws        string
	configDir string
}

type result struct {
	code   int
	stdout string
	stderr string
	vars   map[string]string // KEY=value lines from stdout
}

// orbit runs the binary in dir with stdin, using the scratch config.
func (e *env) orbit(dir, stdin string, args ...string) result {
	e.t.Helper()
	cmd := exec.Command(e.bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), config.DirEnv+"="+e.configDir)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	r := result{stdout: stdout.String(), stderr: stderr.String(), vars: map[string]string{}}
	if exitErr, ok := err.(*exec.ExitError); ok {
		r.code = exitErr.ExitCode()
	} else if err != nil {
		e.t.Fatalf("run orbit %v: %v", args, err)
	}
	sc := bufio.NewScanner(strings.NewReader(r.stdout))
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok && k != "" && strings.ToUpper(k) == k {
			r.vars[k] = v
		}
	}
	e.t.Logf("orbit %v → exit %d\n--- stdout\n%s--- stderr\n%s", args, r.code, r.stdout, r.stderr)
	return r
}

func (e *env) mustOrbit(dir, stdin string, args ...string) result {
	e.t.Helper()
	r := e.orbit(dir, stdin, args...)
	if r.code != 0 {
		e.t.Fatalf("orbit %v: exit %d", args, r.code)
	}
	return r
}

func (r result) want(t *testing.T, key, value string) {
	t.Helper()
	if got := r.vars[key]; got != value {
		t.Errorf("%s = %q, want %q", key, got, value)
	}
}

type listItem struct {
	Name      string `json:"name"`
	Workspace string `json:"workspace"`
	State     string `json:"state"`
	Orphaned  bool   `json:"orphaned"`
}

func (e *env) find(name string) (listItem, bool) {
	e.t.Helper()
	r := e.mustOrbit(e.ws, "", "list", "--json")
	var items []listItem
	if err := json.Unmarshal([]byte(r.stdout), &items); err != nil {
		e.t.Fatalf("list --json: %v", err)
	}
	for _, it := range items {
		if it.Name == name {
			return it, true
		}
	}
	return listItem{}, false
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// storedTokenHash returns a short hash of the Keychain token, or "" if none is stored.
func storedTokenHash(t *testing.T) string {
	out, err := exec.Command("/usr/bin/security", "find-generic-password", "-s", "orbit", "-a", "claude-oauth-token", "-w").Output()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(bytes.TrimRight(out, "\n"))
	return hex.EncodeToString(sum[:])[:16]
}

func TestOrbitEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("orbctl"); err != nil {
		t.Skip("orbctl not installed")
	}
	root := config.Canonical(t.TempDir())
	bin := filepath.Join(root, "orbit")
	build := exec.Command("go", "build", "-o", bin, "../cmd/orbit")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	e := &env{t: t, bin: bin, ws: filepath.Join(root, "ws"), configDir: filepath.Join(root, "config")}
	sub := filepath.Join(e.ws, "sub")
	roFile := filepath.Join(root, "extras", "ro.txt")
	rwDir := filepath.Join(root, "extras", "rw")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rwDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, roFile, "ro-v1\n")
	// Extras under the Mac home land under the VM user's home; the ones above keep their path.
	macHome, _ := os.UserHomeDir()
	homeDir, err := os.MkdirTemp(macHome, ".orbit-integration-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(homeDir) })
	homeRel := filepath.Base(homeDir)
	homeRO := filepath.Join(homeDir, "ro.txt")
	homeRW := filepath.Join(homeDir, "rw")
	write(t, homeRO, "home-ro\n")
	if err := os.MkdirAll(homeRW, 0o755); err != nil {
		t.Fatal(err)
	}
	var cfg strings.Builder
	for _, x := range []struct{ path, mode string }{{roFile, "ro"}, {rwDir, "rw"}, {homeRO, "ro"}, {homeRW, "rw"}} {
		fmt.Fprintf(&cfg, "[[extras]]\npath = %q\nmode = %q\n\n", x.path, x.mode)
	}
	write(t, config.File(e.configDir), cfg.String())
	write(t, config.BaselinePath(e.configDir),
		"echo baseline-ran > /var/tmp/orbit-baseline\necho \"$ORBIT_USER\" > /var/tmp/orbit-user\n")
	// setup.sh appends rather than the test deleting its output before a rebuild: under
	// /var/folders, OrbStack's file sharing won't let a VM recreate a name the Mac just deleted.
	write(t, filepath.Join(e.ws, ".orbit", "setup.sh"), "echo \"$(pwd) $(id -un)\" >> setup-ran.txt\n")

	name := workspace.Name(e.ws)
	t.Cleanup(func() {
		if t.Failed() && os.Getenv("ORBIT_INTEGRATION_KEEP") != "" {
			t.Logf("kept %s for inspection (workspace %s); delete it with `orbctl delete -f %s`", name, e.ws, name)
			return
		}
		_ = exec.Command("orbctl", "delete", "-f", name).Run()
	})
	if _, exists := e.find(name); exists {
		t.Fatalf("%s already exists; delete it first", name)
	}
	macUser := filepath.Base(macHome)
	setupLine := e.ws + " " + macUser + "\n"
	tokenHash := storedTokenHash(t)

	t.Log("1. first run: create, provision, attach")
	r := e.mustOrbit(e.ws, fmt.Sprintf(`
echo "PWD=$(pwd)"
echo "WHO=$(id -un)"
if [ -e /mnt/mac/Users ] || [ -e %[1]q ]; then echo MAC_HOME=visible; else echo MAC_HOME=hidden; fi
echo from-vm > vm.txt
echo "RO=$(cat %[2]q)"
echo changed-in-vm > %[2]q
echo rw-from-vm > %[3]q/x
echo "HOME_RO=$(cat ~/%[4]s/ro.txt)"
echo home-rw-from-vm > ~/%[4]s/rw/y
echo "HOME_PARENT_OWNER=$(stat -c %%U ~/%[4]s)"
echo "BASELINE=$(cat /var/tmp/orbit-baseline)"
echo "ORBIT_USER=$(cat /var/tmp/orbit-user)"
if [ -n "$CLAUDE_CODE_OAUTH_TOKEN" ]; then
  echo "TOKEN_HASH=$(printf %%s "$CLAUDE_CODE_OAUTH_TOKEN" | sha256sum | cut -c1-16)"
  if grep -rqsF "$CLAUDE_CODE_OAUTH_TOKEN" /home /root /var/lib/orbit /var/tmp /tmp; then echo TOKEN_DISK=found; else echo TOKEN_DISK=none; fi
else
  echo TOKEN_HASH=
fi
echo "HTTP=$(curl -sS -o /dev/null -w '%%{http_code}' https://example.com)"
touch ~/persist
exit 0
`, filepath.Join(macHome, "Documents"), roFile, rwDir, homeRel))

	r.want(t, "PWD", e.ws)
	r.want(t, "HOME_RO", "home-ro")
	r.want(t, "HOME_PARENT_OWNER", macUser)
	r.want(t, "WHO", macUser)
	r.want(t, "MAC_HOME", "hidden")
	r.want(t, "RO", "ro-v1")
	r.want(t, "BASELINE", "baseline-ran")
	r.want(t, "ORBIT_USER", macUser)
	r.want(t, "HTTP", "200")
	r.want(t, "TOKEN_HASH", tokenHash)
	if tokenHash != "" {
		r.want(t, "TOKEN_DISK", "none")
	} else if !strings.Contains(r.stderr, "orbit auth") {
		t.Error("no token stored, but no warning naming `orbit auth`")
	}
	if got := read(t, filepath.Join(e.ws, "vm.txt")); got != "from-vm\n" {
		t.Errorf("workspace write from VM: Mac sees %q", got)
	}
	if got := read(t, filepath.Join(e.ws, "setup-ran.txt")); got != setupLine {
		t.Errorf("setup.sh ran with %q, want pwd %s as %s", got, e.ws, macUser)
	}
	if got := read(t, roFile); got != "ro-v1\n" {
		t.Errorf("ro extra edited in the VM reached the Mac: %q", got)
	}
	if got := read(t, filepath.Join(rwDir, "x")); got != "rw-from-vm\n" {
		t.Errorf("rw extra: Mac sees %q", got)
	}
	if got := read(t, filepath.Join(homeRW, "y")); got != "home-rw-from-vm\n" {
		t.Errorf("rw extra under ~, written as ~/%s/rw/y in the VM: Mac sees %q", homeRel, got)
	}

	t.Log("2. list")
	it, ok := e.find(name)
	if !ok || it.Workspace != e.ws || it.State != "running" || it.Orphaned {
		t.Errorf("list entry = %+v, %v", it, ok)
	}

	t.Log("3. stop, then attach from a subdirectory: auto-start, state kept, exit code passes through")
	e.mustOrbit(e.ws, "", "stop")
	if it, _ := e.find(name); it.State != "stopped" {
		t.Errorf("state after stop = %q", it.State)
	}
	r = e.orbit(sub, "echo \"PWD=$(pwd)\"\nif [ -f ~/persist ]; then echo PERSIST=yes; else echo PERSIST=no; fi\nexit 3\n")
	if r.code != 3 {
		t.Errorf("exit code = %d, want the shell's 3", r.code)
	}
	r.want(t, "PWD", sub)
	r.want(t, "PERSIST", "yes")

	t.Log("4. sync re-copies read-only extras")
	write(t, roFile, "ro-v2\n")
	e.mustOrbit(e.ws, "", "sync")
	r = e.mustOrbit(e.ws, fmt.Sprintf("echo \"RO=$(cat %q)\"\n", roFile))
	r.want(t, "RO", "ro-v2")

	t.Log("5. rebuild resets VM-only state and re-provisions")
	e.mustOrbit(e.ws, "", "rebuild", "--yes")
	r = e.mustOrbit(e.ws, "if [ -f ~/persist ]; then echo PERSIST=yes; else echo PERSIST=no; fi\necho \"BASELINE=$(cat /var/tmp/orbit-baseline)\"\n")
	r.want(t, "PERSIST", "no")
	r.want(t, "BASELINE", "baseline-ran")
	if got := read(t, filepath.Join(e.ws, "setup-ran.txt")); got != setupLine+setupLine {
		t.Errorf("setup.sh after rebuild: %q, want it to have run twice in all", got)
	}

	t.Log("6. destroy leaves the Mac's files alone")
	e.mustOrbit(e.ws, "", "destroy", "--yes")
	if _, exists := e.find(name); exists {
		t.Error("VM still listed after destroy")
	}
	if got := read(t, filepath.Join(e.ws, "vm.txt")); got != "from-vm\n" {
		t.Errorf("workspace file after destroy = %q", got)
	}
}
