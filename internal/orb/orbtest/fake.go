// Package orbtest provides an in-memory orb.Client for command tests.
package orbtest

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/emmz-makes-stuff/orbit/internal/orb"
)

var _ orb.Client = (*Fake)(nil)

// Run is one recorded Run call, with stdin read to a string.
type Run struct {
	orb.RunOptions
	Input string
}

// Shell is one recorded ExecShell call.
type Shell struct {
	Machine, Workdir string
	Env              []string
}

// Fake keeps machines in memory and records what was asked of it.
type Fake struct {
	mu       sync.Mutex
	machines map[string]*orb.Machine

	// Calls lists every operation in order, e.g. "create orbit-x-123456", "run orbit-x-123456".
	Calls  []string
	Runs   []Run
	Shells []Shell
	// Created keeps the options of every Create call.
	Created []orb.CreateOptions

	// OnRun, if set, decides each Run's outcome and can write to its Stdout/Stderr.
	OnRun func(Run) error
	// Errs makes the named operation ("list", "create", "start", "stop", "delete") fail.
	Errs map[string]error
}

// New returns a Fake holding the given machines.
func New(machines ...orb.Machine) *Fake {
	f := &Fake{machines: map[string]*orb.Machine{}, Errs: map[string]error{}}
	for i := range machines {
		m := machines[i]
		f.machines[m.Name] = &m
	}
	return f
}

// Machine returns the named machine and whether it exists.
func (f *Fake) Machine(name string) (orb.Machine, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.machines[name]
	if !ok {
		return orb.Machine{}, false
	}
	return *m, true
}

func (f *Fake) record(op, name string) error {
	f.Calls = append(f.Calls, op+" "+name)
	return f.Errs[op]
}

func (f *Fake) List(context.Context) ([]orb.Machine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("list", ""); err != nil {
		return nil, err
	}
	out := make([]orb.Machine, 0, len(f.machines))
	for _, m := range f.machines {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *Fake) Create(_ context.Context, opts orb.CreateOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("create", opts.Name); err != nil {
		return err
	}
	if _, exists := f.machines[opts.Name]; exists {
		return fmt.Errorf("machine %q already exists", opts.Name)
	}
	f.Created = append(f.Created, opts)
	m := &orb.Machine{Name: opts.Name, State: orb.StateRunning}
	for _, p := range opts.Mounts {
		m.Mounts = append(m.Mounts, orb.Mount{Source: p, Destination: p})
	}
	f.machines[opts.Name] = m
	return nil
}

func (f *Fake) setState(op, name, state string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record(op, name); err != nil {
		return err
	}
	m, ok := f.machines[name]
	if !ok {
		return fmt.Errorf("machine %q not found", name)
	}
	m.State = state
	return nil
}

func (f *Fake) Start(_ context.Context, name string) error {
	return f.setState("start", name, orb.StateRunning)
}

func (f *Fake) Stop(_ context.Context, name string) error {
	return f.setState("stop", name, orb.StateStopped)
}

func (f *Fake) Delete(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("delete", name); err != nil {
		return err
	}
	if _, ok := f.machines[name]; !ok {
		return fmt.Errorf("machine %q not found", name)
	}
	delete(f.machines, name)
	return nil
}

func (f *Fake) Run(_ context.Context, opts orb.RunOptions) error {
	r := Run{RunOptions: opts}
	if opts.Stdin != nil {
		b, err := io.ReadAll(opts.Stdin)
		if err != nil {
			return err
		}
		r.Input = string(b)
	}
	f.mu.Lock()
	f.Calls = append(f.Calls, "run "+opts.Machine)
	f.Runs = append(f.Runs, r)
	onRun := f.OnRun
	f.mu.Unlock()
	if onRun != nil {
		return onRun(r)
	}
	return nil
}

// ExecShell records the call and returns nil, standing in for a shell that exited 0.
func (f *Fake) ExecShell(machine, workdir string, env []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, "shell "+machine)
	f.Shells = append(f.Shells, Shell{Machine: machine, Workdir: workdir, Env: env})
	return nil
}
