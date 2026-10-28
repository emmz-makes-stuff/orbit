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
	if home := config.Canonical(s.d.Home); workspace.Within(home, ws) {
		return nil, fmt.Errorf("refusing to make %s a workspace: it would expose everything in your home "+
			"directory (%s), ~/.ssh included, to the VM; run orbit in a project directory instead", ws, home)
	}
	cfg, err := config.Load(s.d.ConfigDir, s.d.Home)
	if err != nil {
		return nil, err
	}
	mounts, copies, err := s.splitExtras(cfg, ws)
	if err != nil {
		return nil, fmt.Errorf("%w; nothing was created", err)
	}
	return &plan{name: workspace.Name(ws), workspace: ws, cfg: cfg, mounts: append([]string{ws}, mounts...), copies: copies}, nil
}

// splitExtras checks ws's extras and splits them into writeable mounts and read-only copies.
func (s *session) splitExtras(cfg *config.Config, ws string) (mounts, copies []string, err error) {
	extras := cfg.ExtrasFor(ws)
	for i, e := range extras {
		if ws != "" && overlaps(e.Path, ws) { // "" is a VM that lost its workspace mount
			return nil, nil, fmt.Errorf("extra %s overlaps the workspace %s; remove it from %s",
				e.Path, ws, config.File(s.d.ConfigDir))
		}
		// Replacing a read-only copy starts with rm -rf in the VM, which reaches through any mount
		// inside it to the Mac, and a copy inside a mount is written to the Mac.
		for _, o := range extras[:i] {
			if (e.Mode == config.ModeRW || o.Mode == config.ModeRW) && overlaps(e.Path, o.Path) {
				return nil, nil, fmt.Errorf("extras %s and %s overlap and one of them is writeable, which could "+
					"change files on your Mac; remove one of them from %s", o.Path, e.Path, config.File(s.d.ConfigDir))
			}
		}
		if err := s.d.Stat(e.Path); err != nil {
			return nil, nil, fmt.Errorf("extra %s doesn't exist on this Mac (declared in %s)",
				e.Path, config.File(s.d.ConfigDir))
		}
		if e.Mode == config.ModeRW {
			mounts = append(mounts, e.Path)
		} else {
			copies = append(copies, e.Path)
		}
	}
	return mounts, copies, nil
}

// overlaps reports whether either path lies within the other.
func overlaps(a, b string) bool { return workspace.Within(a, b) || workspace.Within(b, a) }

// create makes and provisions the workspace VM for ws, returning its name. A VM whose provisioning
// fails is kept for inspection.
func (s *session) create(ctx context.Context, ws string) (string, error) {
	p, err := s.planFor(ws)
	if err != nil {
		return "", err
	}
	return s.build(ctx, p)
}

// build creates and provisions a checked plan.
func (s *session) build(ctx context.Context, p *plan) (string, error) {
	s.info("creating %s for %s", p.name, p.workspace)
	err := s.client.Create(ctx, orb.CreateOptions{
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

	baseline := config.BaselinePath(s.d.ConfigDir)
	if f, err := os.Open(baseline); err == nil {
		s.info("provisioning: running %s", baseline)
		err := s.client.Run(ctx, orb.RunOptions{
			Machine: p.name, User: "root",
			// ORBIT_USER lets the baseline install per-user tools for the default user.
			Args: []string{"env", "ORBIT_USER=" + user,
				"sh", "-c", "if command -v bash >/dev/null 2>&1; then exec bash -s; else exec sh -s; fi"},
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
