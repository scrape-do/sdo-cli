// SPDX-License-Identifier: Apache-2.0

//go:build unix

package cmd

import (
	"os"
	"syscall"
)

// openNoFollow makes opening the config file fail on a symlink, and
// O_NONBLOCK keeps a FIFO swapped in for it from blocking the open.
const openNoFollow = syscall.O_NOFOLLOW | syscall.O_NONBLOCK

// narrowOwnDir sets dir to 0700 when the current user owns it and group or
// others have any access. A directory owned by someone else is left alone.
func narrowOwnDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 == 0 {
		return err
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Getuid() {
		return nil
	}
	return os.Chmod(dir, 0o700)
}
