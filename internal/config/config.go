package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Mode says how an extra reaches the VM.
type Mode string

const (
	// ModeRO copies the extra into the VM; changes there never reach the Mac. The default.
	ModeRO Mode = "ro"
	// ModeRW mounts the extra writeable.
	ModeRW Mode = "rw"
)

// Extra is an allowlisted Mac path made available to a workspace VM at the same path.
type Extra struct {
	Path string
	Mode Mode
}

// Config is the Mac-only orbit configuration. Paths are absolute and cleaned; workspace keys are
// also canonical (symlinks resolved), matching how the current directory is resolved.
type Config struct {
	Distro string
	CPUs   int
	Memory string
	Disk   string

	extras     []Extra
	workspaces map[string][]Extra
}

type rawExtra struct {
	Path string `toml:"path"`
	Mode string `toml:"mode"`
}

type rawWorkspace struct {
	Extras []rawExtra `toml:"extras"`
}

type rawConfig struct {
	Distro     string                  `toml:"distro"`
	CPUs       int                     `toml:"cpus"`
	Memory     string                  `toml:"memory"`
	Disk       string                  `toml:"disk"`
	Extras     []rawExtra              `toml:"extras"`
	Workspaces map[string]rawWorkspace `toml:"workspaces"`
}

// DefaultDistro is used when the config doesn't name one: OrbStack's default Ubuntu.
const DefaultDistro = "ubuntu"

// DirEnv overrides the config directory. It is read on the Mac, so a VM cannot influence it.
const DirEnv = "ORBIT_CONFIG_DIR"

// Dir is orbit's default config directory under the Mac home directory.
func Dir(home string) string { return filepath.Join(home, ".config", "orbit") }

// DirFromEnv returns $ORBIT_CONFIG_DIR (made absolute) when it is set, otherwise Dir(home).
func DirFromEnv(home string, getenv func(string) string) (string, error) {
	dir := getenv(DirEnv)
	if dir == "" {
		return Dir(home), nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("%s=%q: %w", DirEnv, dir, err)
	}
	return abs, nil
}

// File is the config file in a config directory.
func File(dir string) string { return filepath.Join(dir, "config.toml") }

// BaselinePath is the common baseline script every new VM runs.
func BaselinePath(dir string) string { return filepath.Join(dir, "baseline.sh") }

// Load reads config.toml from the config directory dir, expanding ~ to home. A missing file is
// an empty config. Nothing outside dir is ever read: workspace content cannot widen a VM's access.
func Load(dir, home string) (*Config, error) {
	path := File(dir)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{Distro: DefaultDistro, workspaces: map[string][]Extra{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	cfg, err := parse(string(data), home)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func parse(data, home string) (*Config, error) {
	var raw rawConfig
	md, err := toml.Decode(data, &raw)
	if err != nil {
		return nil, err
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, k := range undecoded {
			keys[i] = k.String()
		}
		return nil, fmt.Errorf("unknown keys: %s", strings.Join(keys, ", "))
	}

	cfg := &Config{
		Distro:     raw.Distro,
		CPUs:       raw.CPUs,
		Memory:     raw.Memory,
		Disk:       raw.Disk,
		workspaces: map[string][]Extra{},
	}
	if cfg.Distro == "" {
		cfg.Distro = DefaultDistro
	}
	if cfg.CPUs < 0 {
		return nil, fmt.Errorf("cpus must not be negative, got %d", cfg.CPUs)
	}

	if cfg.extras, err = parseExtras(raw.Extras, home, "extras"); err != nil {
		return nil, err
	}
	for key, ws := range raw.Workspaces {
		where := fmt.Sprintf("workspaces.%q", key)
		wsPath, err := ExpandPath(key, home)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", where, err)
		}
		wsPath = Canonical(wsPath)
		extras, err := parseExtras(ws.Extras, home, where+".extras")
		if err != nil {
			return nil, err
		}
		cfg.workspaces[wsPath] = append(cfg.workspaces[wsPath], extras...)
	}
	return cfg, nil
}

func parseExtras(raw []rawExtra, home, where string) ([]Extra, error) {
	extras := make([]Extra, 0, len(raw))
	for i, r := range raw {
		at := fmt.Sprintf("%s[%d]", where, i)
		if r.Path == "" {
			return nil, fmt.Errorf("%s: path is required", at)
		}
		p, err := ExpandPath(r.Path, home)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", at, err)
		}
		mode := Mode(r.Mode)
		switch mode {
		case "":
			mode = ModeRO
		case ModeRO, ModeRW:
		default:
			return nil, fmt.Errorf("%s: mode must be %q or %q, got %q", at, ModeRO, ModeRW, r.Mode)
		}
		// Not canonicalised: a symlinked dotfile must appear in the VM at the path it was declared
		// with, not at its target.
		extras = append(extras, Extra{Path: p, Mode: mode})
	}
	return extras, nil
}

// ExtrasFor returns the global extras plus those declared for workspace (a canonical path), sorted
// by path. Where both declare the same path, the workspace's mode wins.
func (c *Config) ExtrasFor(workspace string) []Extra {
	byPath := map[string]Mode{}
	for _, e := range c.extras {
		byPath[e.Path] = e.Mode
	}
	for _, e := range c.workspaces[workspace] {
		byPath[e.Path] = e.Mode
	}
	out := make([]Extra, 0, len(byPath))
	for p, m := range byPath {
		out = append(out, Extra{Path: p, Mode: m})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// ExpandPath expands a leading ~ to home and checks the result is an absolute path that can be
// passed to `orb create --mount SRC:DEST`, which rules out ':' and ','.
func ExpandPath(p, home string) (string, error) {
	switch {
	case p == "~":
		p = home
	case strings.HasPrefix(p, "~/"):
		p = filepath.Join(home, p[2:])
	case strings.HasPrefix(p, "~"):
		return "", fmt.Errorf("%q: only ~ and ~/ are supported", p)
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%q: path must be absolute or start with ~/", p)
	}
	if err := CheckMountable(p); err != nil {
		return "", err
	}
	return filepath.Clean(p), nil
}

// CheckMountable rejects paths that `orb create --mount` (':' separates SOURCE from DEST) or a
// hand-edited mounts list (',' separates entries) would misread.
func CheckMountable(p string) error {
	if strings.ContainsAny(p, ":,") {
		return fmt.Errorf("%q: paths containing ':' or ',' cannot be mounted", p)
	}
	return nil
}

// Canonical resolves symlinks in an absolute path. A path that doesn't exist (yet) is only cleaned.
func Canonical(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}
