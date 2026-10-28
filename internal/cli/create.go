package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
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
	// vmHome is where the default user's home is expected; extras under the Mac home go there.
	vmHome string
	mounts []orb.Mount // workspace first, then writeable extras
	copies []orb.Mount // read-only extras: Mac path to VM path
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
	rw, ro, err := s.splitExtras(cfg, ws)
	if err != nil {
		return nil, fmt.Errorf("%w; nothing was created", err)
	}
	vmHome := s.assumedVMHome()
	return &plan{
		name: workspace.Name(ws), workspace: ws, cfg: cfg, vmHome: vmHome,
		mounts: append([]orb.Mount{{Source: ws}}, s.place(rw, vmHome)...),
		copies: s.place(ro, vmHome),
	}, nil
}

// assumedVMHome is where OrbStack puts the default user's home, the user being named after the Mac
// user. Mounts need their destination before the VM exists, so provisioning checks it afterwards.
func (s *session) assumedVMHome() string { return "/home/" + filepath.Base(s.d.Home) }

// place maps Mac paths to where they appear in the VM: paths under the Mac home go under vmHome,
// so tools find them at ~; anything else keeps its path.
func (s *session) place(paths []string, vmHome string) []orb.Mount {
	out := make([]orb.Mount, len(paths))
	for i, p := range paths {
		out[i] = orb.Mount{Source: p, Destination: p}
		if rel, ok := underHome(p, s.d.Home); ok {
			out[i].Destination = path.Join(vmHome, rel)
		}
	}
	return out
}

// mapsHome reports whether any of ms was placed under the VM home rather than at its own path.
func mapsHome(ms []orb.Mount) bool {
	for _, m := range ms {
		if m.Target() != m.Source {
			return true
		}
	}
	return false
}

// underHome returns p relative to home, if p lies beneath it.
func underHome(p, home string) (string, bool) {
	if p == home || !workspace.Within(p, home) {
		return "", false
	}
	return strings.TrimPrefix(p, home+"/"), true
}

// homeParents lists the directories strictly between vmHome and each destination beneath it,
// outermost first, without duplicates.
func homeParents(vmHome string, dsts []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range dsts {
		var chain []string
		for dir := path.Dir(d); dir != vmHome && workspace.Within(dir, vmHome); dir = path.Dir(dir) {
			chain = append([]string{dir}, chain...)
		}
		for _, dir := range chain {
			if !seen[dir] {
				seen[dir] = true
				out = append(out, dir)
			}
		}
	}
	return out
}

// repairHomeScript fixes the home that OrbStack makes when a mount lands inside it: it creates the
// mount point as root before the user exists, so the home is root's and /etc/skel was never copied.
// find -xdev keeps the chown on the VM's own disk, out of the mounts and so off the Mac.
func repairHomeScript(user, home string) string {
	h, u := shellQuote(home), shellQuote(user)
	return fmt.Sprintf("set -e; if [ -d /etc/skel ]; then cp -Rn /etc/skel/. %s/; fi; "+
		"find %s -xdev -user root -exec chown -h %s: {} +; chmod 750 %s", h, h, u, h)
}

// ownDirsScript creates dirs and gives each to user, without recursing into them: a dir that tar
// or OrbStack would otherwise create would be root's, and the user couldn't write next to an extra.
func ownDirsScript(user string, dirs []string) string {
	if len(dirs) == 0 {
		return ""
	}
	list := quoteAll(dirs)
	return fmt.Sprintf("mkdir -p -- %s; chown -h %s -- %s; ", list, shellQuote(user), list)
}

// splitExtras checks ws's extras and splits their Mac paths into writeable and read-only ones.
func (s *session) splitExtras(cfg *config.Config, ws string) (rw, ro []string, err error) {
	extras := cfg.ExtrasFor(ws)
	for i, e := range extras {
		if ws != "" && overlaps(e.Path, ws) { // "" is a VM that lost its workspace mount
			return nil, nil, fmt.Errorf("extra %s overlaps the workspace %s; remove it from %s",
				e.Path, ws, config.File(s.d.ConfigDir))
		}
		// Replacing a read-only copy starts with rm -rf in the VM, which reaches through any mount
		// inside it to the Mac; a copy inside a mount is written to the Mac; and a symlink in one copy
		// can redirect a nested one anywhere, a mount included.
		for _, o := range extras[:i] {
			if overlaps(e.Path, o.Path) {
				return nil, nil, fmt.Errorf("extras %s and %s overlap, which could change files on your Mac; "+
					"declare only one of them in %s", o.Path, e.Path, config.File(s.d.ConfigDir))
			}
		}
		if err := s.d.Stat(e.Path); err != nil {
			return nil, nil, fmt.Errorf("extra %s doesn't exist on this Mac (declared in %s)",
				e.Path, config.File(s.d.ConfigDir))
		}
		if e.Mode == config.ModeRW {
			rw = append(rw, e.Path)
		} else {
			ro = append(ro, e.Path)
		}
	}
	return rw, ro, nil
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
	user, home, err := s.vmUser(ctx, p.name)
	if err != nil {
		return err
	}
	if home != p.vmHome && (mapsHome(p.mounts) || mapsHome(p.copies)) {
		return fmt.Errorf("%s's home directory is %s, not %s as orbit expected, so extras under your Mac "+
			"home would be in the wrong place; `orbit destroy` it and report this", p.name, home, p.vmHome)
	}
	if mapsHome(p.mounts) {
		err := s.client.Run(ctx, orb.RunOptions{Machine: p.name, User: "root", Args: []string{"sh", "-c", repairHomeScript(user, home)}})
		if err != nil {
			return fmt.Errorf("repair %s's home directory in %s: %v", user, p.name, err)
		}
	}
	if len(p.copies) > 0 {
		s.info("copying %d read-only extra(s)", len(p.copies))
		skipped, err := copyExtras(ctx, s.client, p.name, user, home, p.copies)
		if err != nil {
			return err
		}
		s.warnSkipped(skipped)
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
	if err := s.d.Stat(setup); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("provisioning failed: can't check for %s: %w", setupScript, err)
	} else if err == nil {
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

// vmUserScript prints the default user's name and home directory, one per line.
const vmUserScript = `u=$(id -un) && echo "$u" && getent passwd "$u" | cut -d: -f6`

// vmUser asks the VM for its default user and that user's home, for placing and owning extras.
func (s *session) vmUser(ctx context.Context, machine string) (user, home string, err error) {
	var out strings.Builder
	if err := s.client.Run(ctx, orb.RunOptions{Machine: machine, Args: []string{"sh", "-c", vmUserScript}, Stdout: &out}); err != nil {
		return "", "", fmt.Errorf("find the default user in %s: %v", machine, err)
	}
	fields := strings.Fields(out.String())
	if len(fields) != 2 {
		return "", "", fmt.Errorf("find the default user in %s: expected a name and a home directory, got %q",
			machine, strings.TrimSpace(out.String()))
	}
	return fields[0], fields[1], nil
}
