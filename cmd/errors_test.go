// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// failingGateway answers every request with a gateway error envelope, for both
// the sync and the async base URL.
func failingGateway(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		if strings.HasPrefix(r.URL.Path, "/api/v1/") { // the async API's own envelope
			w.Write([]byte(`{"Error":"gateway exploded","Code":500}`))
			return
		}
		w.Write([]byte(`{"Message":["gateway exploded"]}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_BASE_URL", srv.URL)
	t.Setenv("SDO_ASYNC_BASE_URL", srv.URL)
}

// Every command that talks to the gateway must fail cleanly both before a
// request (no token) and after one (a gateway error), carrying the reason.
func TestEveryNetworkCommandReportsFailures(t *testing.T) {
	commands := [][]string{
		{"scrape", "https://e.com"},
		{"plugin", "google/shopping"},
		{"jobs", "create", "https://e.com"},
		{"jobs", "status", "job-1"},
		{"jobs", "result", "job-1", "task-1"},
		{"jobs", "list"},
		{"jobs", "cancel", "job-1"},
		{"account"},
		{"usage"},
	}
	t.Run("no token", func(t *testing.T) {
		t.Setenv("SDO_TOKEN", "")
		for _, args := range commands {
			_, _, err := execute(t, args...)
			if err == nil || !strings.Contains(err.Error(), "no API token") {
				t.Errorf("%v: want a no-token error, got %v", args, err)
			}
		}
	})
	t.Run("gateway error", func(t *testing.T) {
		failingGateway(t)
		for _, args := range commands {
			_, _, err := execute(t, append(args, "--token", "t")...)
			if err == nil || !strings.Contains(err.Error(), "gateway exploded") {
				t.Errorf("%v: want the gateway message surfaced, got %v", args, err)
			}
		}
	})
}

// A plugin answer labelled JSON that does not parse is a failure, and
// nothing reaches stdout.
func TestPluginInvalidJSONFails(t *testing.T) {
	logGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"organic": [`))
	})
	out, errOut, code := runCLI(t, "", "--token", "t", "plugin", "google/search", "--param", "q=x")
	if code != exitFailure || out != "" || !strings.HasPrefix(errOut, "status=200") || !strings.Contains(errOut, "sdo: ") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

// failWriter is a stdout that is gone (a closed pipe).
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("stdout closed") }

// A succeeded task whose --json output cannot be written is not reported as
// a success.
func TestJobsResultJSONWriteFailureFails(t *testing.T) {
	logGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"TaskID":"t1","Status":"success","Content":"page"}`))
	})
	var errBuf bytes.Buffer
	code := newApp().run([]string{"--token", "t", "jobs", "result", "j1", "t1", "--json"}, strings.NewReader(""), failWriter{}, &errBuf)
	if code != exitFailure || !strings.Contains(errBuf.String(), "stdout closed") {
		t.Errorf("exit %d, stderr %q", code, errBuf.String())
	}
}

func TestScrapeWritesOutputFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html>page</html>"))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_BASE_URL", srv.URL)

	path := filepath.Join(t.TempDir(), "page.html")
	out, errOut, err := execute(t, "scrape", "https://e.com", "--token", "t", "--output-file", path)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "<html>page</html>" || out != "" {
		t.Errorf("file=%q stdout=%q (body belongs in the file, not stdout)", got, out)
	}
	if !strings.Contains(errOut, "status=200") {
		t.Errorf("metadata should still go to stderr, got %q", errOut)
	}
}

func TestPluginRejectsMalformedParamAndWritesRawBodies(t *testing.T) {
	if _, _, err := execute(t, "plugin", "google/shopping", "--token", "t", "--param", "novalue"); err == nil {
		t.Error("a --param without '=' was accepted")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		w.Write([]byte("a,b\n1,2\n"))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_BASE_URL", srv.URL)
	out, _, err := execute(t, "plugin", "google/shopping", "--token", "t")
	if err != nil || out != "a,b\n1,2\n" {
		t.Errorf("a non-JSON plugin body should be written verbatim: out=%q err=%v", out, err)
	}
}

// `sdo mcp serve` wires stdin/stdout to the JSON-RPC loop.
func TestMCPServeCommand(t *testing.T) {
	t.Setenv("SDO_TOKEN", "t")
	out, _, code := runCLI(t, `{"jsonrpc":"2.0","id":1,"method":"ping"}`+"\n", "mcp", "serve")
	if code != 0 {
		t.Fatalf("mcp serve exited %d", code)
	}
	if !strings.Contains(out, `"id":1`) || !strings.Contains(out, `"result"`) {
		t.Errorf("ping reply = %q", out)
	}

	t.Setenv("SDO_TOKEN", "")
	if _, _, err := execute(t, "mcp", "serve"); err == nil || !strings.Contains(err.Error(), "no API token") {
		t.Errorf("mcp serve without a token: %v", err)
	}
}

func TestJobsCreateRejectsMalformedOption(t *testing.T) {
	if _, _, err := execute(t, "jobs", "create", "https://e.com", "--token", "t", "--option", "novalue"); err == nil {
		t.Error("an --option without '=' was accepted")
	}
}

// A broken config file must stop a network command before it runs, with the
// fix in the message — not fall through to "no token".
func TestNetworkCommandReportsConfigFileProblem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte("{not json"), 0o600)
	t.Setenv("SDO_CONFIG_FILE", path)
	t.Setenv("SDO_TOKEN", "")
	_, _, err := execute(t, "scrape", "https://e.com")
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("want an error naming the config file, got %v", err)
	}
}

// The gateway's own 400 (a parameter it rejected; the CLI itself refuses
// render=mert, so the gateway's exact render=mert reply stands in) fails with
// the gateway's message; a target's 400 the gateway billed, like a target 404
// or 410, is a page: printed, with its status and cost, and exit 0. Plugins
// follow the same rules, and a plugin's {"error","message"} envelope supplies
// the message.
func TestGateway400FailsAndTargetStatusesArePages(t *testing.T) {
	logGateway(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/plugin/google/search" && r.URL.Query().Get("q") == "bad":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"invalid_zipcode","message":"ZIP code format doesn't match country requirements"}`))
		case r.URL.Path == "/plugin/google/search":
			w.WriteHeader(404)
			w.Write([]byte("no such page"))
		case r.URL.Query().Get("url") == "https://e.com/bad-param":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(400)
			w.Write([]byte(`{"URL":"https://e.com","StatusCode":400,"Message":["'Render' parameter should boolean."],` +
				`"PossibleCauses":["Please check your request."],"Contact":"Please don't hesitate to contact with us via support@scrape.do."}`))
		case r.URL.Query().Get("url") == "https://e.com/gone":
			w.WriteHeader(410)
			w.Write([]byte("gone"))
		default:
			w.Header().Set("Scrape.do-Request-Cost", "1")
			w.WriteHeader(400)
			w.Write([]byte("target says bad request"))
		}
	})

	_, errOut, code := runCLI(t, "", "--token", "t", "scrape", "https://e.com/bad-param")
	if code != exitFailure || !strings.Contains(errOut, "'Render' parameter should boolean.") {
		t.Errorf("gateway 400: exit %d, stderr %q", code, errOut)
	}
	out, errOut, code := runCLI(t, "", "--token", "t", "scrape", "https://e.com")
	if code != exitOK || out != "target says bad request" || !strings.Contains(errOut, "status=400 cost=1") {
		t.Errorf("target 400: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if out, _, code := runCLI(t, "", "--token", "t", "scrape", "https://e.com/gone"); code != exitOK || out != "gone" {
		t.Errorf("target 410: exit %d, stdout %q", code, out)
	}
	_, errOut, code = runCLI(t, "", "--token", "t", "plugin", "google/search", "--param", "q=bad")
	if code != exitFailure || !strings.Contains(errOut, "ZIP code format doesn't match country requirements") {
		t.Errorf("plugin 400: exit %d, stderr %q", code, errOut)
	}
	if out, _, code := runCLI(t, "", "--token", "t", "plugin", "google/search", "--param", "q=x"); code != exitOK ||
		out != "no such page" {
		t.Errorf("plugin 404: exit %d, stdout %q", code, out)
	}
}

// Scrape.do-Request-Cost decides: a target 401/403/422 the gateway billed is a
// page (exit 0, status and cost on stderr); the gateway's own 401 (no cost)
// exits with the auth code, and an error's cost is shown when the gateway
// reported one.
func TestRequestCostDecidesExitCodes(t *testing.T) {
	logGateway(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("url") {
		case "https://e.com/gateway-401":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(401)
			w.Write([]byte(`{"URL":"https://dashboard.scrape.do","StatusCode":401,"Message":["Monthly request limit exceeded."]}`))
		case "https://e.com/unbilled-502":
			w.Header().Set("Scrape.do-Request-Cost", "0")
			w.WriteHeader(502)
			w.Write([]byte(`{"Message":["Request failed and not charged. Please try again."]}`))
		default:
			status, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Query().Get("url"), "https://e.com/"))
			w.Header().Set("Scrape.do-Request-Cost", "2")
			w.WriteHeader(status)
			w.Write([]byte("target page"))
		}
	})
	for _, status := range []string{"401", "403", "422"} {
		out, errOut, code := runCLI(t, "", "--token", "t", "scrape", "https://e.com/"+status)
		if code != exitOK || out != "target page" || !strings.Contains(errOut, "status="+status+" cost=2") {
			t.Errorf("target %s: exit %d, stdout %q, stderr %q", status, code, out, errOut)
		}
	}
	_, errOut, code := runCLI(t, "", "--token", "t", "scrape", "https://e.com/gateway-401")
	if code != exitAuth || !strings.Contains(errOut, "Monthly request limit exceeded.") || strings.Contains(errOut, "cost=") {
		t.Errorf("gateway 401: exit %d, stderr %q", code, errOut)
	}
	_, errOut, code = runCLI(t, "", "--token", "t", "--client-timeout", "5", "scrape", "https://e.com/unbilled-502")
	if code != exitFailure || !strings.Contains(errOut, "not charged") || !strings.Contains(errOut, "cost=0") {
		t.Errorf("unbilled 502: exit %d, stderr %q", code, errOut)
	}
}
