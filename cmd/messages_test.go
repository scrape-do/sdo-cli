// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdo "github.com/scrape-do/sdo-go"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// goIdentifier is what a CLI user cannot act on: a Go option, a qualified
// field, the SDK's render advice or its constructor.
var goIdentifier = regexp.MustCompile(`\bWith[A-Z]|ScrapeConfig|Request\.|Render: true|RenderMode|New\(token\)|CustomWait`)

// Messages name flags and environment variables, never Go SDK identifiers.
func TestMessagesNameFlagsNotGoIdentifiers(t *testing.T) {
	logGateway(t, func(w http.ResponseWriter, r *http.Request) { w.Write(make([]byte, 2000)) })
	cases := []struct {
		args []string
		want string
		code int
	}{
		{[]string{"scrape", "example.com"}, "sdo: the target URL must be an absolute http or https URL", exitUsage},
		{[]string{"scrape", "https://e.com", "--custom-wait", "1000"}, "--custom-wait only applies to rendered requests: pass --render", exitUsage},
		{[]string{"scrape", "https://e.com", "--render", "--render-mode", "false"}, `--render conflicts with --render-mode "false"`, exitUsage},
		{[]string{"scrape", "https://e.com", "--device", "phone"}, "--device must be one of", exitUsage},
		{[]string{"scrape", "https://e.com", "--timeout", "30"}, "--timeout must be between 5000 and 120000", exitUsage},
		{[]string{"scrape", "https://e.com", "--method", "TRACE"}, "--method must be one of", exitUsage},
		{[]string{"scrape", "https://e.com", "--max-response-bytes", "1000"}, "--max-response-bytes", exitFailure},
	}
	for _, c := range cases {
		_, errOut, code := runCLI(t, "", append([]string{"--token", "t"}, c.args...)...)
		if code != c.code || !strings.Contains(errOut, c.want) {
			t.Errorf("%v: exit %d, stderr %q, want %q", c.args, code, errOut, c.want)
		}
		if goIdentifier.MatchString(errOut) {
			t.Errorf("%v: names a Go identifier: %q", c.args, errOut)
		}
	}
}

// The plain-http hint is added only to the plain-http refusal, and once.
func TestInsecureHintOnlyForPlainHTTP(t *testing.T) {
	t.Setenv("SDO_BASE_URL", "http://gateway.test")
	_, errOut, _ := runCLI(t, "", "--token", "t", "scrape", "https://e.com")
	if strings.Count(errOut, "--insecure-http") != 1 || goIdentifier.MatchString(errOut) {
		t.Errorf("stderr = %q", errOut)
	}
	t.Setenv("SDO_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("SDO_TIMEOUT", "abc")
	_, errOut, code := runCLI(t, "", "--token", "t", "scrape", "https://e.com")
	if code != exitUsage || !strings.Contains(errOut, "SDO_TIMEOUT") || strings.Contains(errOut, "--insecure-http") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

// --client-timeout is the client's own deadline, in seconds; it wins over
// SDO_TIMEOUT, and a timeout names it.
func TestClientTimeoutFlag(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	logGateway(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	t.Setenv("SDO_TIMEOUT", "abc") // ignored: the flag wins
	start := time.Now()
	_, errOut, code := runCLI(t, "", "--token", "t", "--client-timeout", "0.05", "scrape", "https://e.com")
	if code != exitFailure || !strings.Contains(errOut, "timed out") ||
		!strings.Contains(errOut, "--client-timeout or SDO_TIMEOUT") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("took %v", time.Since(start))
	}
}

// A request that was sent is not retried after the client times out, and a
// gateway --timeout stretches the client's wait to cover it (plus 10s), so a
// short --client-timeout cannot abandon a scrape the gateway is running.
func TestTimeoutsDoNotMultiplyGatewayRequests(t *testing.T) {
	var delay atomic.Int64 // a timed-out request's handler may still be reading it
	g := logGateway(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(time.Duration(delay.Load())):
			w.Write([]byte("slow page"))
		case <-r.Context().Done():
		}
	})
	delay.Store(int64(5 * time.Second))
	if _, _, code := runCLI(t, "", "--token", "t", "--client-timeout", "0.05", "scrape", "https://e.com"); code != exitFailure {
		t.Errorf("exit %d", code)
	}
	if g.count() != 1 {
		t.Errorf("a timed-out scrape reached the gateway %d times, want 1", g.count())
	}
	delay.Store(int64(200 * time.Millisecond))
	out, errOut, code := runCLI(t, "", "--token", "t", "--client-timeout", "0.05", "scrape", "https://e.com", "--timeout", "5000")
	if code != 0 || out != "slow page" {
		t.Errorf("with --timeout: exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestGlobalFlagValuesAreChecked(t *testing.T) {
	for _, bad := range [][]string{
		{"--client-timeout", "0"}, {"--client-timeout", "0.0001"}, {"--client-timeout", "-1"},
		{"--client-timeout", "abc"}, {"--client-timeout", "1e3"}, {"--client-timeout", "0x10"},
		{"--client-timeout", "1_000"}, {"--max-response-bytes", "0"}, {"--max-response-bytes", "-5"},
		{"--max-response-bytes", "1MiB"},
	} {
		_, errOut, code := runCLI(t, "", append(bad, "--token", "t", "scrape", "https://e.com")...)
		if code != exitUsage || !strings.Contains(errOut, bad[0]) {
			t.Errorf("%v: exit %d, stderr %q", bad, code, errOut)
		}
	}
	var d time.Duration
	v := &secondsValue{&d}
	if err := v.Set("99999999999999999999999"); err != nil || d <= 0 {
		t.Errorf("a huge timeout should clamp, got %v %v", d, err)
	}
	if err := v.Set("2.5"); err != nil || v.String() != "2.5" || v.Type() != "seconds" {
		t.Errorf("2.5 -> %v %q", err, v.String())
	}
	var n int64
	b := &bytesValue{&n}
	if b.String() != "" || b.Set("64") != nil || b.String() != "64" || b.Type() != "bytes" {
		t.Errorf("bytesValue = %q", b.String())
	}
}

// pflag shows back-quoted usage text as the flag's value name, so no flag's
// usage may contain a backquote: `--token sdo auth login` was the result.
func TestHelpShowsValueTypesNotUsageFragments(t *testing.T) {
	out, _, _ := runCLI(t, "", "--help")
	for _, want := range []string{"--token string", "--client-timeout seconds", "--max-response-bytes bytes"} {
		if !strings.Contains(out, want) {
			t.Errorf("sdo --help lacks %q:\n%s", want, out)
		}
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			if strings.Contains(f.Usage, "`") {
				t.Errorf("%s --%s: usage contains a backquote: %q", c.CommandPath(), f.Name, f.Usage)
			}
		})
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(NewRootCommand())
}

// Surrounding whitespace is trimmed from every token source (a secret file
// ends in a newline); whitespace or control characters inside are an error
// naming the source; a token that is only whitespace is no token.
func TestTokenIsTrimmedAndChecked(t *testing.T) {
	withConfigFile(t)
	g := logGateway(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	})
	t.Setenv("SDO_TOKEN", "tok_1234567890abcdef\n") // gitleaks:allow (fake test token)
	if _, errOut, code := runCLI(t, "", "account"); code != 0 || g.xTokens[0] != "tok_1234567890abcdef" {
		t.Errorf("exit %d, X-Token %q, stderr %q", code, g.xTokens, errOut)
	}
	runCLI(t, "", "scrape", "https://e.com")
	if got := g.last().Get("token"); got != "tok_1234567890abcdef" {
		t.Errorf("token= %q", got)
	}

	t.Setenv("SDO_TOKEN", "tok 1234567890")
	if _, errOut, code := runCLI(t, "", "account"); code != exitUsage ||
		!strings.Contains(errOut, "SDO_TOKEN environment variable contains whitespace") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	if _, errOut, code := runCLI(t, "", "--token", "a\x00bcdefghij", "account"); code != exitUsage ||
		!strings.Contains(errOut, "--token flag") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	t.Setenv("SDO_TOKEN", " \n")
	if _, errOut, code := runCLI(t, "", "account"); code != exitAuth || !strings.Contains(errOut, "no API token") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	if out, _, _ := runCLI(t, "", "auth", "status"); !strings.Contains(out, "token source: none") {
		t.Errorf("auth status = %q", out)
	}
}

func TestAuthLoginRejectsAMalformedToken(t *testing.T) {
	path := withConfigFile(t)
	if _, errOut, code := runCLI(t, "tok en\n", "auth", "login"); code != exitUsage || !strings.Contains(errOut, "input contains whitespace") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	if _, err := resolve(t); err != nil {
		t.Fatal(err)
	}
	if _, errOut, code := runCLI(t, "  tok_from_pipe \r\n", "auth", "login"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if got, _ := resolve(t); got != "tok_from_pipe" {
		t.Errorf("stored %q (file %s)", got, path)
	}
}

// The plain-http hint does not depend on the SDK naming its option.
func TestDescribeAddsThePlainHTTPHintOnce(t *testing.T) {
	a := newApp()
	got := a.describe(&sdo.ConfigError{Message: "SDO_BASE_URL: http://x is plain http and would send the token in cleartext"})
	if strings.Count(got, "--insecure-http") != 1 {
		t.Errorf("describe = %q", got)
	}
	got = a.describe(&sdo.ConfigError{Message: "plain http (allowed with WithAllowInsecureHTTP)"})
	if strings.Count(got, "--insecure-http") != 1 || goIdentifier.MatchString(got) {
		t.Errorf("describe = %q", got)
	}
	// The size-limit hint does not depend on the SDK's wording either.
	got = a.describe(&sdo.ResponseTooLargeError{Limit: 10})
	if strings.Count(got, "--max-response-bytes") != 1 || goIdentifier.MatchString(got) {
		t.Errorf("describe = %q", got)
	}
	got = a.describe(rewordedSizeErr{})
	if !strings.HasPrefix(got, "body too big\n") || strings.Count(got, "--max-response-bytes") != 1 {
		t.Errorf("describe = %q", got)
	}
}

// rewordedSizeErr is a size-limit failure in words that name no option.
type rewordedSizeErr struct{}

func (rewordedSizeErr) Error() string { return "body too big" }
func (rewordedSizeErr) Unwrap() error { return &sdo.ResponseTooLargeError{Limit: 10} }
