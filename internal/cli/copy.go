package cli

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/emmz-makes-stuff/orbit/internal/orb"
)

// copyExtras replaces each path's copy in the machine with the Mac's current contents, owned by
// user. The Mac side is only read. A path that is itself a symlink is copied as what it points to,
// so a symlinked dotfile arrives as a file at its declared path; symlinks inside directories are
// kept as symlinks.
func copyExtras(ctx context.Context, client orb.Client, machine, user string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(writeTar(pw, paths)) }()

	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = shellQuote(p)
	}
	list := strings.Join(quoted, " ")
	script := fmt.Sprintf("set -e; rm -rf -- %s; tar -C / -xpf -; chown -R -h %s -- %s",
		list, shellQuote(user), list)

	var stderr strings.Builder
	err := client.Run(ctx, orb.RunOptions{
		Machine: machine,
		User:    "root",
		Args:    []string{"sh", "-c", script},
		Stdin:   pr,
		Stderr:  &stderr,
	})
	_ = pr.Close()
	if err != nil {
		return fmt.Errorf("copy read-only extras into %s: %v %s", machine, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// writeTar archives each absolute path under its own name, relative to /.
func writeTar(w io.Writer, paths []string) error {
	tw := tar.NewWriter(w)
	for _, p := range paths {
		if err := addPath(tw, p); err != nil {
			return err
		}
	}
	return tw.Close()
}

func addPath(tw *tar.Writer, declared string) error {
	// Follow a top-level symlink: the VM gets the target's contents at the declared path.
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
		name := strings.TrimPrefix(filepath.Join(declared, rel), "/")

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

// shellQuote quotes s for POSIX sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
