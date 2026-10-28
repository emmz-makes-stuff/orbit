package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/emmz-makes-stuff/orbit/internal/config"
	"github.com/emmz-makes-stuff/orbit/internal/orb"
	"github.com/emmz-makes-stuff/orbit/internal/workspace"
)

// setupScript is the workspace's own provisioning script, relative to the workspace.
const setupScript = ".orbit/setup.sh"

// plan is a workspace VM to create, checked before anything is created.
type plan struct {
	name      string
	workspace string
	cfg       *config.Config
	mounts    []string // workspace first, then writeable extras
	copies    []string // read-only extras
}

func (s *session) planFor(ws string) (*plan, error) {
	if err := config.CheckMountable(ws); err != nil {
		return nil, fmt.Errorf("can't use this directory as a workspace: %w", err)
	}
	cfg, err := config.Load(s.d.Home)
	if err != nil {
		return nil, err
	}
	p := &plan{name: workspace.Name(ws), workspace: ws, cfg: cfg, mounts: []string{ws}}
	for _, e := range cfg.ExtrasFor(ws) {
		if workspace.Within(e.Path, ws) || workspace.Within(ws, e.Path) {
			return nil, fmt.Errorf("extra %s overlaps the workspace %s; remove it from %s",
				e.Path, ws, filepath.Join(config.Dir(s.d.Home), "config.toml"))
		}
		if err := s.d.Stat(e.Path); err != nil {
			return nil, fmt.Errorf("extra %s doesn't exist on this Mac (declared in %s); nothing was created",
				e.Path, filepath.Join(config.Dir(s.d.Home), "config.toml"))
		}
		if e.Mode == config.ModeRW {
			p.mounts = append(p.mounts, e.Path)
		} else {
			p.copies = append(p.copies, e.Path)
		}
	}
	return p, nil
}

// create makes and provisions the workspace VM for ws, returning its name. A VM whose provisioning
// fails is kept for inspection.
func (s *session) create(ctx context.Context, ws string) (string, error) {
	p, err := s.planFor(ws)
	if err != nil {
		return "", err
	}
	s.info("creating %s for %s", p.name, ws)
	err = s.client.Create(ctx, orb.CreateOptions{
		Name:   p.name,
		Distro: p.cfg.Distro,
		Mounts: p.mounts,
		CPUs:   p.cfg.CPUs,
		Memory: p.cfg.Memory,
		Disk:   p.cfg.Disk,
	})
	if err != nil {
		return "", fmt.Errorf("create %s: %w", p.name, err)
	}
	if err := s.provision(ctx, p); err != nil {
		return "", fmt.Errorf("%w\nThe VM %s was kept so you can inspect it; `orbit rebuild` starts it over, `orbit destroy` removes it",
			err, p.name)
	}
	return p.name, nil
}

// provision copies read-only extras, runs the baseline as root and then the workspace setup as
// the default user, and finally marks the VM provisioned.
func (s *session) provision(ctx context.Context, p *plan) error {
	user, err := s.defaultUser(ctx, p.name)
	if err != nil {
		return err
	}
	if len(p.copies) > 0 {
		s.info("copying %d read-only extra(s)", len(p.copies))
		if err := copyExtras(ctx, s.client, p.name, user, p.copies); err != nil {
			return err
		}
	}

	baseline := config.BaselinePath(s.d.Home)
	if f, err := os.Open(baseline); err == nil {
		s.info("provisioning: running %s", baseline)
		err := s.client.Run(ctx, orb.RunOptions{
			Machine: p.name, User: "root",
			Args:  []string{"sh", "-c", "if command -v bash >/dev/null 2>&1; then exec bash -s; else exec sh -s; fi"},
			Stdin: f, Stdout: s.d.Stdout, Stderr: s.d.Stderr,
		})
		f.Close()
		if err != nil {
			return scriptError(baseline, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("read %s: %w", baseline, err)
	}

	setup := filepath.Join(p.workspace, setupScript)
	if s.d.Stat(setup) == nil {
		s.info("provisioning: running %s", setupScript)
		// Run in place from the mounted workspace, so the script can find files next to it.
		err := s.client.Run(ctx, orb.RunOptions{
			Machine: p.name, Workdir: p.workspace,
			Args:   []string{"sh", "-c", `if command -v bash >/dev/null 2>&1; then exec bash "$0"; else exec sh "$0"; fi`, setup},
			Stdout: s.d.Stdout, Stderr: s.d.Stderr,
		})
		if err != nil {
			return scriptError(setupScript, err)
		}
	}

	err = s.client.Run(ctx, orb.RunOptions{
		Machine: p.name, User: "root",
		Args: []string{"sh", "-c", "mkdir -p " + filepath.Dir(provisionedMarker) + " && touch " + provisionedMarker},
	})
	if err != nil {
		return fmt.Errorf("mark %s provisioned: %v", p.name, err)
	}
	return nil
}

// scriptError reports a failed provisioning script without passing its exit code through as
// orbit's own.
func scriptError(script string, err error) error {
	var exit *orb.ExitError
	if errors.As(err, &exit) {
		return fmt.Errorf("provisioning failed: %s exited %d (its output is above)", script, exit.Code)
	}
	return fmt.Errorf("provisioning failed: %s: %v", script, err)
}

// defaultUser asks the VM who its default user is, for ownership of copied extras.
func (s *session) defaultUser(ctx context.Context, machine string) (string, error) {
	var out strings.Builder
	err := s.client.Run(ctx, orb.RunOptions{Machine: machine, Args: []string{"id", "-un"}, Stdout: &out})
	user := strings.TrimSpace(out.String())
	if err != nil || user == "" {
		return "", fmt.Errorf("find the default user in %s: %v", machine, err)
	}
	return user, nil
}
