// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	sdo "github.com/scrape-do/sdo-go"
)

// gatewayLog records every request a test gateway receives.
type gatewayLog struct {
	mu      sync.Mutex
	queries []string
	xTokens []string
}

func (g *gatewayLog) count() int { g.mu.Lock(); defer g.mu.Unlock(); return len(g.queries) }

func (g *gatewayLog) last() url.Values {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.queries) == 0 {
		return nil
	}
	v, _ := url.ParseQuery(g.queries[len(g.queries)-1])
	return v
}

// logGateway serves both base URLs, records each request and answers with
// reply (or "ok").
func logGateway(t *testing.T, reply http.HandlerFunc) *gatewayLog {
	t.Helper()
	g := &gatewayLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.queries = append(g.queries, r.URL.RawQuery)
		g.xTokens = append(g.xTokens, r.Header.Get("X-Token"))
		g.mu.Unlock()
		if reply != nil {
			reply(w, r)
			return
		}
		w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_BASE_URL", srv.URL)
	t.Setenv("SDO_ASYNC_BASE_URL", srv.URL)
	return g
}

// Every ScrapeConfig field is reachable from `sdo scrape`: through a typed
// flag, or explicitly through something else. A field added to the SDK fails
// here until the CLI decides how to expose it.
func TestEveryScrapeConfigFieldHasAFlag(t *testing.T) {
	elsewhere := map[string]string{"URL": "the argument", "Headers": "-H/--header", "ExtraParams": "--param"}
	flagged := map[string]bool{}
	names := map[string]bool{}
	for _, f := range scrapeFlags {
		flagged[f.field] = true
		if names[f.name] {
			t.Errorf("flag --%s is defined twice", f.name)
		}
		names[f.name] = true
		if _, ok := reflect.TypeOf(sdo.ScrapeConfig{}).FieldByName(f.field); !ok {
			t.Errorf("--%s names a field ScrapeConfig does not have: %s", f.name, f.field)
		}
	}
	typ := reflect.TypeOf(sdo.ScrapeConfig{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if !flagged[name] && elsewhere[name] == "" && typ.Field(i).IsExported() {
			t.Errorf("ScrapeConfig.%s has no sdo scrape flag and no documented alternative", name)
		}
	}
}

// sampleValue is a valid value for each typed flag.
func sampleValue(f scrapeFlag) string {
	samples := map[string]string{
		"render-mode": "true", "device": "mobile", "output": "markdown", "wait-until": "load",
		"timeout": "10000", "retry-timeout": "10000", "custom-wait": "1000",
		"play-with-browser": `[{"Action":"Click","Selector":"#a","n":12345678901234567890}]`,
		"regional-geo-code": "europe", "zip-code": "90210", "session-id": "1234",
	}
	if v, ok := samples[f.name]; ok {
		return v
	}
	switch f.kind {
	case boolFlag:
		return "true"
	case intFlag:
		return "640"
	}
	return "v-" + f.name
}

// flagCompanions are the flags a scrape flag requires (the SDK refuses it
// without them, as the gateway does).
var flagCompanions = map[string][]string{
	"regional-geo-code":       {"--super"},
	"zip-code":                {"--super", "--geo-code", "us"},
	"show-websocket-requests": {"--return-json"},
	"show-frames":             {"--return-json"},
	"screenshot":              {"--return-json"},
	"full-screenshot":         {"--return-json"},
	"particular-screenshot":   {"--return-json"},
}

// Each typed flag reaches the gateway as its own parameter. (A flag that was
// silently dropped on the way to the SDK would fail here.)
func TestEveryScrapeFlagReachesTheWire(t *testing.T) {
	g := logGateway(t, nil)
	for _, f := range scrapeFlags {
		args := []string{"--token", "t", "scrape", "https://e.com", "--" + f.name + "=" + sampleValue(f)}
		if f.wire != "render" && f.wire != "retryTimeout" {
			args = append(args, "--render") // render-only parameters need it; the rest ignore it
		}
		// The documented companions a parameter cannot go without.
		args = append(args, flagCompanions[f.name]...)
		if _, errOut, code := runCLI(t, "", args...); code != 0 {
			t.Errorf("--%s: exit %d, stderr %q", f.name, code, errOut)
			continue
		}
		got := g.last().Get(f.wire)
		if got == "" {
			t.Errorf("--%s did not send %s", f.name, f.wire)
		}
		if f.name == "play-with-browser" && got != `[{"Action":"Click","Selector":"#a","n":12345678901234567890}]` {
			t.Errorf("--play-with-browser lost precision: %s", got)
		}
	}
}

// --param is the escape hatch for gateway parameters without a flag. It
// cannot set the token or the URL, cannot shadow a flag, and a key is given
// once.
func TestScrapeParamEscapeHatch(t *testing.T) {
	g := logGateway(t, nil)
	if _, errOut, code := runCLI(t, "", "--token", "t", "scrape", "https://e.com",
		"--param", "newThing=1", "--param", "Other=a=b"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if q := g.last(); q.Get("newThing") != "1" || q.Get("Other") != "a=b" {
		t.Errorf("query = %v", q)
	}
	sent := g.count()
	cases := map[string][]string{
		"the token comes from":   {"--param", "TOKEN=x"},
		"the target URL is":      {"--param", "url=https://evil.example"},
		"use --wait-selector":    {"--param", "waitselector=#x"},
		"use --render":           {"--param", "render=true"},
		"given more than once":   {"--param", "a=1", "--param", "a=2"},
		"expected key=value":     {"--param", "novalue"},
		"invalid --param \"=x\"": {"--param", "=x"},
	}
	for want, extra := range cases {
		args := append([]string{"--token", "t", "scrape", "https://e.com"}, extra...)
		if _, errOut, code := runCLI(t, "", args...); code != exitUsage || !strings.Contains(errOut, want) {
			t.Errorf("%v: exit %d, stderr %q, want %q", extra, code, errOut, want)
		}
	}
	if g.count() != sent {
		t.Error("a rejected --param reached the gateway")
	}
}

// A header the HTTP layer would reject is a configuration error before
// anything is sent (exit 2, no request, no retries), and its value is never
// echoed: it may be a credential.
func TestInvalidHeadersAreRejectedBeforeSending(t *testing.T) {
	g := logGateway(t, nil)
	cases := map[string]string{
		"X-A: se\ncret":          "printable ASCII",
		"X-A: se\rcret":          "printable ASCII",
		"X-A: se\x00cret":        "printable ASCII",
		"X-A: sé":                "printable ASCII",
		"Bad Name: secret":       "header name",
		"X(A): secret":           "header name",
		"Host: evil.example":     "set by the HTTP client",
		"content-length: 5":      "set by the HTTP client",
		"Transfer-Encoding: x":   "set by the HTTP client",
		"Connection: keep-alive": "set by the HTTP client",
	}
	for header, want := range cases {
		_, errOut, code := runCLI(t, "", "--token", "t", "scrape", "https://e.com", "-H", header)
		if code != exitUsage || !strings.Contains(errOut, want) || !strings.Contains(errOut, "--header") {
			t.Errorf("-H %q: exit %d, stderr %q, want %q", header, code, errOut, want)
		}
		if strings.Contains(errOut, "cret") {
			t.Errorf("-H %q: the value was echoed: %q", header, errOut)
		}
	}
	if g.count() != 0 {
		t.Errorf("%d requests reached the gateway", g.count())
	}
	if _, errOut, code := runCLI(t, "", "--token", "t", "scrape", "https://e.com",
		"-H", "X-Ok!#$%&'*+-.^_`|~: a\tb ~"); code != 0 {
		t.Errorf("a valid header was rejected: %q", errOut)
	}
}

func TestJSONFlagsAreChecked(t *testing.T) {
	logGateway(t, nil)
	cases := map[string][]string{
		"unexpected JSON object; want a JSON array": {"--render", "--play-with-browser", `{"a":1}`},
		"--play-with-browser: not valid JSON":       {"--render", "--play-with-browser", `[`},
		"unexpected data after the value":           {"--render", "--play-with-browser", `[] []`},
	}
	for want, extra := range cases {
		args := append([]string{"--token", "t", "scrape", "https://e.com"}, extra...)
		if _, errOut, code := runCLI(t, "", args...); code != exitUsage || !strings.Contains(errOut, want) {
			t.Errorf("%v: exit %d, stderr %q, want %q", extra, code, errOut, want)
		}
	}
}

// --output-file writes the file owner-only, like the token file, also when it
// replaces an existing wider file. A path that cannot be written is a failure.
func TestOutputFileIsCreatedOwnerOnly(t *testing.T) {
	logGateway(t, nil)
	dir := t.TempDir()
	fresh := filepath.Join(dir, "fresh.html")
	if _, errOut, code := runCLI(t, "", "--token", "t", "scrape", "https://e.com", "--output-file", fresh); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	info, err := os.Stat(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("new output file mode = %04o, want 0600", info.Mode().Perm())
	}

	existing := filepath.Join(dir, "existing.html")
	os.WriteFile(existing, []byte("a much longer previous body"), 0o644)
	runCLI(t, "", "--token", "t", "scrape", "https://e.com", "--output-file", existing)
	got, _ := os.ReadFile(existing)
	info, _ = os.Stat(existing)
	if string(got) != "ok" || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Errorf("existing file: %q mode %04o, want \"ok\" mode 0600", got, info.Mode().Perm())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("temporary files left behind: %v", entries)
	}

	if _, _, code := runCLI(t, "", "--token", "t", "scrape", "https://e.com",
		"--output-file", filepath.Join(dir, "missing", "x.html")); code != exitFailure {
		t.Errorf("unwritable path: exit %d", code)
	}
}

// An absent cost header is unknown, not free: the metadata line leaves it out.
func TestScrapeMetadataOmitsAnUnknownCost(t *testing.T) {
	logGateway(t, nil)
	_, errOut, _ := runCLI(t, "", "--token", "t", "scrape", "https://e.com")
	if !strings.HasPrefix(errOut, "status=200 url=") || strings.Contains(errOut, "cost") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestWriteOutputFileReportsCloseAndWriteErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no /dev/full")
	}
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("no /dev/full")
	}
	if err := writeOutputFile("/dev/full", []byte("x")); err == nil {
		t.Error("a failed write was not reported")
	}
}

// --output-file never follows a symlink (the body would land wherever the link
// points) and never replaces anything but a regular file.
func TestOutputFileRefusesSymlinksAndSpecialFiles(t *testing.T) {
	logGateway(t, nil)
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.html")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	_, errOut, code := runCLI(t, "", "--token", "t", "scrape", "https://e.com", "--output-file", link)
	if code != exitFailure || !strings.Contains(errOut, "symlink") {
		t.Errorf("symlink: exit %d, stderr %q", code, errOut)
	}
	if got, _ := os.ReadFile(target); string(got) != "untouched" {
		t.Errorf("the body was written through the link: %q", got)
	}
	_, errOut, code = runCLI(t, "", "--token", "t", "scrape", "https://e.com", "--output-file", dir)
	if code != exitFailure || !strings.Contains(errOut, "not a regular file") {
		t.Errorf("directory: exit %d, stderr %q", code, errOut)
	}
	// A path that cannot be looked up (its parent is a file) is reported as
	// it is, not taken for a file to create. The operating system words the
	// error: ENOTDIR on Unix, "cannot find the path" on Windows.
	wantErr := "not a directory"
	if runtime.GOOS == "windows" {
		wantErr = "cannot find the path"
	}
	_, errOut, code = runCLI(t, "", "--token", "t", "scrape", "https://e.com", "--output-file", filepath.Join(target, "x.html"))
	if code != exitFailure || !strings.Contains(errOut, wantErr) {
		t.Errorf("path under a file: exit %d, stderr %q", code, errOut)
	}
}
