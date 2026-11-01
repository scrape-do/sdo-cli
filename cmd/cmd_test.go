// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scrape-do/sdo-cli/internal/version"
	sdo "github.com/scrape-do/sdo-go"
)

// execute runs one command tree built for a fresh invocation and returns the
// command's error as is (Main's formatting is covered by runCLI tests).
func execute(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := newApp().rootCmd()
	var out, errBuf bytes.Buffer
	root.SetIn(strings.NewReader(""))
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errBuf.String(), err
}

// runCLI runs a whole invocation the way main does and returns the exit code.
func runCLI(t *testing.T, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = newApp().run(args, strings.NewReader(stdin), &out, &errBuf)
	return out.String(), errBuf.String(), code
}

// Both version sources are labelled, and --version is the CLI's own version,
// not the SDK's.
func TestVersionOutputs(t *testing.T) {
	out, _, err := execute(t, "version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	want := "sdo CLI " + version.CLI + "\nsdo-go SDK " + sdo.Version + "\n"
	if out != want {
		t.Errorf("version output = %q, want %q", out, want)
	}
	out, _, err = execute(t, "--version")
	if err != nil || out != "sdo version "+version.CLI+"\n" {
		t.Errorf("--version = %q (err %v), want the CLI version", out, err)
	}
	if CLIVersion != version.CLI {
		t.Errorf("CLIVersion %q drifted from version.CLI %q", CLIVersion, version.CLI)
	}
}

func TestBuildScrapeConfig(t *testing.T) {
	cmd := newScrapeCmd(newApp())
	_ = cmd.Flags().Set("render", "true")
	_ = cmd.Flags().Set("geo-code", "us")
	_ = cmd.Flags().Set("output", "markdown")
	cfg, err := buildScrapeConfig(cmd.Flags(), "https://example.com")
	if err != nil || cfg.URL != "https://example.com" || !cfg.Render || cfg.GeoCode != "us" || cfg.Output != "markdown" {
		t.Errorf("config = %+v", cfg)
	}
}

// --block-resources is tri-state: unset must stay nil (gateway default), and
// an explicit false must reach the SDK as false, not be dropped.
func TestBuildScrapeConfigTriStateBool(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want *bool
	}{
		{nil, nil},
		{[]string{"--block-resources=false"}, sdo.Bool(false)},
		{[]string{"--block-resources"}, sdo.Bool(true)},
	} {
		cmd := newScrapeCmd(newApp())
		if err := cmd.Flags().Parse(tc.args); err != nil {
			t.Fatal(err)
		}
		cfg, err := buildScrapeConfig(cmd.Flags(), "https://example.com")
		if err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		got := cfg.BlockResources
		if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
			t.Errorf("%v: BlockResources = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestScrapeCommandAgainstServer(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.Query().Get("url")
		w.Header().Set("Scrape.do-Request-Cost", "3")
		w.Write([]byte("<html>cli body</html>"))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_BASE_URL", srv.URL)

	out, errOut, err := execute(t, "scrape", "https://example.com", "--token", "t", "--render")
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	if !strings.Contains(out, "cli body") {
		t.Errorf("stdout = %q", out)
	}
	if !strings.Contains(errOut, "status=200") || !strings.Contains(errOut, "cost=3") {
		t.Errorf("stderr = %q", errOut)
	}
	if gotURL != "https://example.com" {
		t.Errorf("server saw url = %q", gotURL)
	}
}

func TestPluginCommandAgainstServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/plugin/google/shopping") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_BASE_URL", srv.URL)

	out, _, err := execute(t, "plugin", "google/shopping", "--token", "t", "--param", "q=laptop")
	if err != nil {
		t.Fatalf("plugin: %v", err)
	}
	if !strings.Contains(out, `"ok": true`) {
		t.Errorf("stdout = %q", out)
	}
}

func TestNoTokenFails(t *testing.T) {
	t.Setenv("SDO_TOKEN", "")
	_, _, err := execute(t, "scrape", "https://example.com")
	if err == nil {
		t.Fatal("expected error without a token")
	}
}

func TestParseParams(t *testing.T) {
	p, err := parseParams([]string{"a=1", "b=two"})
	if err != nil || p["a"] != "1" || p["b"] != "two" {
		t.Fatalf("parseParams: %v err=%v", p, err)
	}
	if _, err := parseParams([]string{"bad"}); err == nil {
		t.Fatal("expected error for malformed param")
	}
}

func TestNewRootCommandIsTheCLITree(t *testing.T) {
	root := NewRootCommand()
	if root.Name() != "sdo" || root.Version != version.CLI {
		t.Errorf("root = %s %s", root.Name(), root.Version)
	}
	if c, _, err := root.Find([]string{"jobs", "result"}); err != nil || c.Name() != "result" {
		t.Errorf("jobs result not found: %v", err)
	}
}
