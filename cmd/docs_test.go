// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// `sdo usage` reads /info and prints the API's fields as JSON.
func TestUsageCommand(t *testing.T) {
	var path, query string
	logGateway(t, func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"IsActive":true,"ConcurrentRequest":40,"MaxMonthlyRequest":3500000,` +
			`"RemainingConcurrentRequest":15,"RemainingMonthlyRequest":2565023}`))
	})
	out, errOut, code := runCLI(t, "", "--token", "t", "usage")
	var got map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil {
		t.Fatalf("exit %d, out %q, stderr %q", code, out, errOut)
	}
	if path != "/info" || query != "token=t" || got["IsActive"] != true ||
		got["RemainingMonthlyRequest"] != float64(2565023) || len(got) != 5 {
		t.Errorf("path %q query %q out %v", path, query, got)
	}
}

// The /info rate limit is reported once, not retried into a longer lockout.
func TestUsageCommandRateLimited(t *testing.T) {
	g := logGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"message":"You have exceeded your request limit. You can send 10 requests per minute."}`))
	})
	_, errOut, code := runCLI(t, "", "--token", "t", "usage")
	if code == 0 || g.count() != 1 || !strings.Contains(errOut, "10 requests per minute") {
		t.Errorf("exit %d, requests %d, stderr %q", code, g.count(), errOut)
	}
}

// `sdo plugin` prints status and cost on stderr, like `sdo scrape`.
func TestPluginPrintsStatusAndCost(t *testing.T) {
	logGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Scrape.do-Request-Cost", "10")
		w.Write([]byte(`{"organic":[1]}`))
	})
	out, errOut, code := runCLI(t, "", "--token", "t", "plugin", "google/search", "--param", "q=x", "--param", "gl=us")
	if code != 0 || errOut != "status=200 cost=10\n" || !strings.Contains(out, `"organic": [`) {
		t.Errorf("exit %d, out %q, stderr %q", code, out, errOut)
	}
}

// The Amazon raw-HTML plugin is reachable as the route "amazon".
func TestPluginAmazonRawHTML(t *testing.T) {
	var path string
	logGateway(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html></html>"))
	})
	out, _, code := runCLI(t, "", "--token", "t", "plugin", "amazon", "--param", "url=https://www.amazon.com/dp/B0")
	if code != 0 || path != "/plugin/amazon/" || out != "<html></html>" {
		t.Errorf("exit %d, path %q, out %q", code, path, out)
	}
}

// With --disable-redirection the target's Location is in the metadata line.
func TestScrapePrintsRedirectLocation(t *testing.T) {
	logGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Scrape.do-Target-Redirected-Location", "https://e.com/next")
		w.WriteHeader(http.StatusFound)
	})
	_, errOut, code := runCLI(t, "", "--token", "t", "scrape", "https://e.com", "--disable-redirection")
	if code != 0 || !strings.Contains(errOut, "status=302") || !strings.Contains(errOut, "location=https://e.com/next") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

// A dependency the SDK enforces reads in flag names.
func TestDependencyErrorsNameFlags(t *testing.T) {
	for args, want := range map[string]string{
		"--regional-geo-code europe":                       "--regional-geo-code requires --super",
		"--render --screenshot":                            "--screenshot requires --return-json",
		"--session-id abc":                                 "--session-id must be an integer from 0 to 1000000",
		"--super --geo-code us --regional-geo-code europe": "--regional-geo-code and --geo-code cannot be combined",
	} {
		full := append([]string{"--token", "t", "scrape", "https://e.com"}, strings.Fields(args)...)
		_, errOut, code := runCLI(t, "", full...)
		if code != 2 || !strings.Contains(errOut, want) {
			t.Errorf("%s: exit %d, stderr %q, want %q", args, code, errOut, want)
		}
	}
}

// The flag help says what the documented parameters do.
func TestScrapeFlagHelp(t *testing.T) {
	usage := map[string]string{}
	for _, f := range scrapeFlags {
		usage[f.name] = f.usage
	}
	for name, want := range map[string]string{
		"pure-cookies":      "Set-Cookie header instead of Scrape.do-Cookies",
		"return-json":       "network requests",
		"session-id":        "same IP",
		"regional-geo-code": "requires --super; cannot be combined with --geo-code",
		"zip-code":          "requires --super and --geo-code",
		"screenshot":        "requires --render and --return-json",
	} {
		if !strings.Contains(usage[name], want) {
			t.Errorf("--%s: %q lacks %q", name, usage[name], want)
		}
	}
	if strings.Contains(usage["regional-geo-code"], "overrides") {
		t.Error("--regional-geo-code does not override --geo-code: the two are mutually exclusive")
	}
}
