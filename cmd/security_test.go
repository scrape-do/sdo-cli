// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/scrape-do/sdo-cli/internal/version"
)

const echoedToken = "SECRETtoken/+42&x" // needs percent-encoding on the wire

// The CLI, and everything it runs, identifies as sdo-cli/<version> — never as
// the Go SDK it is built on.
func TestUserAgentIsTheCLIs(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_BASE_URL", srv.URL)
	t.Setenv("SDO_ASYNC_BASE_URL", srv.URL)

	runCLI(t, "", "--token", "t", "scrape", "https://e.com")
	runCLI(t, "", "--token", "t", "plugin", "google/search")
	runCLI(t, "", "--token", "t", "account")
	mcpCall := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"scrape","arguments":{"url":"https://e.com"}}}` + "\n"
	runCLI(t, mcpCall, "--token", "t", "mcp", "serve")

	if len(got) != 4 {
		t.Fatalf("requests = %d, want 4", len(got))
	}
	for i, ua := range got {
		if ua != "sdo-cli/"+version.CLI {
			t.Errorf("request %d: User-Agent = %q, want exactly sdo-cli/%s", i, ua, version.CLI)
		}
	}
}

// echoGateway echoes the token back in every shape a gateway could: inside a
// JSON body, as an X-Token header line, and in prose — on success and error,
// for the sync and the async API.
func echoGateway(t *testing.T, status int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.URL.Query().Get("token") + r.Header.Get("X-Token")
		prose := "Invalid token " + tok + " (X-Token: " + tok + ")"
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/jobs/j1/t1"):
			w.Write([]byte(`{"TaskID":"t1","Status":"success","ErrorMessage":` + quote(prose) +
				`,"URL":` + quote("https://e.com/?token="+tok) + `,"Content":` + quote(prose) + `}`))
		case strings.HasPrefix(r.URL.Path, "/api/v1/"):
			w.Write([]byte(`{"Error":` + quote(prose) + `,"Code":400,"JobID":"j1","Token":` + quote(tok) + `}`))
		default:
			w.Write([]byte(`{"Message":` + quote(prose) + `,"echo":{"token":` + quote(tok) +
				`,"header":` + quote("X-Token: "+tok) + `}}`))
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_BASE_URL", srv.URL)
	t.Setenv("SDO_ASYNC_BASE_URL", srv.URL)
}

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }

var everyNetworkCommand = [][]string{
	{"scrape", "https://e.com"},
	{"plugin", "google/search"},
	{"jobs", "create", "https://e.com"},
	{"jobs", "status", "j1"},
	{"jobs", "result", "j1", "t1"},
	{"jobs", "result", "j1", "t1", "--json"},
	{"jobs", "list"},
	{"account"},
}

// No output path — stdout, stderr, success or failure — prints the token.
func TestTokenNeverPrinted(t *testing.T) {
	for _, status := range []int{200, 400, 500} {
		echoGateway(t, status)
		for _, args := range everyNetworkCommand {
			out, errOut, _ := runCLI(t, "", append([]string{"--token", echoedToken}, args...)...)
			for _, form := range []string{echoedToken, percentEncode(echoedToken)} {
				if strings.Contains(out+errOut, form) {
					t.Errorf("status %d %v leaked %q:\nstdout %q\nstderr %q", status, args, form, out, errOut)
				}
			}
		}
	}
}

// Structured output stays valid JSON after redaction, even where a credential
// shape sits right before a closing quote.
func TestRedactedJSONOutputStaysValid(t *testing.T) {
	echoGateway(t, 200)
	for _, args := range [][]string{{"plugin", "google/search"}, {"jobs", "result", "j1", "t1", "--json"}} {
		out, errOut, code := runCLI(t, "", append([]string{"--token", echoedToken}, args...)...)
		if code != 0 {
			t.Fatalf("%v exited %d: %s", args, code, errOut)
		}
		var v any
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			t.Errorf("%v: output is not JSON after redaction: %v\n%s", args, err, out)
		}
		if !strings.Contains(out, "***") {
			t.Errorf("%v: nothing was masked: %s", args, out)
		}
	}
}

// Before any client exists (here, a bad flag value) the token given on the
// command line is still masked in the error.
func TestTokenMaskedBeforeAClientExists(t *testing.T) {
	_, errOut, code := runCLI(t, "", "--token", echoedToken, "scrape", "https://e.com", "--timeout", echoedToken)
	if code != exitUsage || strings.Contains(errOut, echoedToken) || !strings.Contains(errOut, "***") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

// Page content is the user's data: only the literal token is masked in it, so
// a credential-shaped string that belongs to the page arrives intact.
func TestPageBodyKeepsItsOwnCredentialShapes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<a href="/next?token=pagevalue">` + r.URL.Query().Get("token") + `</a>`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_BASE_URL", srv.URL)
	out, _, _ := runCLI(t, "", "--token", echoedToken, "scrape", "https://e.com")
	if out != `<a href="/next?token=pagevalue">***</a>` {
		t.Errorf("body = %q", out)
	}
}

func TestMaskTokenFormsAndFloor(t *testing.T) {
	if got := string(maskToken([]byte("a SECRETtoken/+42&x b SECRETtoken%2F%2B42%26x c"), echoedToken)); got != "a *** b *** c" {
		t.Errorf("maskToken = %q", got)
	}
	if got := string(maskToken([]byte("short tok"), "tok")); got != "short tok" {
		t.Errorf("a token under 8 characters must not be masked: %q", got)
	}
	if got := percentEncode("aZ09-._~ /é"); got != "aZ09-._~%20%2F%C3%A9" {
		t.Errorf("percentEncode = %q", got)
	}
}

// Plain http to a non-loopback gateway is refused unless --insecure-http is
// given; the refusal names the flag. Loopback http needs no flag.
func TestInsecureHTTPFlag(t *testing.T) {
	t.Setenv("SDO_BASE_URL", "http://gateway.test")
	_, errOut, code := runCLI(t, "", "--token", "t", "scrape", "https://e.com")
	if code != exitUsage || !strings.Contains(errOut, "--insecure-http") || !strings.Contains(errOut, "plain http") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}

	a := newApp()
	a.token, a.insecureHTTP = "t", true
	if _, err := a.newClient(); err != nil {
		t.Errorf("--insecure-http should allow plain http: %v", err)
	}

	t.Setenv("SDO_BASE_URL", "")
	t.Setenv("SDO_ASYNC_BASE_URL", "http://gateway.test")
	if _, errOut, _ := runCLI(t, "", "--token", "t", "account"); !strings.Contains(errOut, "--insecure-http") {
		t.Errorf("async base: stderr %q", errOut)
	}
	t.Setenv("SDO_ASYNC_BASE_URL", "gateway.test") // not a URL: no http hint
	if _, errOut, _ := runCLI(t, "", "--token", "t", "account"); strings.Contains(errOut, "--insecure-http") {
		t.Errorf("hint shown for a non-http problem: %q", errOut)
	}
	// With the flag given, the error is passed through as is.
	a = newApp()
	a.token, a.insecureHTTP = "t", true
	if _, err := a.newClient(); err == nil || strings.Contains(err.Error(), "--insecure-http") {
		t.Errorf("err = %v", err)
	}
}

func TestTerminalSafe(t *testing.T) {
	for in, want := range map[string]string{
		"plain text\n\twith tab":   "plain text\n\twith tab",
		"héllo � ok":               "héllo � ok",
		"\x1b]0;pwned\x07title":    `\x1b]0;pwned\x07title`,
		"a\rb\x00c\x7fd":           `a\x0db\x00c\x7fd`,
		"csi \u009b2J and \u0085":  `csi \u009b2J and \u0085`,
		"raw \x9b byte, bad \xff!": `raw \x9b byte, bad \xff!`,
	} {
		if got := terminalSafe(in); got != want {
			t.Errorf("terminalSafe(%q) = %q, want %q", in, got, want)
		}
	}
}

// Text the API or a target controls (a URL, a task state, an error message)
// reaches stderr with its control characters escaped, so it cannot drive the
// terminal.
func TestAPITextOnStderrCannotDriveTheTerminal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"TaskID":"t1","URL":"https://a.com/\u001b]0;pwned\u0007","Status":"error",` +
			`"StatusCode":500,"ErrorMessage":"boom\u001b[2J\u009b31m"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_ASYNC_BASE_URL", srv.URL)
	_, errOut, code := runCLI(t, "", "--token", "t", "jobs", "result", "job-1", "t1")
	if code != exitFailure {
		t.Errorf("exit %d", code)
	}
	if strings.ContainsAny(errOut, "\x1b\x07\u009b") {
		t.Errorf("a control character reached stderr: %q", errOut)
	}
	if !strings.Contains(errOut, `https://a.com/\x1b]0;pwned\x07`) || !strings.Contains(errOut, `boom\x1b[2J\u009b31m`) {
		t.Errorf("escaped text missing from stderr: %q", errOut)
	}
}

// runOnTerminal runs the CLI with stdout as a file the app takes for a
// terminal, and returns what was written to it.
func runOnTerminal(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "tty")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	a := newApp()
	a.isTerminal = func(fd int) bool { return fd == int(f.Fd()) }
	var errBuf bytes.Buffer
	code = a.run(args, strings.NewReader(""), f, &errBuf)
	out, _ := os.ReadFile(f.Name())
	return string(out), errBuf.String(), code
}

// A body with NUL or ESC bytes is not written to a terminal unless --raw is
// given, as curl does; text bodies and non-terminal stdout are unaffected.
func TestBinaryBodyIsNotWrittenToATerminal(t *testing.T) {
	const binary = "\x1b]0;pwned\x07\x00PNG"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/"):
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"TaskID":"t1","Status":"success","StatusCode":200,"Content":"\u001b]0;pwned\u0007\u0000PNG"}`))
		case r.URL.Query().Get("url") == "https://e.com/text":
			w.Write([]byte("plain page"))
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte(binary))
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_BASE_URL", srv.URL)
	t.Setenv("SDO_ASYNC_BASE_URL", srv.URL)
	for _, args := range [][]string{
		{"scrape", "https://e.com"},
		{"plugin", "google/shopping"},
		{"jobs", "result", "job-1", "t1"},
	} {
		args = append([]string{"--token", "t"}, args...)
		out, errOut, code := runOnTerminal(t, args...)
		if code != exitFailure || out != "" || !strings.Contains(errOut, "binary output can mess up your terminal") ||
			!strings.Contains(errOut, "--raw") {
			t.Errorf("%v on a terminal: exit %d, stdout %q, stderr %q", args, code, out, errOut)
		}
		if out, errOut, code := runOnTerminal(t, append(args, "--raw")...); code != 0 || out != binary {
			t.Errorf("%v --raw: exit %d, stdout %q, stderr %q", args, code, out, errOut)
		}
		if out, _, code := runCLI(t, "", args...); code != 0 || out != binary {
			t.Errorf("%v to a pipe: exit %d, stdout %q", args, code, out)
		}
	}
	if out, errOut, code := runOnTerminal(t, "--token", "t", "scrape", "https://e.com/text"); code != 0 || out != "plain page" {
		t.Errorf("text body on a terminal: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}
