// SPDX-License-Identifier: Apache-2.0

//go:build !unix

package cmd

// openNoFollow is 0 where the OS has no O_NOFOLLOW; lstatConfig and the
// check on the opened file still refuse a link or a non-regular file.
const openNoFollow = 0

// narrowOwnDir is a no-op where POSIX permission bits do not apply.
func narrowOwnDir(string) error { return nil }
