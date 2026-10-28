package orbtest

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/emmz-makes-stuff/orbit/internal/orb"
)

func TestFakeLifecycle(t *testing.T) {
	ctx := context.Background()
	f := New(orb.Machine{Name: "ubuntu", State: orb.StateStopped})

	if err := f.Create(ctx, orb.CreateOptions{Name: "orbit-w-123456", Mounts: []string{"/w"}}); err != nil {
		t.Fatal(err)
	}
	m, ok := f.Machine("orbit-w-123456")
	if !ok || m.State != orb.StateRunning || m.Mounts[0].Source != "/w" {
		t.Fatalf("created machine = %+v, %v", m, ok)
	}
	if err := f.Stop(ctx, "orbit-w-123456"); err != nil {
		t.Fatal(err)
	}
	if m, _ := f.Machine("orbit-w-123456"); m.State != orb.StateStopped {
		t.Errorf("state after stop = %q", m.State)
	}
	if err := f.Run(ctx, orb.RunOptions{Machine: "orbit-w-123456", Stdin: strings.NewReader("echo")}); err != nil {
		t.Fatal(err)
	}
	if f.Runs[0].Input != "echo" {
		t.Errorf("recorded input = %q", f.Runs[0].Input)
	}
	if err := f.Delete(ctx, "orbit-w-123456"); err != nil {
		t.Fatal(err)
	}
	list, _ := f.List(ctx)
	if len(list) != 1 || list[0].Name != "ubuntu" {
		t.Errorf("list after delete = %+v", list)
	}
	want := []string{"create orbit-w-123456", "stop orbit-w-123456", "run orbit-w-123456",
		"delete orbit-w-123456", "list "}
	if !reflect.DeepEqual(f.Calls, want) {
		t.Errorf("calls = %q, want %q", f.Calls, want)
	}
}

func TestFakeInjectedErrors(t *testing.T) {
	boom := errors.New("boom")
	f := New()
	f.Errs["create"] = boom
	if err := f.Create(context.Background(), orb.CreateOptions{Name: "x"}); !errors.Is(err, boom) {
		t.Errorf("err = %v, want boom", err)
	}
	f.OnRun = func(Run) error { return &orb.ExitError{Code: 2} }
	var exit *orb.ExitError
	if err := f.Run(context.Background(), orb.RunOptions{}); !errors.As(err, &exit) || exit.Code != 2 {
		t.Errorf("run err = %v, want exit 2", err)
	}
}
