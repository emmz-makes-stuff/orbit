package orb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

// fakeRunner records orbctl invocations and answers with canned output.
type fakeRunner struct {
	calls  [][]string
	stdout string
	stderr string
	err    error
	stdin  string
}

func (f *fakeRunner) run(_ context.Context, _ string, c command) error {
	f.calls = append(f.calls, c.args)
	if c.stdin != nil {
		b, _ := io.ReadAll(c.stdin)
		f.stdin = string(b)
	}
	if c.stdout != nil {
		_, _ = io.WriteString(c.stdout, f.stdout)
	}
	if c.stderr != nil {
		_, _ = io.WriteString(c.stderr, f.stderr)
	}
	return f.err
}

func newFake(f *fakeRunner) *CLI { return &CLI{path: "/opt/orbctl", run: f.run} }

func TestParseList(t *testing.T) {
	// Shape observed from `orbctl list -f json` on OrbStack 2.2.4 (see devlog spike notes).
	data := []byte(`[
	  {"name":"ubuntu","state":"stopped","image":{"distro":"ubuntu"},
	   "config":{"isolated":false,"forward_ssh_agent":true}},
	  {"name":"orbit-emmz-abc123","state":"running",
	   "config":{"isolated":true,"mounts":[
	     {"source":"/Users/e/github/emmz","destination":"/Users/e/github/emmz"},
	     {"source":"/Users/e/notes"}]}}
	]`)
	got, err := parseList(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []Machine{
		{Name: "ubuntu", State: StateStopped},
		{Name: "orbit-emmz-abc123", State: StateRunning, Mounts: []Mount{
			{Source: "/Users/e/github/emmz", Destination: "/Users/e/github/emmz"},
			{Source: "/Users/e/notes"},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseList =\n  %+v\nwant\n  %+v", got, want)
	}
}

func TestParseListRejectsGarbage(t *testing.T) {
	if _, err := parseList([]byte("not json")); err == nil {
		t.Error("want an error for non-JSON output")
	}
}

func TestCreateArgs(t *testing.T) {
	f := &fakeRunner{}
	err := newFake(f).Create(context.Background(), CreateOptions{
		Name:   "orbit-emmz-abc123",
		Distro: "ubuntu:24.04",
		Mounts: []string{"/Users/e/github/emmz", "/Users/e/notes"},
		CPUs:   4,
		Memory: "8G",
		Disk:   "64G",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"create", "--isolated", "--forward-ssh-agent",
		"--mount", "/Users/e/github/emmz:/Users/e/github/emmz",
		"--mount", "/Users/e/notes:/Users/e/notes",
		"--cpus", "4", "--memory", "8G", "--disk", "64G",
		"ubuntu:24.04", "orbit-emmz-abc123"}
	if !reflect.DeepEqual(f.calls[0], want) {
		t.Errorf("args =\n  %q\nwant\n  %q", f.calls[0], want)
	}
}

func TestCreateArgsOmitUnsetLimits(t *testing.T) {
	got := createArgs(CreateOptions{Name: "n", Distro: "ubuntu", Mounts: []string{"/w"}})
	want := []string{"create", "--isolated", "--forward-ssh-agent", "--mount", "/w:/w", "ubuntu", "n"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args = %q, want %q", got, want)
	}
}

func TestLifecycleArgs(t *testing.T) {
	f := &fakeRunner{}
	c := newFake(f)
	ctx := context.Background()
	for _, step := range []func() error{
		func() error { return c.Start(ctx, "m") },
		func() error { return c.Stop(ctx, "m") },
		func() error { return c.Delete(ctx, "m") },
	} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
	want := [][]string{{"start", "m"}, {"stop", "m"}, {"delete", "-f", "m"}}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %q, want %q", f.calls, want)
	}
}

func TestErrorsCarryStderr(t *testing.T) {
	f := &fakeRunner{stderr: "machine not found\n", err: &ExitError{Code: 1}}
	err := newFake(f).Start(context.Background(), "nope")
	if err == nil || err.Error() != "orbctl start: machine not found" {
		t.Errorf("err = %v", err)
	}
}

func TestErrorsWithoutOutputWrapCause(t *testing.T) {
	cause := errors.New("boom")
	err := newFake(&fakeRunner{err: cause}).Stop(context.Background(), "m")
	if !errors.Is(err, cause) {
		t.Errorf("err = %v, want it to wrap %v", err, cause)
	}
}

func TestRun(t *testing.T) {
	f := &fakeRunner{stdout: "hello\n", err: &ExitError{Code: 3}}
	var out bytes.Buffer
	err := newFake(f).Run(context.Background(), RunOptions{
		Machine: "m", User: "root", Workdir: "/w",
		Args:  []string{"bash", "-s"},
		Stdin: strings.NewReader("echo hi"), Stdout: &out,
	})
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != 3 {
		t.Fatalf("err = %v, want exit 3", err)
	}
	want := []string{"run", "-m", "m", "-u", "root", "-w", "/w", "bash", "-s"}
	if !reflect.DeepEqual(f.calls[0], want) {
		t.Errorf("args = %q, want %q", f.calls[0], want)
	}
	if f.stdin != "echo hi" || out.String() != "hello\n" {
		t.Errorf("stdin = %q, stdout = %q", f.stdin, out.String())
	}
}

func TestExecShell(t *testing.T) {
	var gotPath string
	var gotArgv, gotEnv []string
	c := &CLI{path: "/opt/orbctl", exec: func(path string, argv, env []string) error {
		gotPath, gotArgv, gotEnv = path, argv, env
		return fmt.Errorf("exec stubbed")
	}}
	env := []string{"A=1"}
	if err := c.ExecShell("m", "/w/sub", env); err == nil {
		t.Fatal("want the stubbed exec error back")
	}
	if gotPath != "/opt/orbctl" {
		t.Errorf("path = %q", gotPath)
	}
	if want := []string{"orbctl", "run", "-m", "m", "-w", "/w/sub"}; !reflect.DeepEqual(gotArgv, want) {
		t.Errorf("argv = %q, want %q", gotArgv, want)
	}
	if !reflect.DeepEqual(gotEnv, env) {
		t.Errorf("env = %q, want %q", gotEnv, env)
	}
}

func TestForwardEnv(t *testing.T) {
	cases := []struct {
		name string
		env  []string
		vars map[string]string
		want []string
	}{
		{"adds var and ORBENV",
			[]string{"HOME=/h"}, map[string]string{"TOK": "s3cret"},
			[]string{"HOME=/h", "TOK=s3cret", "ORBENV=TOK"}},
		{"keeps existing ORBENV entries",
			[]string{"ORBENV=EDITOR:AWS_PROFILE", "HOME=/h"}, map[string]string{"TOK": "x"},
			[]string{"HOME=/h", "TOK=x", "ORBENV=EDITOR:AWS_PROFILE:TOK"}},
		{"replaces a stale value, no duplicate ORBENV name",
			[]string{"TOK=old", "ORBENV=TOK"}, map[string]string{"TOK": "new"},
			[]string{"TOK=new", "ORBENV=TOK"}},
		{"nothing to forward leaves env alone",
			[]string{"ORBENV=EDITOR"}, nil,
			[]string{"ORBENV=EDITOR"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ForwardEnv(tc.env, tc.vars); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ForwardEnv = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewWithoutOrbctl(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := New(); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("New() error = %v, want ErrNotInstalled", err)
	}
	if !strings.Contains(ErrNotInstalled.Error(), "brew install orbstack") {
		t.Errorf("ErrNotInstalled doesn't say how to fix it: %q", ErrNotInstalled)
	}
}

func TestExitError(t *testing.T) {
	if got := (&ExitError{Code: 7}).Error(); got != "exit status 7" {
		t.Errorf("Error() = %q", got)
	}
}
