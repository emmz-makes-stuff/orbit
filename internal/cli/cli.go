// Package cli builds orbit's command tree over injectable dependencies.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/emmz-makes-stuff/orbit/internal/config"
	"github.com/emmz-makes-stuff/orbit/internal/keychain"
	"github.com/emmz-makes-stuff/orbit/internal/orb"
)

// Deps is everything the commands touch outside the process.
type Deps struct {
	// Orb connects to OrbStack lazily, so commands that don't need it (auth) work without it.
	Orb    func() (orb.Client, error)
	Keys   keychain.Store
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// StdinIsTerminal reports whether stdin is interactive.
	StdinIsTerminal func() bool
	// ReadSecret reads a line from the terminal without echoing it.
	ReadSecret func() ([]byte, error)
	Home       string
	// ConfigDir holds config.toml and baseline.sh: ~/.config/orbit, or $ORBIT_CONFIG_DIR.
	ConfigDir string
	Getwd     func() (string, error)
	Stat      func(string) error
	Environ   func() []string
	// SSHAgentHasKeys reports whether the Mac's SSH agent holds at least one key.
	SSHAgentHasKeys func() bool
}

// NewRoot builds the orbit command tree.
func NewRoot(d *Deps) *cobra.Command {
	root := &cobra.Command{
		Use:   "orbit",
		Short: "Create or attach to an isolated OrbStack VM for the current workspace",
		Long: "Run `orbit` in a project directory to open a shell in that directory's workspace VM.\n\n" +
			"If no VM covers the directory yet, orbit creates an isolated OrbStack machine that can see\n" +
			"only this directory (writeable) and the extras allowlisted in ~/.config/orbit/config.toml,\n" +
			"provisions it with ~/.config/orbit/baseline.sh and the workspace's .orbit/setup.sh, then\n" +
			"attaches. Running it in a subdirectory reuses the parent's VM.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRoot(cmd.Context(), d)
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	root.SetIn(d.Stdin)
	root.SetOut(d.Stdout)
	root.SetErr(d.Stderr)
	root.AddCommand(newAuthCmd(d))
	root.AddCommand(lifecycleCommands(d)...)
	return root
}

// Main runs orbit with the real dependencies and returns the process exit code.
func Main(args []string) int {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "orbit:", err)
		return 1
	}
	configDir, err := config.DirFromEnv(home, os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "orbit:", err)
		return 1
	}
	stdinFd := int(os.Stdin.Fd())
	d := &Deps{
		Orb:             func() (orb.Client, error) { return orb.New() },
		Keys:            keychain.NewSecurity(),
		Stdin:           os.Stdin,
		Stdout:          os.Stdout,
		Stderr:          os.Stderr,
		StdinIsTerminal: func() bool { return term.IsTerminal(stdinFd) },
		ReadSecret:      func() ([]byte, error) { return term.ReadPassword(stdinFd) },
		Home:            home,
		ConfigDir:       configDir,
		Getwd:           os.Getwd,
		Stat:            func(p string) error { _, err := os.Stat(p); return err },
		Environ:         os.Environ,
		SSHAgentHasKeys: func() bool { return exec.Command("ssh-add", "-l").Run() == nil },
	}
	return Run(context.Background(), d, args)
}

// Run executes the command tree: an error prints and exits 1. (A shell's own exit code reaches the
// caller directly, because attaching replaces orbit with orbctl.)
func Run(ctx context.Context, d *Deps, args []string) int {
	root := NewRoot(d)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(d.Stderr, "orbit:", err)
		return 1
	}
	return 0
}
