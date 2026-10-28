package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"
)

// Prefix marks an OrbStack machine as managed by orbit. Machines without it are never touched.
const Prefix = "orbit-"

// maxBaseLen keeps generated names short enough to read in OrbStack's UI.
const maxBaseLen = 40

// Machine is the part of an OrbStack machine that resolution needs.
type Machine struct {
	Name  string
	State string
	// Workspace is the source of the machine's first mount, or "" if it has none.
	Workspace string
}

// IsManaged reports whether a machine name belongs to orbit.
func IsManaged(name string) bool { return strings.HasPrefix(name, Prefix) }

// Name derives the machine name for a canonical workspace path:
// orbit-<basename>-<first 6 hex digits of sha256(path)>.
func Name(workspace string) string {
	sum := sha256.Sum256([]byte(workspace))
	return Prefix + slug(filepath.Base(workspace)) + "-" + hex.EncodeToString(sum[:])[:6]
}

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > maxBaseLen {
		out = strings.TrimRight(out[:maxBaseLen], "-")
	}
	if out == "" {
		return "ws"
	}
	return out
}

// Kind is the outcome of resolving a directory.
type Kind int

const (
	// Found: an existing workspace covers the directory.
	Found Kind = iota
	// Create: nothing covers the directory and nothing lies beneath it.
	Create
	// Overlap: the directory is a strict ancestor of existing workspaces; creating one is refused.
	Overlap
)

// Resolution says what to do for a directory.
type Resolution struct {
	Kind Kind
	// Machine is the covering workspace VM, for Found.
	Machine Machine
	// Overlaps are the workspaces beneath the directory, for Overlap, sorted by workspace path.
	Overlaps []Machine
}

// Resolve maps a canonical directory to its workspace VM. Only managed machines with a workspace
// mount take part. The nearest covering workspace wins; ties (which orbit never creates) go to the
// lowest machine name.
func Resolve(dir string, machines []Machine) Resolution {
	var best *Machine
	var beneath []Machine
	for i := range machines {
		m := machines[i]
		if !IsManaged(m.Name) || m.Workspace == "" {
			continue
		}
		switch {
		case Within(dir, m.Workspace):
			if best == nil || len(m.Workspace) > len(best.Workspace) ||
				(m.Workspace == best.Workspace && m.Name < best.Name) {
				best = &machines[i]
			}
		case Within(m.Workspace, dir):
			beneath = append(beneath, m)
		}
	}
	if best != nil {
		return Resolution{Kind: Found, Machine: *best}
	}
	if len(beneath) > 0 {
		sort.Slice(beneath, func(i, j int) bool { return beneath[i].Workspace < beneath[j].Workspace })
		return Resolution{Kind: Overlap, Overlaps: beneath}
	}
	return Resolution{Kind: Create}
}

// Within reports whether path equals dir or lies beneath it, comparing whole path segments, so
// /a/b is within /a but /a/bc is not within /a/b.
func Within(path, dir string) bool {
	if path == dir || dir == "/" {
		return true
	}
	return strings.HasPrefix(path, dir+"/")
}

// Orphaned reports whether a managed machine has lost its workspace: it has no workspace mount, or
// the directory no longer exists on the Mac (stat returns an error).
func Orphaned(m Machine, stat func(string) error) bool {
	return m.Workspace == "" || stat(m.Workspace) != nil
}
