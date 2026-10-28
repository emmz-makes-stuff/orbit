package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/emmz-makes-stuff/orbit/internal/keychain"
)

func newAuthCmd(d *Deps) *cobra.Command {
	var clearToken bool
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Store the Claude token (from `claude setup-token`) in the macOS Keychain",
		Long: "Store the Claude token from `claude setup-token` in the macOS Keychain. Every shell orbit\n" +
			"opens gets it as CLAUDE_CODE_OAUTH_TOKEN; it is never written to a VM.\n\n" +
			"The token is read from stdin when piped, otherwise from a hidden prompt.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if clearToken {
				if err := d.Keys.Clear(ctx); err != nil {
					return err
				}
				fmt.Fprintln(d.Stdout, "Claude token removed from the Keychain.")
				return nil
			}
			token, err := readToken(d)
			if err != nil {
				return err
			}
			if err := d.Keys.Set(ctx, token); err != nil {
				if errors.Is(err, keychain.ErrInvalidToken) {
					return fmt.Errorf("that doesn't look like a `claude setup-token` token: %w", err)
				}
				return err
			}
			fmt.Fprintln(d.Stdout, "Claude token stored in the Keychain.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&clearToken, "clear", false, "remove the stored token")
	return cmd
}

func readToken(d *Deps) (string, error) {
	var raw []byte
	var err error
	if d.StdinIsTerminal() {
		fmt.Fprint(d.Stderr, "Paste the token from `claude setup-token` (input hidden): ")
		raw, err = d.ReadSecret()
		fmt.Fprintln(d.Stderr)
	} else {
		raw, err = io.ReadAll(d.Stdin)
	}
	if err != nil {
		return "", fmt.Errorf("read token: %w", err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", errors.New("no token given: nothing stored")
	}
	return token, nil
}
