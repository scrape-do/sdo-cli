// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/scrape-do/sdo-cli/internal/testenv"
)

// TestMain points token resolution at a path that does not exist, and clears
// the SDK's environment, so no test can pick up the developer's real token or
// settings and reach the live gateway.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "sdo-cli-test")
	if err != nil {
		panic(err)
	}
	testenv.Clear()
	os.Setenv("SDO_CONFIG_FILE", filepath.Join(dir, "absent", "config.json"))
	// A dead address, so a test that forgets to point `sdo version --check` at
	// its own server fails instead of asking the real GitHub API.
	githubAPI = "http://127.0.0.1:9"
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// Every failure reaches the user once, as `sdo: <reason>`, with an exit code a
// script can branch on.
func TestMainPrintsErrorsWithExitCodes(t *testing.T) {
	t.Setenv("SDO_TOKEN", "")
	cases := []struct {
		args []string
		want string
		code int
	}{
		{[]string{"scrape", "https://e.com"}, "no API token", exitAuth},
		{[]string{"scrape", "--nope"}, "unknown flag: --nope", exitUsage},
		{[]string{"scrape"}, "accepts 1 arg(s)", exitUsage},
		{[]string{"nope"}, `unknown command "nope" for "sdo"`, exitUsage},
		{[]string{"scrap", "https://e.com"}, "did you mean scrape?", exitUsage},
		{[]string{"jobs", "stauts", "x"}, `unknown command "stauts" for "sdo jobs"`, exitUsage},
		{[]string{"auth", "logout"}, `unknown command "logout" for "sdo auth"`, exitUsage},
		{[]string{"mcp", "run"}, `unknown command "run" for "sdo mcp"`, exitUsage},
		{[]string{"completion", "bash", "extra"}, `unknown command "extra"`, exitUsage},
		{[]string{"--token", "t", "plugin", "../x"}, "invalid plugin route", exitUsage},
		{[]string{"--token", "t", "scrape", "ftp://e.com"}, "", exitUsage},
		{[]string{"--token", "t", "scrape", "https://e.com", "--timeout", "30"}, "milliseconds", exitUsage},
	}
	for _, c := range cases {
		out, errOut, code := runCLI(t, "", c.args...)
		if code != c.code {
			t.Errorf("%v: exit code = %d, want %d (stderr %q)", c.args, code, c.code, errOut)
		}
		if !strings.HasPrefix(errOut, "sdo: ") || !strings.Contains(errOut, c.want) {
			t.Errorf("%v: stderr = %q, want an 'sdo: …%s…' line", c.args, errOut, c.want)
		}
		if out != "" {
			t.Errorf("%v: a failure wrote to stdout: %q", c.args, out)
		}
	}
}

// Only command-line mistakes get the "--help" pointer; it names the command.
func TestUsageErrorsPointAtHelp(t *testing.T) {
	_, errOut, _ := runCLI(t, "", "jobs", "result", "only-one")
	if !strings.Contains(errOut, "Run 'sdo jobs result --help' for usage.") {
		t.Errorf("stderr = %q", errOut)
	}
	t.Setenv("SDO_TOKEN", "")
	if _, errOut, _ := runCLI(t, "", "account"); strings.Contains(errOut, "--help") {
		t.Errorf("a missing token is not a usage error: %q", errOut)
	}
}

func TestGatewayAuthFailureExitsWithAuthCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_BASE_URL", srv.URL)
	if _, _, code := runCLI(t, "", "--token", "t", "scrape", "https://e.com"); code != exitAuth {
		t.Errorf("401 exit code = %d, want %d", code, exitAuth)
	}
	t.Setenv("SDO_BASE_URL", "http://127.0.0.1:1") // nothing listens: a plain failure
	t.Setenv("SDO_TIMEOUT", "5")
	if _, _, code := runCLI(t, "", "--token", "t", "scrape", "https://e.com"); code != exitFailure {
		t.Errorf("connection failure exit code = %d, want %d", code, exitFailure)
	}
}

// A bare group command prints its help and succeeds, as before.
func TestBareGroupCommandsPrintHelp(t *testing.T) {
	for _, args := range [][]string{{}, {"jobs"}, {"auth"}, {"mcp"}} {
		out, errOut, code := runCLI(t, "", args...)
		if code != 0 || !strings.Contains(out, "Available Commands") {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out, errOut)
		}
	}
}

func TestMainSucceedsWithExitZero(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := Main([]string{"version"}, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, stderr=%q", code, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "sdo CLI ") || errOut.Len() != 0 {
		t.Errorf("stdout=%q stderr=%q", out.String(), errOut.String())
	}
}

// Invocations share no state: the global flags live on a per-invocation app,
// so concurrent runs (with -race) cannot see each other's --token.
func TestConcurrentInvocationsAreIndependent(t *testing.T) {
	withConfigFile(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := []string{"auth", "status"}
			want := "token source: none"
			if i%2 == 0 {
				args = append(args, "--token", "x")
				want = "--token flag"
			}
			var out, errOut bytes.Buffer
			newApp().run(args, strings.NewReader(""), &out, &errOut)
			if !strings.Contains(out.String(), want) {
				t.Errorf("run %d: %q, want %q", i, out.String(), want)
			}
		}(i)
	}
	wg.Wait()
}
