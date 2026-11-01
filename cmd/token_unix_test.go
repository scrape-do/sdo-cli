// SPDX-License-Identifier: Apache-2.0

//go:build unix

package cmd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// narrowOwnDir changes only a directory of the current user's that group or
// others can reach. An already private directory, a file, a missing path and
// a directory owned by someone else are left as they are.
func TestNarrowOwnDirTouchesOnlyALooseOwnDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := narrowOwnDir(dir); err != nil {
		t.Errorf("private directory: %v", err)
	}
	if d, _ := os.Stat(dir); d.Mode().Perm() != 0o700 {
		t.Errorf("private directory mode = %04o", d.Mode().Perm())
	}

	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chmod(file, 0o644) // umask may have narrowed the create
	if err := narrowOwnDir(file); err != nil {
		t.Errorf("file: %v", err)
	}
	if f, _ := os.Stat(file); f.Mode().Perm() != 0o644 {
		t.Errorf("a file was changed to %04o", f.Mode().Perm())
	}

	if err := narrowOwnDir(filepath.Join(dir, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing directory: %v, want fs.ErrNotExist", err)
	}

	// A directory open to others and owned by someone else: "/", or, when
	// the tests run as root (who owns "/"), a directory given to nobody.
	other := "/"
	if os.Getuid() == 0 {
		other = t.TempDir()
		if err := os.Chmod(other, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(other, 65534, 65534); err != nil {
			t.Skipf("cannot give a directory away: %v", err)
		}
	}
	before, err := os.Lstat(other)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := before.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) == os.Getuid() || !before.IsDir() || before.Mode().Perm()&0o077 == 0 {
		t.Skipf("%s is not a shared directory owned by someone else", other)
	}
	if err := narrowOwnDir(other); err != nil {
		t.Errorf("someone else's directory: %v", err)
	}
	if after, _ := os.Lstat(other); after.Mode().Perm() != before.Mode().Perm() {
		t.Errorf("%s was changed to %04o", other, after.Mode().Perm())
	}
}
