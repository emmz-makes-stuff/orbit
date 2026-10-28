package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/emmz-makes-stuff/orbit/internal/config"
	"github.com/emmz-makes-stuff/orbit/internal/keychain"
	"github.com/emmz-makes-stuff/orbit/internal/orb"
	"github.com/emmz-makes-stuff/orbit/internal/orb/orbtest"
)

// memKeys is an in-memory keychain.Store.
type memKeys struct {
	token  string
	stored bool
	err    error
}

func (m *memKeys) Get(context.Context) (string, bool, error) { return m.token, m.stored, m.err }

func (m *memKeys) Set(_ context.Context, token string) error {
	if !keychain.ValidToken(token) {
		return keychain.ErrInvalidToken
	}
	m.token, m.stored = token, true
	return m.err
}

func (m *memKeys) Clear(context.Context) error {
	m.token, m.stored = "", false
	return m.err
}

// harness wires Deps to fakes and captures output.
type harness struct {
	deps   *Deps
	orb    *orbtest.Fake
	keys   *memKeys
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{orb: orbtest.New(), keys: &memKeys{}}
	h.deps = &Deps{
		Orb:             func() (orb.Client, error) { return h.orb, nil },
		Keys:            h.keys,
		Stdin:           strings.NewReader(""),
		Stdout:          &h.stdout,
		Stderr:          &h.stderr,
		StdinIsTerminal: func() bool { return false },
		ReadSecret:      func() ([]byte, error) { return nil, errors.New("no terminal in tests") },
		Home:            t.TempDir(),
		Getwd:           func() (string, error) { return "/", nil },
		Stat:            func(p string) error { _, err := os.Stat(p); return err },
		Environ:         func() []string { return []string{"HOME=/Users/e"} },
		SSHAgentHasKeys: func() bool { return true },
	}
	h.deps.ConfigDir = config.Dir(h.deps.Home)
	return h
}

func (h *harness) run(args ...string) int {
	return Run(context.Background(), h.deps, args)
}

const token = "sk-ant-oat01-abc_DEF-123"

func TestAuthPipedToken(t *testing.T) {
	h := newHarness(t)
	h.deps.Stdin = strings.NewReader(token + "\n")
	if code := h.run("auth"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, h.stderr.String())
	}
	if !h.keys.stored || h.keys.token != token {
		t.Errorf("stored = %v %q", h.keys.stored, h.keys.token)
	}
	if out := h.stdout.String() + h.stderr.String(); strings.Contains(out, token) {
		t.Errorf("token was printed: %q", out)
	}
}

func TestAuthHiddenPrompt(t *testing.T) {
	h := newHarness(t)
	h.deps.StdinIsTerminal = func() bool { return true }
	h.deps.ReadSecret = func() ([]byte, error) { return []byte(token), nil }
	if code := h.run("auth"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, h.stderr.String())
	}
	if h.keys.token != token {
		t.Errorf("stored %q", h.keys.token)
	}
	if !strings.Contains(h.stderr.String(), "input hidden") {
		t.Errorf("no prompt shown: %q", h.stderr.String())
	}
	if strings.Contains(h.stdout.String()+h.stderr.String(), token) {
		t.Error("token was echoed")
	}
}

func TestAuthReplacesExistingToken(t *testing.T) {
	h := newHarness(t)
	h.keys.token, h.keys.stored = "old-token", true
	h.deps.Stdin = strings.NewReader("new-token")
	if code := h.run("auth"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if h.keys.token != "new-token" {
		t.Errorf("token = %q, want new-token", h.keys.token)
	}
}

func TestAuthEmptyInput(t *testing.T) {
	h := newHarness(t)
	h.deps.Stdin = strings.NewReader("  \n")
	if code := h.run("auth"); code == 0 {
		t.Fatal("want non-zero exit for empty input")
	}
	if h.keys.stored {
		t.Error("something was stored")
	}
	if !strings.Contains(h.stderr.String(), "nothing stored") {
		t.Errorf("stderr = %q", h.stderr.String())
	}
}

func TestAuthInvalidToken(t *testing.T) {
	h := newHarness(t)
	h.deps.Stdin = strings.NewReader("not a token")
	if code := h.run("auth"); code == 0 || h.keys.stored {
		t.Fatalf("exit %d, stored %v; want rejection", code, h.keys.stored)
	}
	if !strings.Contains(h.stderr.String(), "claude setup-token") {
		t.Errorf("stderr = %q", h.stderr.String())
	}
}

func TestAuthClear(t *testing.T) {
	h := newHarness(t)
	h.keys.token, h.keys.stored = token, true
	if code := h.run("auth", "--clear"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if h.keys.stored {
		t.Error("token still stored")
	}
	// Clearing again, with nothing stored, still succeeds.
	if code := h.run("auth", "--clear"); code != 0 {
		t.Fatalf("second clear exit %d", code)
	}
}

func TestAuthDoesNotNeedOrbStack(t *testing.T) {
	h := newHarness(t)
	h.deps.Orb = func() (orb.Client, error) { return nil, orb.ErrNotInstalled }
	h.deps.Stdin = strings.NewReader(token)
	if code := h.run("auth"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, h.stderr.String())
	}
}

func TestRunPrintsEveryError(t *testing.T) {
	h := newHarness(t)
	h.keys.err = &orb.ExitError{Code: 5}
	if code := h.run("auth", "--clear"); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.HasPrefix(h.stderr.String(), "orbit: ") {
		t.Errorf("stderr = %q, want the error printed", h.stderr.String())
	}
}

func TestSilentOrbctlFailurePrints(t *testing.T) {
	h := newHarness(t)
	h.orb.Errs["list"] = fmt.Errorf("orbctl list: exited 1 without saying why")
	if code := h.run("list"); code != 1 || !strings.Contains(h.stderr.String(), "exited 1 without saying why") {
		t.Errorf("exit %d, stderr %q", code, h.stderr.String())
	}
}
