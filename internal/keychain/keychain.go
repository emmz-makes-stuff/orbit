package keychain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
)

// Keychain item that holds the Claude token.
const (
	Service = "orbit"
	Account = "claude-oauth-token"
)

// notFound is the exit code `security` uses when no item matches.
const notFound = 44

// ErrInvalidToken means the token has characters `security -i` would misparse. Real
// `claude setup-token` tokens never do.
var ErrInvalidToken = errors.New("token must contain only letters, digits, '-' and '_'")

var validToken = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidToken reports whether a token can be stored.
func ValidToken(token string) bool { return validToken.MatchString(token) }

// Store holds the Claude token on the Mac.
type Store interface {
	// Get returns the stored token, or ok=false when there is none.
	Get(ctx context.Context) (token string, ok bool, err error)
	// Set stores the token, replacing any previous one.
	Set(ctx context.Context, token string) error
	// Clear removes the token. Clearing when none is stored succeeds.
	Clear(ctx context.Context) error
}

type runFunc func(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (exitCode int, err error)

// Security is the Store backed by macOS's `security` tool and the login Keychain.
type Security struct {
	run runFunc
}

// NewSecurity returns a Store that runs /usr/bin/security.
func NewSecurity() *Security { return &Security{run: runSecurity} }

func runSecurity(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

func (s *Security) Get(ctx context.Context) (string, bool, error) {
	var stdout, stderr bytes.Buffer
	code, err := s.run(ctx, []string{"find-generic-password", "-s", Service, "-a", Account, "-w"}, nil, &stdout, &stderr)
	switch {
	case err != nil:
		return "", false, fmt.Errorf("read Claude token from Keychain: %w", err)
	case code == notFound:
		return "", false, nil
	case code != 0:
		return "", false, fmt.Errorf("read Claude token from Keychain: %s", failure(code, &stderr))
	}
	return strings.TrimRight(stdout.String(), "\n"), true, nil
}

func (s *Security) Set(ctx context.Context, token string) error {
	if !ValidToken(token) {
		return ErrInvalidToken
	}
	// Sent as a `security -i` command on stdin, so the token is never in a process's argv.
	line := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n", Service, Account, token)
	var stderr bytes.Buffer
	code, err := s.run(ctx, []string{"-i"}, strings.NewReader(line), io.Discard, &stderr)
	if err != nil {
		return fmt.Errorf("store Claude token in Keychain: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("store Claude token in Keychain: %s", failure(code, &stderr))
	}
	return nil
}

func (s *Security) Clear(ctx context.Context) error {
	var stderr bytes.Buffer
	code, err := s.run(ctx, []string{"delete-generic-password", "-s", Service, "-a", Account}, nil, io.Discard, &stderr)
	if err != nil {
		return fmt.Errorf("remove Claude token from Keychain: %w", err)
	}
	if code != 0 && code != notFound {
		return fmt.Errorf("remove Claude token from Keychain: %s", failure(code, &stderr))
	}
	return nil
}

func failure(code int, stderr *bytes.Buffer) string {
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return msg
	}
	return fmt.Sprintf("security exited %d", code)
}
