// Package cli builds orbit's command tree over injectable dependencies.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

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
	Getwd      func() (string, error)
	Stat       func(string) error
}

// NewRoot builds the orbit command tree.
func NewRoot(d *Deps) *cobra.Command {
	root := &cobra.Command{
		Use:           "orbit",
		Short:         "Create or attach to an isolated OrbStack VM for the current workspace",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.SetIn(d.Stdin)
	root.SetOut(d.Stdout)
	root.SetErr(d.Stderr)
	root.AddCommand(newAuthCmd(d))
	return root
}

// Main runs orbit with the real dependencies and returns the process exit code.
func Main(args []string) int {
	home, err := os.UserHomeDir()
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
		Getwd:           os.Getwd,
		Stat:            func(p string) error { _, err := os.Stat(p); return err },
	}
	return Run(context.Background(), d, args)
}

// Run executes the command tree and maps errors to exit codes: a command's own *orb.ExitError
// passes its code through, anything else prints and exits 1.
func Run(ctx context.Context, d *Deps, args []string) int {
	root := NewRoot(d)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	var exit *orb.ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	fmt.Fprintln(d.Stderr, "orbit:", err)
	return 1
}
