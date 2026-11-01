// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAuthLoginReadsStdinAndDoesNotEchoTheToken(t *testing.T) {
	withConfigFile(t)
	out, errOut, code := runCLI(t, "SUPERSECRETTOKEN\n", "auth", "login")
	if code != 0 {
		t.Fatalf("auth login exited %d: %s", code, errOut)
	}
	if strings.Contains(out+errOut, "SUPERSECRETTOKEN") {
		t.Errorf("auth login echoed the token: %q %q", out, errOut)
	}
	if got, _ := resolve(t); got != "SUPERSECRETTOKEN" {
		t.Errorf("token not stored, got %q", got)
	}
}

// On a terminal the token is read with echo off, after a prompt on stderr.
func TestAuthLoginReadsATerminalWithoutEcho(t *testing.T) {
	withConfigFile(t)
	tty, err := os.Open(os.DevNull) // any *os.File; the hooks decide it is a terminal
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	var echoOff bool
	a := &app{
		isTerminal:   func(int) bool { return true },
		readPassword: func(int) ([]byte, error) { echoOff = true; return []byte("  TTYSECRET12  "), nil },
	}
	var out, errOut bytes.Buffer
	if code := a.run([]string{"auth", "login"}, tty, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if !echoOff || !strings.Contains(errOut.String(), "input hidden") {
		t.Errorf("the terminal path was not taken: prompt %q", errOut.String())
	}
	if got, _ := resolve(t); got != "TTYSECRET12" {
		t.Errorf("stored token = %q", got)
	}

	a.readPassword = func(int) ([]byte, error) { return nil, errors.New("interrupted") }
	if code := a.run([]string{"auth", "login"}, tty, &out, &errOut); code != 1 ||
		!strings.Contains(errOut.String(), "interrupted") {
		t.Errorf("a failed terminal read must fail the login: exit %d, %q", code, errOut.String())
	}
}

// A non-terminal *os.File (a redirected file) is read as a line, not as a TTY.
func TestAuthLoginReadsARedirectedFile(t *testing.T) {
	withConfigFile(t)
	in := filepath.Join(t.TempDir(), "token.txt")
	os.WriteFile(in, []byte("FILETOKEN123\n"), 0o600)
	f, _ := os.Open(in)
	defer f.Close()
	var out, errOut bytes.Buffer
	if code := newApp().run([]string{"auth", "login"}, f, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if got, _ := resolve(t); got != "FILETOKEN123" {
		t.Errorf("stored token = %q", got)
	}
}

func TestAuthStatusNeverPrintsTheToken(t *testing.T) {
	withConfigFile(t)
	if _, err := writeTokenToConfigFile("SUPERSECRETTOKEN"); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, errOut, code := runCLI(t, "", "auth", "status")
	if code != 0 {
		t.Fatalf("auth status exited %d: %s", code, errOut)
	}
	if strings.Contains(out+errOut, "SUPERSECRETTOKEN") {
		t.Errorf("auth status printed the token: %q", out)
	}
	if !strings.Contains(out, "config file") {
		t.Errorf("auth status should name the source, got %q", out)
	}
}

func TestAuthLoginRejectsEmptyInput(t *testing.T) {
	withConfigFile(t)
	for name, stdin := range map[string]string{"closed stdin": "", "blank line": "   \n"} {
		if _, _, code := runCLI(t, stdin, "auth", "login"); code == 0 {
			t.Errorf("%s: auth login accepted no token", name)
		}
	}
}

func TestAuthLoginTakesTokenFlag(t *testing.T) {
	withConfigFile(t)
	if _, _, err := execute(t, "auth", "login", "--token", "FROMFLAG"); err != nil {
		t.Fatalf("auth login --token: %v", err)
	}
	if got, _ := resolve(t); got != "FROMFLAG" {
		t.Errorf("stored token = %q", got)
	}
}

func TestAuthLoginReportsUnwritableConfigPath(t *testing.T) {
	// A regular file where the config directory should be: MkdirAll fails.
	blocker := filepath.Join(t.TempDir(), "blocker")
	os.WriteFile(blocker, []byte("x"), 0o600)
	t.Setenv("SDO_CONFIG_FILE", filepath.Join(blocker, "sdo", "config.json"))
	t.Setenv("SDO_TOKEN", "")
	if _, _, code := runCLI(t, "tok\n", "auth", "login"); code == 0 {
		t.Error("writing under a regular file should fail")
	}
}

// The temp file is created next to the target; a directory that refuses new
// files fails the login instead of falling back to a non-atomic write.
func TestAuthLoginReportsAnUnwritableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	dir := t.TempDir()
	t.Setenv("SDO_CONFIG_FILE", filepath.Join(dir, "config.json"))
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if _, err := writeTokenToConfigFile("secret"); err == nil {
		t.Skip("running with privileges that ignore directory permissions")
	}
}

func TestAuthStatusNamesEachSource(t *testing.T) {
	withConfigFile(t)
	cases := []struct {
		args []string
		env  string
		want string
	}{
		{[]string{"auth", "status", "--token", "x"}, "", "--token flag"},
		{[]string{"auth", "status"}, "fromenv", "SDO_TOKEN environment variable"},
		{[]string{"auth", "status"}, "", "token source: none"},
	}
	for _, c := range cases {
		t.Setenv("SDO_TOKEN", c.env)
		out, _, err := execute(t, c.args...)
		if err != nil || !strings.Contains(out, c.want) {
			t.Errorf("%v env=%q: out=%q err=%v, want %q", c.args, c.env, out, err, c.want)
		}
	}
}

func TestAuthStatusSurfacesAConfigProblem(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	path := withConfigFile(t)
	writeTokenToConfigFile("secret")
	os.Chmod(path, 0o644)
	if _, _, err := execute(t, "auth", "status"); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Errorf("status should report the permission problem, got %v", err)
	}
}

func TestConfigFileThatIsADirectoryIsAnError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SDO_CONFIG_FILE", dir)
	t.Setenv("SDO_TOKEN", "")
	if _, err := resolve(t); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("a directory at the config path should be an error, got %v", err)
	}
	if _, err := writeTokenToConfigFile("x"); err == nil {
		t.Error("writing the token over a directory should fail")
	}
}

// A config path whose parent cannot be inspected is an error, not "no token".
func TestConfigFileBehindAFileIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports a missing path here, which is simply no file")
	}
	blocker := filepath.Join(t.TempDir(), "blocker")
	os.WriteFile(blocker, []byte("x"), 0o600)
	t.Setenv("SDO_CONFIG_FILE", filepath.Join(blocker, "config.json"))
	t.Setenv("SDO_TOKEN", "")
	if _, err := resolve(t); err == nil {
		t.Error("an unreadable config location should be reported")
	}
}

func TestDefaultConfigPathUsesUserConfigDir(t *testing.T) {
	t.Setenv("SDO_CONFIG_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	path, err := configFilePath()
	if err != nil {
		t.Fatalf("configFilePath: %v", err)
	}
	if filepath.Base(path) != "config.json" || filepath.Base(filepath.Dir(path)) != "sdo" {
		t.Errorf("path = %q, want …/sdo/config.json", path)
	}
}

// With no discoverable config directory there is simply no file tier.
func TestNoConfigDirMeansNoFileToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.UserConfigDir reads APPDATA on Windows")
	}
	t.Setenv("SDO_CONFIG_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("SDO_TOKEN", "")
	got, err := resolve(t)
	if err != nil || got != "" {
		t.Errorf("want (\"\", nil) with no config dir, got (%q, %v)", got, err)
	}
	if _, err := writeTokenToConfigFile("x"); err == nil {
		t.Error("writing with no config dir should fail")
	}
}

// A failed write leaves no temporary file (with the token in it) behind.
func TestAtomicWriteCleansUpOnFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	os.MkdirAll(filepath.Join(target, "occupied"), 0o700) // rename onto a non-empty directory fails
	if err := writeFileAtomic(target, []byte("secret")); err == nil {
		t.Fatal("rename onto a non-empty directory should fail")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temporary file left behind: %v", entries)
	}
}

// An owner-only file that still cannot be read is reported, not ignored.
func TestUnreadableConfigFileIsAnError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	path := withConfigFile(t)
	writeTokenToConfigFile("secret")
	os.Chmod(path, 0o000)
	t.Cleanup(func() { os.Chmod(path, 0o600) })
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("running with privileges that ignore file permissions")
	}
	if _, err := resolve(t); err == nil || !strings.Contains(err.Error(), "read "+path) {
		t.Errorf("err = %v", err)
	}
}
