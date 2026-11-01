// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	sdo "github.com/scrape-do/sdo-go"
)

// Token resolution for the CLI: --token > SDO_TOKEN > config file.
//
// The config file is the third tier so the token does not have to sit in shell
// history or in an exported environment variable that every child process
// inherits. It is deliberately stdlib-only — an OS keyring would mean a
// third-party dependency in a credential-custodian binary, which is exactly the
// supply-chain surface this program keeps minimal.

// configFilePath is the per-user credentials file (SDO_CONFIG_FILE, else
// <user config dir>/sdo/config.json).
func configFilePath() (string, error) {
	if override := os.Getenv("SDO_CONFIG_FILE"); override != "" {
		return override, nil
	}
	dir, err := os.UserConfigDir() // XDG_CONFIG_HOME on Linux, ~/Library/Application Support on macOS
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sdo", "config.json"), nil
}

type fileConfig struct {
	Token string `json:"token"`
}

// lstatConfig returns the config file's info, or nil when nothing is there
// yet. A symlink or a non-regular file is refused: following a link would read
// the token from, or write it into, whatever the link points at (a dotfiles
// repository, another user's file).
func lstatConfig(path string) (fs.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("config file %s is a symlink; refusing to follow it with a credential "+
			"(replace it with a regular file, or set SDO_CONFIG_FILE)", path)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("config file %s is not a regular file", path)
	}
	return info, nil
}

// tokenFromConfigFile reads the token from the per-user config file.
//
// A file readable by other users is refused rather than used: silently loading
// a world-readable credential would hide exactly the misconfiguration that
// matters. Returns "" when no file exists.
//
// lstatConfig gives the friendly errors; the file is then opened without
// following a symlink (where the OS can refuse one) and the type and mode
// are checked on the opened file itself, so a link or a wider file swapped in
// after the Lstat is still refused.
func tokenFromConfigFile() (string, error) {
	path, err := configFilePath()
	if err != nil {
		return "", nil // no discoverable config dir: not an error, just no token
	}
	info, err := lstatConfig(path)
	if err != nil || info == nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|openNoFollow, 0)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	defer f.Close()
	if info, err = f.Stat(); err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("config file %s is not a regular file", path)
	}
	// Windows does not carry POSIX permission bits, so only check where they mean something.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf(
			"config file %s is readable by other users (mode %04o); run: chmod 600 %s",
			path, info.Mode().Perm(), path)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	var cfg fileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg.Token, nil
}

// writeTokenToConfigFile stores the token with owner-only permissions.
//
// The file is written whole to a 0600 temporary file in the same directory and
// renamed over the target, so the token is never in a file anyone else can
// read — not even when a file already existed with wider permissions, which a
// plain os.WriteFile would keep — and a crash cannot leave half a file.
func writeTokenToConfigFile(token string) (string, error) {
	path, err := configFilePath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if os.Getenv("SDO_CONFIG_FILE") == "" {
		// The default <user config dir>/sdo is the CLI's own; one created
		// looser (by hand, or by an older version) is narrowed. A directory
		// named by SDO_CONFIG_FILE may be shared with other files, so it is
		// left as it is.
		if err := narrowOwnDir(filepath.Dir(path)); err != nil {
			return "", err
		}
	}
	if _, err := lstatConfig(path); err != nil {
		return "", err
	}
	data, _ := json.MarshalIndent(fileConfig{Token: token}, "", "  ") // a struct of one string always encodes
	if err := writeFileAtomic(path, append(data, '\n')); err != nil {
		return "", err
	}
	return path, nil
}

// writeFileAtomic writes data to a 0600 temporary file next to path and
// renames it over path. It serves the config file and --output-file.
func writeFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	steps := []func() error{
		func() error { return f.Chmod(0o600) }, // before any byte of the token lands
		func() error { _, err := f.Write(data); return err },
		f.Sync,
		f.Close,
		func() error { return os.Rename(f.Name(), path) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			f.Close()
			os.Remove(f.Name())
			return err
		}
	}
	return nil
}

// resolveToken applies the documented precedence and says where the token
// came from ("none" when nowhere).
//
// Surrounding whitespace is trimmed from every source, since a secret mounted
// from a file or pasted from a terminal often ends in a newline, and a value
// that is empty once trimmed counts as not given. Whitespace, control or
// non-ASCII characters inside the token are an error naming the source: sent
// as is they would fail as a confusing connection or authentication error.
func (a *app) resolveToken() (token, source string, err error) {
	if token = trimToken(a.token); token != "" {
		return token, "--token flag", checkToken(token, "--token flag")
	}
	if token = trimToken(os.Getenv("SDO_TOKEN")); token != "" {
		source = "SDO_TOKEN environment variable"
		return token, source, checkToken(token, source)
	}
	raw, err := tokenFromConfigFile()
	if err != nil {
		return "", "", err
	}
	if token = trimToken(raw); token == "" {
		return "", "none", nil
	}
	path, _ := configFilePath()
	source = "config file " + path
	return token, source, checkToken(token, source)
}

// trimToken removes surrounding ASCII whitespace.
func trimToken(s string) string { return strings.Trim(s, " \t\r\n\v\f") }

// checkToken accepts only visible ASCII, which every Scrape.do token is.
func checkToken(token, source string) error {
	for i := 0; i < len(token); i++ {
		if c := token[i]; c <= ' ' || c >= 0x7f {
			return &sdo.ConfigError{Message: fmt.Sprintf(
				"the token from the %s contains whitespace, control or non-ASCII characters; "+
					"check that it was copied whole and nothing else", source)}
		}
	}
	return nil
}
