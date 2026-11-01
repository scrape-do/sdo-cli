// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const dataToken = "tok_SECRET_1234567890" // gitleaks:allow (fake test token)

// Structured output is data: a plugin's links and fields, a task's content.
// Only the token itself is masked in it (values and keys); credential shapes
// that belong to the data reach the script unaltered.
func TestStructuredOutputKeepsThirdPartyCredentialShapes(t *testing.T) {
	logGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			w.Write([]byte(`{"TaskID":"t1","Status":"success","Content":"see https://shop.example/reset?token=abc123&x=1",` +
				`"ResponseHeaders":{"api_key":"k1","X-Token":"k2"}}`))
			return
		}
		w.Write([]byte(`{"link":"https://shop.example/reset?token=abc123&x=1","note":"use api_key=k2 to call",` +
			`"access_token":"k3","echo":"` + dataToken + `","` + dataToken + `":1}`))
	})
	out, errOut, code := runCLI(t, "", "--token", dataToken, "plugin", "google/search")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	want := map[string]any{"link": "https://shop.example/reset?token=abc123&x=1", "note": "use api_key=k2 to call",
		"access_token": "k3", "echo": "***", "***": float64(1)}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}

	out, _, code = runCLI(t, "", "--token", dataToken, "jobs", "result", "j1", "t1", "--json")
	if code != 0 || !strings.Contains(out, "reset?token=abc123&x=1") || !strings.Contains(out, `"api_key": "k1"`) ||
		!strings.Contains(out, `"X-Token": "k2"`) {
		t.Errorf("jobs result --json: exit %d\n%s", code, out)
	}
}

// Messages are the CLI's own text: errors and the metadata lines are still
// redacted by shape, whatever token is in use.
func TestMessagesAreRedactedByShape(t *testing.T) {
	logGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		w.Write([]byte(`{"Message":"rejected https://x.example/?token=abc123 and api_key=k1"}`))
	})
	_, errOut, _ := runCLI(t, "", "--token", dataToken, "scrape", "https://e.com")
	if strings.Contains(errOut, "abc123") || strings.Contains(errOut, "k1") || !strings.Contains(errOut, "token=***") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestPrintJSONMasksKeysBeforeAClientExists(t *testing.T) {
	a := newApp()
	a.token = dataToken + "\n"
	var b bytes.Buffer
	if err := a.printJSON(&b, map[string]any{dataToken: []any{dataToken, 1, true, nil}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), dataToken) || strings.Count(b.String(), "***") != 2 {
		t.Errorf("output = %s", b.String())
	}
}

// A repeated plugin --param is a list, sent as a repeated key; the token is
// not a plugin parameter.
func TestPluginParamRepeatsAKey(t *testing.T) {
	g := logGateway(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	if _, errOut, code := runCLI(t, "", "--token", "t", "plugin", "google/search",
		"--param", "tags=x", "--param", "q=1", "--param", "tags=y", "--param", "tags=z"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if q := g.last(); strings.Join(q["tags"], ",") != "x,y,z" || q.Get("q") != "1" {
		t.Errorf("query = %v", q)
	}
	for _, key := range []string{"token", "TOKEN", "Token"} {
		_, errOut, code := runCLI(t, "", "--token", "t", "plugin", "google/search", "--param", key+"=evil")
		if code != exitUsage || !strings.Contains(errOut, "the token comes from") {
			t.Errorf("%s: exit %d, stderr %q", key, code, errOut)
		}
	}
	if g.count() != 1 {
		t.Errorf("requests = %d, want 1", g.count())
	}
}
