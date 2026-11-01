// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type captured struct {
	method, contentType, trace, body string
	query                            map[string][]string
}

func captureGateway(t *testing.T) *captured {
	t.Helper()
	c := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.method, c.contentType, c.trace, c.body = r.Method, r.Header.Get("Content-Type"), r.Header.Get("X-Trace"), string(b)
		c.query = r.URL.Query()
		w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_BASE_URL", srv.URL)
	return c
}

// --method, --data, --data-json and -H shape the request the gateway sends to
// the target.
func TestScrapeRequestFlags(t *testing.T) {
	c := captureGateway(t)
	cases := []struct {
		args                      []string
		method, contentType, body string
		trace                     string
	}{
		{nil, "GET", "", "", ""},
		{[]string{"--data", "a=1&b=2"}, "POST", "", "a=1&b=2", ""},
		{[]string{"--method", "put", "--data-json", `{"b":1,"a":12345678901234567890}`}, "PUT", "application/json",
			`{"a":12345678901234567890,"b":1}`, ""},
		{[]string{"-H", "X-Trace:  7 ", "--header", "Content-Type: text/plain", "--data", "x", "--custom-headers"},
			"POST", "text/plain", "x", "7"},
		{[]string{"--method", "DELETE"}, "DELETE", "", "", ""},
	}
	for _, tc := range cases {
		args := append([]string{"--token", "t", "scrape", "https://e.com"}, tc.args...)
		if _, errOut, code := runCLI(t, "", args...); code != 0 {
			t.Fatalf("%v: exit %d: %s", tc.args, code, errOut)
		}
		if c.method != tc.method || c.contentType != tc.contentType || c.body != tc.body || c.trace != tc.trace {
			t.Errorf("%v: sent %s %q %q X-Trace=%q", tc.args, c.method, c.contentType, c.body, c.trace)
		}
		if want := tc.trace != ""; (strings.Join(c.query["customHeaders"], "") == "true") != want {
			t.Errorf("%v: customHeaders = %v", tc.args, c.query["customHeaders"])
		}
	}
}

func TestScrapeRequestFlagErrors(t *testing.T) {
	captureGateway(t)
	cases := map[string][]string{
		"mutually exclusive":          {"--data", "x", "--data-json", "{}"},
		"--data-json: not valid JSON": {"--data-json", "{"},
		"unexpected data":             {"--data-json", "1 2"},
		"null is not a request body":  {"--data-json", "null"},
		"invalid --header":            {"-H", "no-colon"},
		"cannot carry a body":         {"--method", "GET", "--data", "x"},
		"--method must be":            {"--method", "BREW"},
	}
	for want, extra := range cases {
		args := append([]string{"--token", "t", "scrape", "https://e.com"}, extra...)
		if _, errOut, code := runCLI(t, "", args...); code != exitUsage || !strings.Contains(errOut, want) {
			t.Errorf("%v: exit %d, stderr %q, want %q", extra, code, errOut, want)
		}
	}
}

// By default `jobs result` writes the task's bytes exactly — base64 content
// decoded, binary-safe; --json prints the whole task result instead.
func TestJobsResultBinaryAndJSON(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n\x00\xff"
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SDO_ASYNC_BASE_URL", srv.URL)

	b64, _ := json.Marshal([]byte(png)) // encoding/json writes []byte as base64
	body = `{"TaskID":"t1","Status":"success","StatusCode":200,"URL":"https://e.com/?a=1&b=2","Base64EncodedContent":true,"Content":` + string(b64) + `}`
	out, errOut, code := runCLI(t, "", "--token", "t", "jobs", "result", "j1", "t1")
	if code != 0 || out != png {
		t.Errorf("exit %d, stdout %q, want the decoded bytes", code, out)
	}
	if !strings.Contains(errOut, "state=success") {
		t.Errorf("stderr = %q", errOut)
	}

	out, _, code = runCLI(t, "", "--token", "t", "jobs", "result", "j1", "t1", "--json")
	var tr map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &tr) != nil || tr["Base64EncodedContent"] != true {
		t.Errorf("--json: exit %d, stdout %q", code, out)
	}
	if !strings.Contains(out, "https://e.com/?a=1&b=2") {
		t.Errorf("--json should not HTML-escape URLs: %s", out)
	}

	body = `{"TaskID":"t1","Status":"success","Base64EncodedContent":true,"Content":"not base64!"}`
	if _, errOut, code := runCLI(t, "", "--token", "t", "jobs", "result", "j1", "t1"); code != exitFailure ||
		!strings.Contains(errOut, "base64") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

func TestPrintJSONReportsUnencodableValues(t *testing.T) {
	if err := newApp().printJSON(io.Discard, func() {}); err == nil {
		t.Error("a func value should fail to encode")
	}
}

// Job and task ids are validated by the SDK before any request.
func TestJobsRejectsMalformedIDs(t *testing.T) {
	c := captureGateway(t)
	t.Setenv("SDO_ASYNC_BASE_URL", "http://127.0.0.1:1")
	if _, errOut, code := runCLI(t, "", "--token", "t", "jobs", "status", "../me"); code != exitUsage ||
		!strings.Contains(errOut, "invalid") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	if c.method != "" {
		t.Error("a malformed id reached the wire")
	}
}
