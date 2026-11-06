// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/scrape-do/sdo-cli/internal/version"
	sdo "github.com/scrape-do/sdo-go"
)

// versionLines is what `sdo version` prints with or without --check.
var versionLines = "sdo CLI " + version.CLI + "\nsdo-go SDK " + sdo.Version + "\n"

// fakeGitHub points the check at handler for the duration of the test. The
// package variables it swaps are why these tests never run in parallel.
func fakeGitHub(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	saved := githubAPI
	githubAPI = srv.URL
	t.Cleanup(func() { githubAPI = saved })
	return srv
}

func releaseJSON(tag string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"url":"x","tag_name":%q,"name":"ignored","body":"ignored"}`, tag)
	}
}

// The latest release is the CLI's own repository, asked anonymously, with the
// CLI's User-Agent and the API version GitHub documents.
func TestVersionCheckRequest(t *testing.T) {
	var got *http.Request
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(r.Context())
		releaseJSON("v"+version.CLI)(w, r)
	})
	if _, stderr, code := runCLI(t, "", "version", "--check"); code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if got.Method != http.MethodGet || got.URL.Path != "/repos/scrape-do/sdo-cli/releases/latest" || got.URL.RawQuery != "" {
		t.Errorf("request = %s %s", got.Method, got.URL)
	}
	for k, want := range map[string]string{
		"Accept":               "application/vnd.github+json",
		"X-Github-Api-Version": "2022-11-28",
		"User-Agent":           version.UserAgent,
	} {
		if v := got.Header.Get(k); v != want {
			t.Errorf("%s = %q, want %q", k, v, want)
		}
	}
}

// The check must never carry the Scrape.do token, whichever source holds it.
func TestVersionCheckSendsNoToken(t *testing.T) {
	const secret = "sdo-test-token-0123456789"
	t.Setenv("SDO_TOKEN", secret)
	var headers http.Header
	var query string
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		headers, query = r.Header.Clone(), r.URL.RawQuery
		releaseJSON("v"+version.CLI)(w, r)
	})
	if _, stderr, code := runCLI(t, "", "--token", secret, "version", "--check"); code != exitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	for _, k := range []string{"Authorization", "X-Token", "Cookie"} {
		if v := headers.Get(k); v != "" {
			t.Errorf("%s header sent: %q", k, v)
		}
	}
	for k, vs := range headers {
		for _, v := range vs {
			if strings.Contains(v, secret) {
				t.Errorf("header %s carries the token", k)
			}
		}
	}
	if strings.Contains(query, "token") || strings.Contains(query, secret) {
		t.Errorf("query %q carries a token", query)
	}
}

func TestVersionCheckOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, tag string
		want      string
	}{
		{"newer", "v99.0.0", "latest release: v99.0.0 (installed: " + version.CLI + ")\n" +
			"upgrade: brew upgrade sdo\n" +
			"upgrade: scoop update sdo\n" +
			"upgrade: go install github.com/scrape-do/sdo-cli/cmd/sdo@latest\n" +
			"download: https://github.com/scrape-do/sdo-cli/releases/tag/v99.0.0\n"},
		{"up to date", "v" + version.CLI, "latest release: v" + version.CLI + " (up to date)\n"},
		{"installed newer", "v0.0.1", "latest release: v0.0.1 (installed " + version.CLI + " is newer)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeGitHub(t, releaseJSON(tc.tag))
			out, stderr, code := runCLI(t, "", "version", "--check")
			if code != exitOK || stderr != "" {
				t.Errorf("exit %d, stderr %q; a newer release is not a failure", code, stderr)
			}
			if out != versionLines+tc.want {
				t.Errorf("stdout = %q, want %q", out, versionLines+tc.want)
			}
		})
	}
}

// Every way the check can fail still prints the versions, names a reason that
// holds no text from the response, and exits 1.
func TestVersionCheckFailures(t *testing.T) {
	const planted = "PLANTED-RESPONSE-TEXT"
	big := `{"tag_name":"v9.9.9","pad":"` + strings.Repeat("a", 64<<10) + `"}`
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		reason  string
	}{
		{"bad tag", releaseJSON("v1.2.3-" + planted), "the latest release could not be determined"},
		{"tag with build", releaseJSON("v1.2.3+" + planted), "the latest release could not be determined"},
		{"tag without v", releaseJSON("1.2.3"), "the latest release could not be determined"},
		{"tag leading zero", releaseJSON("v01.2.3"), "the latest release could not be determined"},
		{"tag overflow", releaseJSON("v99999999999999999999.0.0"), "the latest release could not be determined"},
		{"no tag", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"name":"`+planted+`"}`) },
			"the latest release could not be determined"},
		{"tag not a string", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"tag_name":3}`) },
			"the response is not a release"},
		{"invalid JSON", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, planted+"{") },
			"the response is not a release"},
		{"JSON array", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `["v9.9.9"]`) },
			"the response is not a release"},
		{"404", func(w http.ResponseWriter, r *http.Request) { http.Error(w, planted, http.StatusNotFound) },
			"GitHub answered HTTP 404"},
		{"500", func(w http.ResponseWriter, r *http.Request) { http.Error(w, planted, http.StatusInternalServerError) },
			"GitHub answered HTTP 500"},
		{"rate limited", func(w http.ResponseWriter, r *http.Request) { http.Error(w, planted, http.StatusForbidden) },
			"GitHub answered HTTP 403 (its limit for anonymous requests may be used up; try again later)"},
		{"too many requests", func(w http.ResponseWriter, r *http.Request) { http.Error(w, planted, http.StatusTooManyRequests) },
			"GitHub answered HTTP 429 (its limit for anonymous requests may be used up; try again later)"},
		{"oversized", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, big) },
			"the response is larger than 64 KiB"},
		{"truncated", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "100")
			fmt.Fprint(w, `{"tag_name":`)
		}, "reading the response failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeGitHub(t, tc.handler)
			out, stderr, code := runCLI(t, "", "version", "--check")
			if code != exitFailure {
				t.Errorf("exit %d, want %d", code, exitFailure)
			}
			if out != versionLines {
				t.Errorf("stdout = %q, want only the version lines", out)
			}
			want := "sdo: could not check for a newer release: " + tc.reason + "\n"
			if !strings.HasPrefix(stderr, strings.TrimSuffix(want, "\n")) {
				t.Errorf("stderr = %q, want %q", stderr, want)
			}
			if strings.Contains(stderr, planted) || strings.Contains(out, planted) {
				t.Errorf("response text echoed: %q", stderr)
			}
		})
	}
}

// A redirect is refused, never followed: the check talks to one host only.
func TestVersionCheckRefusesRedirects(t *testing.T) {
	followed := false
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followed = true
		releaseJSON("v99.0.0")(w, r)
	}))
	t.Cleanup(elsewhere.Close)
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusFound)
	})
	out, stderr, code := runCLI(t, "", "version", "--check")
	if followed {
		t.Error("the redirect was followed")
	}
	if code != exitFailure || out != versionLines ||
		stderr != "sdo: could not check for a newer release: GitHub answered with a redirect, which is not followed\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, stderr)
	}
}

// A server that never answers costs at most the check's timeout.
func TestVersionCheckTimesOut(t *testing.T) {
	release := make(chan struct{})
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) }) // runs before the server closes
	saved := versionCheckTimeout
	versionCheckTimeout = 100 * time.Millisecond
	t.Cleanup(func() { versionCheckTimeout = saved })
	start := time.Now()
	out, stderr, code := runCLI(t, "", "version", "--check")
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %v", elapsed)
	}
	if code != exitFailure || out != versionLines ||
		stderr != "sdo: could not check for a newer release: no answer from GitHub within 100ms\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, stderr)
	}
}

// A connection that fails outright is reported as such, without the URL.
func TestVersionCheckUnreachable(t *testing.T) {
	srv := fakeGitHub(t, releaseJSON("v99.0.0"))
	srv.Close()
	out, stderr, code := runCLI(t, "", "version", "--check")
	if code != exitFailure || out != versionLines ||
		!strings.HasPrefix(stderr, "sdo: could not check for a newer release: the request failed: ") ||
		strings.Contains(stderr, "http://") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, stderr)
	}
}

// The production endpoint is GitHub's API and nothing else; tests replace it
// (TestMain sets a dead address so a forgotten override cannot reach GitHub).
func TestVersionCheckDefaultEndpoint(t *testing.T) {
	if defaultGitHubAPI != "https://api.github.com" {
		t.Errorf("defaultGitHubAPI = %q", defaultGitHubAPI)
	}
	if githubAPI == defaultGitHubAPI {
		t.Error("tests would reach the real GitHub API")
	}
	if versionCheckTimeout != 5*time.Second {
		t.Errorf("versionCheckTimeout = %v, want 5s", versionCheckTimeout)
	}
}

// Without --check, version contacts nothing.
func TestVersionWithoutCheckContactsNothing(t *testing.T) {
	called := false
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) { called = true })
	out, _, code := runCLI(t, "", "version")
	if called || code != exitOK || out != versionLines {
		t.Errorf("called %v, exit %d, stdout %q", called, code, out)
	}
}

func TestReportRelease(t *testing.T) {
	for _, tc := range []struct {
		installed, latest, want string
	}{
		{"0.2.0", "0.3.0", "latest release: v0.3.0 (installed: 0.2.0)\n"},
		{"0.3.0-rc.1", "0.3.0", "latest release: v0.3.0 (installed: 0.3.0-rc.1)\n"},
		{"0.3.0", "0.3.0", "latest release: v0.3.0 (up to date)\n"},
		{"0.3.0-rc.1", "0.2.0", "latest release: v0.2.0 (installed 0.3.0-rc.1 is newer)\n"},
		{"0.10.0", "0.9.0", "latest release: v0.9.0 (installed 0.10.0 is newer)\n"},
	} {
		latest, ok := parseVersion(tc.latest)
		if !ok {
			t.Fatalf("parseVersion(%q) failed", tc.latest)
		}
		var b strings.Builder
		if err := reportRelease(&b, tc.installed, latest); err != nil {
			t.Fatalf("%s vs %s: %v", tc.installed, tc.latest, err)
		}
		if got, _, _ := strings.Cut(b.String(), "upgrade:"); got != tc.want {
			t.Errorf("%s vs %s: %q, want %q", tc.installed, tc.latest, got, tc.want)
		}
	}
	latest, _ := parseVersion("1.0.0")
	if err := reportRelease(&strings.Builder{}, "dev", latest); err == nil ||
		err.Error() != `the installed version "dev" is not a release version` {
		t.Errorf("unparseable installed version: %v", err)
	}
}

func TestParseVersion(t *testing.T) {
	for _, tc := range []struct {
		in   string
		ok   bool
		want semver
	}{
		{"0.2.0", true, semver{0, 2, 0, nil}},
		{"10.20.30", true, semver{10, 20, 30, nil}},
		{"0.3.0-rc.1", true, semver{0, 3, 0, []string{"rc", "1"}}},
		{"1.0.0-alpha-1.x", true, semver{1, 0, 0, []string{"alpha-1", "x"}}},
		{"1.0.0-0", true, semver{1, 0, 0, []string{"0"}}},
		{"", false, semver{}},
		{"1", false, semver{}},
		{"1.2", false, semver{}},
		{"1.2.3.4", false, semver{}},
		{"v1.2.3", false, semver{}},
		{"01.2.3", false, semver{}},
		{"1.02.3", false, semver{}},
		{"1.2.03", false, semver{}},
		{"1.2.x", false, semver{}},
		{"1.-2.3", false, semver{}},
		{"+1.2.3", false, semver{}},
		{"1.2.3-", false, semver{}},
		{"1.2.3-rc..1", false, semver{}},
		{"1.2.3-rc.01", false, semver{}},
		{"1.2.3-rc_1", false, semver{}},
		{"1.2.3+build", false, semver{}},
		{"1.2.3 ", false, semver{}},
		{"18446744073709551616.0.0", false, semver{}},
		{"1.0.0-18446744073709551616", false, semver{}},
	} {
		got, ok := parseVersion(tc.in)
		if ok != tc.ok || (ok && fmt.Sprint(got) != fmt.Sprint(tc.want)) {
			t.Errorf("parseVersion(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	// Each list is in strictly ascending order (the SemVer 2.0.0 examples plus
	// the cases this CLI meets: 0.10.0 after 0.9.0, rc before final).
	ascending := []string{
		"0.1.0", "0.2.0", "0.9.0", "0.9.1", "0.10.0", "0.10.1",
		"1.0.0-0", "1.0.0-1", "1.0.0-2", "1.0.0-10", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta",
		"1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0-rc.2", "1.0.0-rc.10", "1.0.0",
		"1.0.1", "1.1.0", "2.0.0", "10.0.0",
	}
	vs := make([]semver, len(ascending))
	for i, s := range ascending {
		v, ok := parseVersion(s)
		if !ok {
			t.Fatalf("parseVersion(%q) failed", s)
		}
		vs[i] = v
	}
	for i := range vs {
		for j := range vs {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := compareVersions(vs[i], vs[j]); got != want {
				t.Errorf("compare(%s, %s) = %d, want %d", ascending[i], ascending[j], got, want)
			}
		}
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("stdout closed") }

// A failed write of the version lines is the command's error, and the check
// is not attempted after it.
func TestVersionWriteFailure(t *testing.T) {
	called := false
	fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) { called = true })
	for _, args := range [][]string{{"version"}, {"version", "--check"}} {
		root := newApp().rootCmd()
		root.SetOut(failingWriter{})
		root.SetErr(&strings.Builder{})
		root.SetArgs(args)
		if err := root.Execute(); err == nil || err.Error() != "stdout closed" {
			t.Errorf("%v: err = %v", args, err)
		}
	}
	if called {
		t.Error("checked after the version lines could not be written")
	}
}

// An endpoint that is not a URL fails before anything is sent.
func TestVersionCheckBadEndpoint(t *testing.T) {
	saved := githubAPI
	githubAPI = "http://[::1"
	t.Cleanup(func() { githubAPI = saved })
	out, stderr, code := runCLI(t, "", "version", "--check")
	if code != exitFailure || out != versionLines ||
		!strings.HasPrefix(stderr, "sdo: could not check for a newer release: ") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, stderr)
	}
}
