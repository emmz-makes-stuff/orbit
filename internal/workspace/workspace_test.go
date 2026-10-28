package workspace

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestName(t *testing.T) {
	cases := []struct{ path, wantBase string }{
		{"/Users/emmz/github/emmz", "orbit-emmz-"},
		{"/Users/emmz/My Repos_2", "orbit-my-repos-2-"},
		{"/Users/emmz/--Weird..Name--", "orbit-weird-name-"},
		{"/Users/emmz/日本", "orbit-ws-"},
		{"/", "orbit-ws-"},
	}
	for _, tc := range cases {
		got := Name(tc.path)
		if !strings.HasPrefix(got, tc.wantBase) || len(got) != len(tc.wantBase)+6 {
			t.Errorf("Name(%q) = %q, want %q + 6 hex digits", tc.path, got, tc.wantBase)
		}
		if !IsManaged(got) {
			t.Errorf("Name(%q) = %q is not managed", tc.path, got)
		}
	}
}

func TestNameIsDeterministicAndDistinct(t *testing.T) {
	a, b := Name("/Users/e/one/app"), Name("/Users/e/two/app")
	if a != Name("/Users/e/one/app") {
		t.Error("Name is not deterministic")
	}
	if a == b {
		t.Errorf("same basename in different dirs gave the same name %q", a)
	}
}

func TestNameTruncatesLongBasenames(t *testing.T) {
	got := Name("/x/" + strings.Repeat("a", 100))
	if len(got) > len(Prefix)+maxBaseLen+1+6 {
		t.Errorf("Name too long: %q (%d chars)", got, len(got))
	}
}

func machines() []Machine {
	return []Machine{
		{Name: "orbit-emmz-aaaaaa", Workspace: "/Users/e/github/emmz"},
		{Name: "orbit-orbit-bbbbbb", Workspace: "/Users/e/work/orbit"},
		{Name: "ubuntu", Workspace: "/Users/e/github"}, // unmanaged, shares an ancestor
		{Name: "orbit-broken-cccccc", Workspace: ""},   // managed, no workspace mount
	}
}

func TestResolve(t *testing.T) {
	cases := []struct {
		name, dir string
		want      Resolution
	}{
		{"exact match", "/Users/e/github/emmz",
			Resolution{Kind: Found, Machine: machines()[0]}},
		{"nearest ancestor", "/Users/e/github/emmz/orbit/docs",
			Resolution{Kind: Found, Machine: machines()[0]}},
		{"segment prefix only", "/Users/e/github/emmzz",
			Resolution{Kind: Create}},
		{"parent of a workspace is refused", "/Users/e/work",
			Resolution{Kind: Overlap, Overlaps: []Machine{machines()[1]}}},
		{"unmanaged machine is neither match nor overlap", "/Users/e/github",
			Resolution{Kind: Overlap, Overlaps: []Machine{machines()[0]}}},
		{"unrelated directory", "/Users/e/elsewhere",
			Resolution{Kind: Create}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Resolve(tc.dir, machines()); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Resolve(%q) = %+v, want %+v", tc.dir, got, tc.want)
			}
		})
	}
}

func TestResolveUnmanagedOnlyCreates(t *testing.T) {
	ms := []Machine{{Name: "ubuntu", Workspace: "/Users/e/github/emmz"}}
	if got := Resolve("/Users/e/github/emmz", ms); got.Kind != Create {
		t.Errorf("Resolve with only an unmanaged match = %+v, want Create", got)
	}
	if got := Resolve("/Users/e/github", ms); got.Kind != Create {
		t.Errorf("Resolve above only an unmanaged machine = %+v, want Create", got)
	}
}

func TestResolvePrefersNearestAndSortsOverlaps(t *testing.T) {
	ms := []Machine{
		{Name: "orbit-outer-111111", Workspace: "/w"},
		{Name: "orbit-inner-222222", Workspace: "/w/a/b"},
	}
	if got := Resolve("/w/a/b/c", ms); got.Machine.Name != "orbit-inner-222222" {
		t.Errorf("nearest = %q, want orbit-inner-222222", got.Machine.Name)
	}

	ms = []Machine{
		{Name: "orbit-z-111111", Workspace: "/p/z"},
		{Name: "orbit-a-222222", Workspace: "/p/a"},
	}
	got := Resolve("/p", ms)
	if got.Kind != Overlap || got.Overlaps[0].Workspace != "/p/a" || got.Overlaps[1].Workspace != "/p/z" {
		t.Errorf("Resolve(/p) = %+v, want both overlaps sorted by workspace", got)
	}
}

func TestWithin(t *testing.T) {
	cases := []struct {
		path, dir string
		want      bool
	}{
		{"/a/b", "/a/b", true},
		{"/a/b/c", "/a/b", true},
		{"/a/bc", "/a/b", false},
		{"/a", "/a/b", false},
		{"/anything", "/", true},
	}
	for _, tc := range cases {
		if got := Within(tc.path, tc.dir); got != tc.want {
			t.Errorf("Within(%q, %q) = %v, want %v", tc.path, tc.dir, got, tc.want)
		}
	}
}

func TestOrphaned(t *testing.T) {
	exists := func(string) error { return nil }
	gone := func(string) error { return errors.New("no such file") }
	m := Machine{Name: "orbit-x-111111", Workspace: "/w"}
	if Orphaned(m, exists) {
		t.Error("existing workspace reported orphaned")
	}
	if !Orphaned(m, gone) {
		t.Error("missing workspace not reported orphaned")
	}
	if !Orphaned(Machine{Name: "orbit-x-111111"}, exists) {
		t.Error("machine with no workspace mount not reported orphaned")
	}
}
