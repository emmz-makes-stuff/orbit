package keychain

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

type call struct {
	args  []string
	stdin string
}

type fakeSecurity struct {
	calls  []call
	code   int
	stdout string
	stderr string
	err    error
}

func (f *fakeSecurity) run(_ context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	c := call{args: args}
	if stdin != nil {
		b, _ := io.ReadAll(stdin)
		c.stdin = string(b)
	}
	f.calls = append(f.calls, c)
	_, _ = io.WriteString(stdout, f.stdout)
	_, _ = io.WriteString(stderr, f.stderr)
	return f.code, f.err
}

func store(f *fakeSecurity) *Security { return &Security{run: f.run} }

const token = "sk-ant-oat01-abc_DEF-123"

func TestSetSendsTokenOnStdinOnly(t *testing.T) {
	f := &fakeSecurity{}
	if err := store(f).Set(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	c := f.calls[0]
	if !reflect.DeepEqual(c.args, []string{"-i"}) {
		t.Errorf("args = %q, want only -i", c.args)
	}
	for _, a := range c.args {
		if strings.Contains(a, token) {
			t.Fatalf("token appears in argv: %q", c.args)
		}
	}
	want := "add-generic-password -U -s orbit -a claude-oauth-token -w " + token + "\n"
	if c.stdin != want {
		t.Errorf("stdin = %q, want %q", c.stdin, want)
	}
}

func TestSetRejectsUnsafeTokens(t *testing.T) {
	for _, bad := range []string{"", "has space", `quo"te`, `back\slash`, "new\nline", "semi;colon"} {
		f := &fakeSecurity{}
		if err := store(f).Set(context.Background(), bad); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("Set(%q) err = %v, want ErrInvalidToken", bad, err)
		}
		if len(f.calls) != 0 {
			t.Errorf("Set(%q) ran security", bad)
		}
	}
}

func TestSetFailure(t *testing.T) {
	f := &fakeSecurity{code: 2, stderr: "add-generic-password: returned 2\n"}
	err := store(f).Set(context.Background(), token)
	if err == nil || !strings.Contains(err.Error(), "returned 2") {
		t.Errorf("err = %v", err)
	}
}

func TestGet(t *testing.T) {
	f := &fakeSecurity{stdout: token + "\n"}
	got, ok, err := store(f).Get(context.Background())
	if err != nil || !ok || got != token {
		t.Fatalf("Get = %q, %v, %v", got, ok, err)
	}
	want := []string{"find-generic-password", "-s", "orbit", "-a", "claude-oauth-token", "-w"}
	if !reflect.DeepEqual(f.calls[0].args, want) {
		t.Errorf("args = %q, want %q", f.calls[0].args, want)
	}
}

func TestGetNotFound(t *testing.T) {
	f := &fakeSecurity{code: 44, stderr: "The specified item could not be found in the keychain."}
	got, ok, err := store(f).Get(context.Background())
	if err != nil || ok || got != "" {
		t.Errorf("Get = %q, %v, %v; want none stored", got, ok, err)
	}
}

func TestGetOtherFailureIsError(t *testing.T) {
	f := &fakeSecurity{code: 51, stderr: "User interaction is not allowed."}
	if _, _, err := store(f).Get(context.Background()); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("err = %v", err)
	}
}

func TestClear(t *testing.T) {
	for _, code := range []int{0, 44} {
		f := &fakeSecurity{code: code}
		if err := store(f).Clear(context.Background()); err != nil {
			t.Errorf("Clear with exit %d: %v", code, err)
		}
		want := []string{"delete-generic-password", "-s", "orbit", "-a", "claude-oauth-token"}
		if !reflect.DeepEqual(f.calls[0].args, want) {
			t.Errorf("args = %q, want %q", f.calls[0].args, want)
		}
	}
	if err := store(&fakeSecurity{code: 1}).Clear(context.Background()); err == nil {
		t.Error("Clear with exit 1: want an error")
	}
}

func TestRunError(t *testing.T) {
	f := &fakeSecurity{err: errors.New("exec failed")}
	if _, _, err := store(f).Get(context.Background()); err == nil {
		t.Error("Get: want the run error")
	}
}
