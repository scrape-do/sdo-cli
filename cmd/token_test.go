// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// withConfigFile points token resolution at a temp file and clears the env so
// each case exercises exactly one tier of the precedence chain.
func withConfigFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("SDO_CONFIG_FILE", path)
	t.Setenv("SDO_TOKEN", "")
	return path
}

func resolve(t *testing.T) (string, error) {
	t.Helper()
	token, _, err := (&app{}).resolveToken()
	return token, err
}

func TestTokenPrecedenceFlagBeatsEnvBeatsFile(t *testing.T) {
	path := withConfigFile(t)
	if _, err := writeTokenToConfigFile("from-file"); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := resolve(t)
	if err != nil || got != "from-file" {
		t.Fatalf("file tier: got %q err=%v", got, err)
	}

	t.Setenv("SDO_TOKEN", "from-env")
	if got, _ := resolve(t); got != "from-env" {
		t.Errorf("env should beat file, got %q", got)
	}

	if got, _, _ := (&app{token: "from-flag"}).resolveToken(); got != "from-flag" {
		t.Errorf("flag should beat env, got %q", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config file should still exist: %v", err)
	}
}

func TestNoTokenAnywhereIsNotAnError(t *testing.T) {
	withConfigFile(t)
	got, err := resolve(t)
	if err != nil || got != "" {
		t.Errorf("absent config should yield (\"\", nil), got (%q, %v)", got, err)
	}
}

func TestConfigFileIsWrittenOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	dir := filepath.Join(t.TempDir(), "sdo")
	path := filepath.Join(dir, "config.json")
	t.Setenv("SDO_CONFIG_FILE", path)
	if _, err := writeTokenToConfigFile("secret"); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file mode = %04o, want 0600", perm)
	}
	if d, _ := os.Stat(dir); d.Mode().Perm() != 0o700 {
		t.Errorf("config dir mode = %04o, want 0700", d.Mode().Perm())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("temporary files left behind: %v", entries)
	}
}

// An existing file with wider permissions (created by hand, say) must not keep
// them when login writes the token into it: os.WriteFile applies the mode only
// when it creates the file.
func TestLoginNarrowsAnExistingWideFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	path := withConfigFile(t)
	if err := os.WriteFile(path, []byte(`{"token":"old"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chmod(path, 0o644) // umask may have narrowed the create
	if _, err := writeTokenToConfigFile("new-secret"); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, _ := os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode after login = %04o, want 0600", perm)
	}
	if got, err := resolve(t); err != nil || got != "new-secret" {
		t.Errorf("token = %q err=%v", got, err)
	}
}

// A symlink at the config path is refused on both write and read: following it
// would put the token into (or take it from) whatever the link points at.
func TestConfigSymlinkIsRefused(t *testing.T) {
	path := withConfigFile(t)
	target := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(target, []byte(`{"token":"linked"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	if _, err := writeTokenToConfigFile("secret"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("write through a symlink: err = %v", err)
	}
	if data, _ := os.ReadFile(target); strings.Contains(string(data), "secret") {
		t.Errorf("the token was written through the link: %s", data)
	}
	if _, err := resolve(t); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("read through a symlink: err = %v", err)
	}
}

// A world-readable credentials file is refused rather than used: silently
// loading it would hide exactly the misconfiguration that matters.
func TestWorldReadableConfigFileIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	path := withConfigFile(t)
	if _, err := writeTokenToConfigFile("secret"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	_, err := resolve(t)
	if err == nil {
		t.Fatal("expected a permissions error")
	}
	if !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("error should say how to fix it, got %q", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("error must not echo the token: %q", err)
	}
}

func TestMalformedConfigFileReportsPathNotToken(t *testing.T) {
	path := withConfigFile(t)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := resolve(t)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("expected an error naming %s, got %v", path, err)
	}
}

// The config file is opened without following a symlink, so a link swapped in
// after lstatConfig is refused by the open itself.
func TestConfigOpenDoesNotFollowSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no O_NOFOLLOW on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	if err := os.WriteFile(target, []byte(`{"token":"linked"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "config.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	if f, err := os.OpenFile(link, os.O_RDONLY|openNoFollow, 0); err == nil {
		f.Close()
		t.Error("opening the config path followed a symlink")
	}
}

// Login narrows the default config directory when it exists with group or
// other access; a directory named through SDO_CONFIG_FILE is left alone.
func TestLoginNarrowsTheDefaultConfigDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)                                  // macOS: ~/Library/Application Support
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg")) // Linux and the other Unixes
	t.Setenv("SDO_CONFIG_FILE", "")
	path, err := configFilePath()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0o755) // umask may have narrowed the create
	if _, err := writeTokenToConfigFile("secret"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if d, _ := os.Stat(dir); d.Mode().Perm() != 0o700 {
		t.Errorf("default config dir mode = %04o, want 0700", d.Mode().Perm())
	}

	shared := t.TempDir()
	os.Chmod(shared, 0o755)
	t.Setenv("SDO_CONFIG_FILE", filepath.Join(shared, "config.json"))
	if _, err := writeTokenToConfigFile("secret"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if d, _ := os.Stat(shared); d.Mode().Perm() != 0o755 {
		t.Errorf("an SDO_CONFIG_FILE directory was changed to %04o", d.Mode().Perm())
	}
}
