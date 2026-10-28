package cli

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/emmz-makes-stuff/orbit/internal/orb"
)

// copyExtras replaces each copy's Destination in the machine with the current contents of its Mac
// Source, owned by user, as are any directories it needs under home. The Mac side is only read. A
// source that is itself a symlink is copied as what it points to, so a symlinked dotfile arrives as
// a file; symlinks inside directories are kept as symlinks.
func copyExtras(ctx context.Context, client orb.Client, machine, user, home string, copies []orb.Mount) error {
	if len(copies) == 0 {
		return nil
	}
	pr, pw := io.Pipe()
	archived := make(chan error, 1)
	go func() {
		err := writeTar(pw, copies)
		pw.CloseWithError(err)
		archived <- err
	}()

	dsts := make([]string, len(copies))
	for i, c := range copies {
		dsts[i] = c.Target()
	}
	list := quoteAll(dsts)
	script := fmt.Sprintf("set -e; %srm -rf -- %s; tar -C / -xpf -; chown -R -h %s -- %s",
		ownDirsScript(user, homeParents(home, dsts)), list, shellQuote(user), list)

	var stderr strings.Builder
	err := client.Run(ctx, orb.RunOptions{
		Machine: machine,
		User:    "root",
		Args:    []string{"sh", "-c", script},
		Stdin:   pr,
		Stderr:  &stderr,
	})
	_ = pr.Close() // unblocks writeTar if the VM stopped reading early
	// A Mac-side read failure is the real cause even when tar in the VM accepted a short archive.
	// A closed pipe only means the VM side gave up first, so its error explains more.
	if werr := <-archived; werr != nil && (err == nil || !errors.Is(werr, io.ErrClosedPipe)) {
		return fmt.Errorf("copy read-only extras into %s: read them on this Mac: %w", machine, werr)
	}
	if err != nil {
		return fmt.Errorf("copy read-only extras into %s: %v %s", machine, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// writeTar archives each copy's Source under its Destination, relative to /.
func writeTar(w io.Writer, copies []orb.Mount) error {
	tw := tar.NewWriter(w)
	for _, c := range copies {
		if err := addPath(tw, c.Source, c.Target()); err != nil {
			return err
		}
	}
	return tw.Close()
}

func addPath(tw *tar.Writer, declared, dst string) error {
	// Follow a top-level symlink: the VM gets the target's contents at the destination.
	root, err := filepath.EvalSymlinks(declared)
	if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := de.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		name := strings.TrimPrefix(path.Join(dst, filepath.ToSlash(rel)), "/")

		link := ""
		if info.Mode()&fs.ModeSymlink != 0 {
			if link, err = os.Readlink(p); err != nil {
				return err
			}
		}
		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		hdr.Name = name
		if info.IsDir() {
			hdr.Name += "/"
		}
		// Ownership is set by chown in the VM; Mac uids/gids mean nothing there.
		hdr.Uid, hdr.Gid, hdr.Uname, hdr.Gname = 0, 0, "", ""
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
}

// quoteAll quotes each of ss for POSIX sh and joins them with spaces.
func quoteAll(ss []string) string {
	quoted := make([]string, len(ss))
	for i, s := range ss {
		quoted[i] = shellQuote(s)
	}
	return strings.Join(quoted, " ")
}

// shellQuote quotes s for POSIX sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
