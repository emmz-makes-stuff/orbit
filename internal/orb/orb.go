package orb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// ErrNotInstalled means orbctl isn't on the PATH.
var ErrNotInstalled = errors.New("OrbStack is required but `orbctl` is not on the PATH: install it with `brew install orbstack`, then open OrbStack once")

// Mount is one host directory shared into a machine.
type Mount struct {
	Source      string `json:"source"`
	Destination string `json:"destination,omitempty"`
}

// Target is where the mount appears in the machine.
func (m Mount) Target() string {
	if m.Destination == "" {
		return m.Source
	}
	return m.Destination
}

// Machine is an OrbStack machine as `orbctl list -f json` reports it.
type Machine struct {
	Name  string
	State string
	// Mounts are in the order they were given at creation.
	Mounts []Mount
}

// State values reported by orbctl.
const (
	StateRunning = "running"
	StateStopped = "stopped"
)

// CreateOptions describes an isolated machine to create.
type CreateOptions struct {
	Name   string
	Distro string
	// Mounts are shared writeable, each at its Destination (its Source when empty). Order is kept.
	Mounts []Mount
	CPUs   int
	Memory string
	Disk   string
}

// RunOptions describes a command to run inside a machine.
type RunOptions struct {
	Machine string
	User    string // "" means the default user
	Workdir string // "" means orbctl's default
	Args    []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

// ExitError carries a non-zero exit code from a command run inside a machine.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return "exit status " + strconv.Itoa(e.Code) }

// Client is everything orbit asks of OrbStack.
type Client interface {
	List(ctx context.Context) ([]Machine, error)
	Create(ctx context.Context, opts CreateOptions) error
	Start(ctx context.Context, name string) error
	Stop(ctx context.Context, name string) error
	Delete(ctx context.Context, name string) error
	// Run runs a command in a machine; a non-zero exit is an *ExitError.
	Run(ctx context.Context, opts RunOptions) error
	// ExecShell replaces the current process with a login shell in the machine. On success it
	// does not return.
	ExecShell(machine, workdir string, env []string) error
}

type command struct {
	args   []string
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

type runFunc func(ctx context.Context, path string, c command) error
type execFunc func(path string, argv, env []string) error

// CLI is the Client backed by the orbctl binary.
type CLI struct {
	path string
	run  runFunc
	exec execFunc
}

// New finds orbctl on the PATH.
func New() (*CLI, error) {
	path, err := exec.LookPath("orbctl")
	if err != nil {
		return nil, ErrNotInstalled
	}
	return &CLI{path: path, run: runProcess, exec: syscall.Exec}, nil
}

func runProcess(ctx context.Context, path string, c command) error {
	cmd := exec.CommandContext(ctx, path, c.args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = c.stdin, c.stdout, c.stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return &ExitError{Code: exitErr.ExitCode()}
	}
	return err
}

// output runs an orbctl subcommand, returning stdout and folding stderr into any error.
func (c *CLI) output(ctx context.Context, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	err := c.run(ctx, c.path, command{args: args, stdout: &stdout, stderr: &stderr})
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg != "" {
			return nil, fmt.Errorf("orbctl %s: %s", args[0], msg)
		}
		// Not wrapped: an *ExitError means a command in a machine failed, and orbctl's own failure
		// isn't that.
		var exit *ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("orbctl %s: exited %d without saying why", args[0], exit.Code)
		}
		return nil, fmt.Errorf("orbctl %s: %w", args[0], err)
	}
	return stdout.Bytes(), nil
}

type listEntry struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Config struct {
		Mounts []Mount `json:"mounts"`
	} `json:"config"`
}

func (c *CLI) List(ctx context.Context) ([]Machine, error) {
	out, err := c.output(ctx, "list", "-f", "json")
	if err != nil {
		return nil, err
	}
	return parseList(out)
}

func parseList(data []byte) ([]Machine, error) {
	var entries []listEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("orbctl list: unexpected output: %w", err)
	}
	machines := make([]Machine, len(entries))
	for i, e := range entries {
		machines[i] = Machine{Name: e.Name, State: e.State, Mounts: e.Config.Mounts}
	}
	return machines, nil
}

func (c *CLI) Create(ctx context.Context, opts CreateOptions) error {
	_, err := c.output(ctx, createArgs(opts)...)
	return err
}

func createArgs(opts CreateOptions) []string {
	args := []string{"create", "--isolated", "--forward-ssh-agent"}
	for _, m := range opts.Mounts {
		// Without an explicit DEST, OrbStack mounts under /mnt/mac/ instead of at the same path.
		args = append(args, "--mount", m.Source+":"+m.Target())
	}
	if opts.CPUs > 0 {
		args = append(args, "--cpus", strconv.Itoa(opts.CPUs))
	}
	if opts.Memory != "" {
		args = append(args, "--memory", opts.Memory)
	}
	if opts.Disk != "" {
		args = append(args, "--disk", opts.Disk)
	}
	return append(args, opts.Distro, opts.Name)
}

func (c *CLI) Start(ctx context.Context, name string) error {
	_, err := c.output(ctx, "start", name)
	return err
}

func (c *CLI) Stop(ctx context.Context, name string) error {
	_, err := c.output(ctx, "stop", name)
	return err
}

func (c *CLI) Delete(ctx context.Context, name string) error {
	_, err := c.output(ctx, "delete", "-f", name)
	return err
}

func (c *CLI) Run(ctx context.Context, opts RunOptions) error {
	return c.run(ctx, c.path, command{
		args:   append(runPrefix(opts.Machine, opts.User, opts.Workdir), opts.Args...),
		stdin:  opts.Stdin,
		stdout: opts.Stdout,
		stderr: opts.Stderr,
	})
}

func runPrefix(machine, user, workdir string) []string {
	args := []string{"run", "-m", machine}
	if user != "" {
		args = append(args, "-u", user)
	}
	if workdir != "" {
		args = append(args, "-w", workdir)
	}
	return args
}

func (c *CLI) ExecShell(machine, workdir string, env []string) error {
	// With no command, `orbctl run` starts the default user's login shell.
	argv := append([]string{"orbctl"}, runPrefix(machine, "", workdir)...)
	return c.exec(c.path, argv, env)
}

// ForwardEnv returns env with vars set and named in ORBENV, which is how OrbStack decides which
// Mac environment variables reach a machine. Existing ORBENV entries are kept. Values stay in the
// process environment: they are never put on a command line.
func ForwardEnv(env []string, vars map[string]string) []string {
	if len(vars) == 0 {
		return env
	}
	names := make([]string, 0, len(vars))
	for k := range vars {
		names = append(names, k)
	}
	sort.Strings(names)

	var orbenv []string
	out := make([]string, 0, len(env)+len(vars)+1)
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if _, replaced := vars[k]; replaced {
			continue
		}
		if k == "ORBENV" {
			orbenv = strings.FieldsFunc(v, func(r rune) bool { return r == ':' })
			continue
		}
		out = append(out, kv)
	}
	for _, k := range names {
		out = append(out, k+"="+vars[k])
		if !slices.Contains(orbenv, k) {
			orbenv = append(orbenv, k)
		}
	}
	return append(out, "ORBENV="+strings.Join(orbenv, ":"))
}
