package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/emmz-makes-stuff/orbit/internal/config"
	"github.com/emmz-makes-stuff/orbit/internal/orb"
	"github.com/emmz-makes-stuff/orbit/internal/workspace"
)

// tokenEnv is how Claude Code picks up a long-lived token.
const tokenEnv = "CLAUDE_CODE_OAUTH_TOKEN"

// provisionedMarker exists in a VM once its baseline and workspace setup have succeeded.
const provisionedMarker = "/var/lib/orbit/provisioned"

// session is one orbit invocation's view of OrbStack.
type session struct {
	d      *Deps
	client orb.Client
}

func connect(d *Deps) (*session, error) {
	client, err := d.Orb()
	if err != nil {
		return nil, err
	}
	return &session{d: d, client: client}, nil
}

func (s *session) warn(format string, args ...any) {
	fmt.Fprintf(s.d.Stderr, "orbit: warning: "+format+"\n", args...)
}

func (s *session) info(format string, args ...any) {
	fmt.Fprintf(s.d.Stderr, "orbit: "+format+"\n", args...)
}

// machines lists OrbStack machines as resolution sees them.
func (s *session) machines(ctx context.Context) ([]workspace.Machine, error) {
	list, err := s.client.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not reach OrbStack: %w", err)
	}
	out := make([]workspace.Machine, len(list))
	for i, m := range list {
		out[i] = workspace.Machine{Name: m.Name, State: m.State}
		if len(m.Mounts) > 0 {
			out[i].Workspace = m.Mounts[0].Source
		}
	}
	return out, nil
}

// cwd is the current directory with symlinks resolved, as workspaces are keyed.
func (s *session) cwd() (string, error) {
	dir, err := s.d.Getwd()
	if err != nil {
		return "", fmt.Errorf("current directory: %w", err)
	}
	return config.Canonical(dir), nil
}

// runRoot is bare `orbit`: create the current directory's workspace VM if there is none, then
// open a shell in the current directory.
func runRoot(ctx context.Context, d *Deps) error {
	s, err := connect(d)
	if err != nil {
		return err
	}
	dir, err := s.cwd()
	if err != nil {
		return err
	}
	machines, err := s.machines(ctx)
	if err != nil {
		return err
	}

	res := workspace.Resolve(dir, machines)
	switch res.Kind {
	case workspace.Overlap:
		return overlapError(dir, res.Overlaps)
	case workspace.Create:
		s.checkSSHAgent()
		name, err := s.create(ctx, dir)
		if err != nil {
			return err
		}
		return s.attach(ctx, workspace.Machine{Name: name, State: orb.StateRunning, Workspace: dir}, dir, true)
	default:
		s.checkSSHAgent()
		return s.attach(ctx, res.Machine, dir, false)
	}
}

func overlapError(dir string, overlaps []workspace.Machine) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s contains existing workspaces, so it can't be a workspace itself:\n", dir)
	for _, m := range overlaps {
		fmt.Fprintf(&b, "  %s  (%s)\n", m.Workspace, m.Name)
	}
	b.WriteString("Run orbit from inside one of them, or remove them first with `orbit destroy <name>`.")
	return errors.New(b.String())
}

// attach starts the VM if needed and replaces orbit with a login shell in dir.
func (s *session) attach(ctx context.Context, m workspace.Machine, dir string, fresh bool) error {
	if m.State != orb.StateRunning {
		s.info("starting %s", m.Name)
		if err := s.client.Start(ctx, m.Name); err != nil {
			return fmt.Errorf("start %s: %w", m.Name, err)
		}
	}
	if !fresh {
		var exit *orb.ExitError
		switch err := s.client.Run(ctx, orb.RunOptions{Machine: m.Name, Args: []string{"test", "-f", provisionedMarker}}); {
		case errors.As(err, &exit) && exit.Code == 1: // test's "no such file"; anything else is orbctl's
			s.warn("%s never finished provisioning; `orbit rebuild` recreates it from scratch", m.Name)
		case err != nil:
			s.warn("could not check whether %s finished provisioning: %v", m.Name, err)
		}
	}

	vars := map[string]string{}
	token, ok, err := s.d.Keys.Get(ctx)
	switch {
	case err != nil:
		s.warn("could not read the Claude token (%v); Claude will ask you to log in", err)
	case !ok:
		s.warn("no Claude token stored; run `claude setup-token`, then `orbit auth`")
	default:
		vars[tokenEnv] = token
	}
	return s.client.ExecShell(m.Name, dir, orb.ForwardEnv(s.d.Environ(), vars))
}

// checkSSHAgent warns when git push from the VM would fail for want of a loaded key.
func (s *session) checkSSHAgent() {
	if !s.d.SSHAgentHasKeys() {
		s.warn("your Mac's SSH agent has no keys, so git push from the VM will fail; " +
			"load one with `ssh-add --apple-use-keychain ~/.ssh/<key>`")
	}
}
