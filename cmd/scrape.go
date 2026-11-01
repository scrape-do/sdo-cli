// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"reflect"
	"strings"

	sdo "github.com/scrape-do/sdo-go"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type flagKind int

const (
	boolFlag   flagKind = iota
	stringFlag          // a string field
	intFlag             // an int field
	jsonFlag            // a JSON array decoded into the field
)

// scrapeFlag ties a typed `sdo scrape` flag to the ScrapeConfig field it sets
// and the gateway parameter that field becomes. The table defines the flags,
// fills the config, names fields in error messages, and tells --param which
// keys already have a flag.
type scrapeFlag struct {
	name, field, wire string
	kind              flagKind
	usage             string
}

// scrapeFlags covers every ScrapeConfig field except URL (the argument),
// Headers (-H/--header sets the request's headers) and ExtraParams (--param);
// TestEveryScrapeConfigFieldHasAFlag keeps it that way.
var scrapeFlags = []scrapeFlag{
	{"render", "Render", "render", boolFlag, "enable JS rendering (costs more credits)"},
	{"render-mode", "RenderMode", "render", stringFlag, "render mode: true|false (--render is the same as --render-mode true)"},
	{"super", "SuperProxy", "super", boolFlag, "use residential & mobile proxies (costs more credits)"},
	{"device", "Device", "device", stringFlag, "device: desktop|mobile|tablet"},
	{"timeout", "Timeout", "timeout", intFlag, "gateway-side timeout in milliseconds, 5000-120000; the client then " +
		"waits at least this long plus 10s (its own per-attempt timeout is --client-timeout, in seconds)"},
	{"retry-timeout", "RetryTimeout", "retryTimeout", intFlag, "gateway-side retry timeout in milliseconds, 5000-55000 " +
		"(cannot be combined with --render)"},
	{"disable-retry", "DisableRetry", "disableRetry", boolFlag, "disable the gateway's automatic retries"},
	{"geo-code", "GeoCode", "geoCode", stringFlag, "country code to route from (e.g. us)"},
	{"regional-geo-code", "RegionalGeoCode", "regionalGeoCode", stringFlag, "route from a random country of a region: " +
		"europe|asia|africa|oceania|northamerica|southamerica (requires --super; cannot be combined with --geo-code)"},
	{"zip-code", "ZipCode", "zipCode", stringFlag, "postal/ZIP code targeting (requires --super and --geo-code)"},
	{"session-id", "SessionID", "sessionId", stringFlag, "sticky proxy session: reuse the same IP across requests " +
		"that pass the same id, an integer 0-1000000"},
	{"set-cookies", "SetCookies", "setCookies", stringFlag, "cookies to send, unencoded: 'name=value; name2=value2'"},
	{"custom-headers", "CustomHeaders", "customHeaders", boolFlag, "send the --header values to the target in place of " +
		"the headers Scrape.do generates (only one of --custom-headers, --extra-headers, --forward-headers)"},
	{"extra-headers", "ExtraHeaders", "extraHeaders", boolFlag, "add --header values named Sd-<Name> to the generated " +
		"headers, as <Name> (only one of --custom-headers, --extra-headers, --forward-headers)"},
	{"forward-headers", "ForwardHeaders", "forwardHeaders", boolFlag, "forward only the --header values, without " +
		"generated headers (only one of --custom-headers, --extra-headers, --forward-headers)"},
	{"custom-wait", "CustomWait", "customWait", intFlag, "extra wait after load, 0-35000 ms (requires --render)"},
	{"wait-selector", "WaitSelector", "waitSelector", stringFlag, "wait for a CSS selector before returning (requires --render)"},
	{"wait-until", "WaitUntil", "waitUntil", stringFlag, "render-completion condition: domcontentloaded|load|networkidle0|networkidle2 (requires --render)"},
	{"block-resources", "BlockResources", "blockResources", boolFlag, "block CSS, images and fonts while rendering " +
		"(the gateway's default; --block-resources=false loads them; true requires --render)"},
	{"play-with-browser", "PlayWithBrowser", "playWithBrowser", jsonFlag, "browser actions, a JSON array (requires --render)"},
	{"show-websocket-requests", "ShowWebsocketRequests", "showWebsocketRequests", boolFlag, "include WebSocket messages " +
		"in the JSON response (requires --render and --return-json)"},
	{"show-frames", "ShowFrames", "showFrames", boolFlag, "include iframe content in the JSON response (requires --render and --return-json)"},
	{"screenshot", "Screenshot", "screenShot", boolFlag, "capture a viewport screenshot, base64 in the JSON response " +
		"(requires --render and --return-json; one screenshot flag at a time)"},
	{"full-screenshot", "FullScreenshot", "fullScreenShot", boolFlag, "capture a full-page screenshot, base64 in the JSON " +
		"response (requires --render and --return-json; one screenshot flag at a time)"},
	{"particular-screenshot", "ParticularScreenshot", "particularScreenShot", stringFlag, "capture a screenshot of one " +
		"CSS selector, base64 in the JSON response (requires --render and --return-json; one screenshot flag at a time; " +
		"cannot be combined with --play-with-browser)"},
	{"width", "Width", "width", intFlag, "browser viewport width in px (requires --render)"},
	{"height", "Height", "height", intFlag, "browser viewport height in px (requires --render)"},
	{"output", "Output", "output", stringFlag, "output format: raw|markdown"},
	{"return-json", "ReturnJSON", "returnJSON", boolFlag, "return the network requests and page content as JSON " +
		"(with --render, the browser's XHR/fetch requests; without it, the plain response wrapped in JSON)"},
	{"pure-cookies", "PureCookies", "pureCookies", boolFlag, "return the target's original Set-Cookie header instead " +
		"of Scrape.do-Cookies (the body is unchanged)"},
	{"disable-redirection", "DisableRedirection", "disableRedirection", boolFlag, "do not follow the target's redirects " +
		"(its Location is printed as location= on stderr)"},
	{"transparent-response", "TransparentResponse", "transparentResponse", boolFlag, "return the target's own status " +
		"code instead of the gateway's"},
}

// resultMeta is the start of the metadata line a result prints on stderr:
// its status and, when the gateway stated it, its cost.
func resultMeta(res *sdo.Result) string {
	meta := fmt.Sprintf("status=%d", res.StatusCode)
	if cost, ok := res.Cost(); ok { // an absent cost is unknown, not free
		meta += fmt.Sprintf(" cost=%d", cost)
	}
	return meta
}

// fieldFlags names each SDK field the way a CLI user sets it, for error
// messages.
func fieldFlags() map[string]string {
	m := map[string]string{
		"URL": "the target URL", "ExtraParams": "--param",
		"Method": "--method", "JSON": "--data-json", "Data": "--data", "Headers": "--header",
	}
	for _, f := range scrapeFlags {
		if _, taken := m[f.field]; !taken {
			m[f.field] = "--" + f.name
		}
	}
	return m
}

func newScrapeCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scrape <url>",
		Short: "Scrape a URL and print the response body",
		Long: "Scrapes a URL through Scrape.do and writes the response body to stdout (or\n" +
			"--output-file); a metadata line goes to stderr. Each call spends credits;\n" +
			"--render and --super cost more.\n\n" +
			"Gateway parameters without a flag of their own can be sent with\n" +
			"--param key=value (repeatable).",
		Args: cobra.ExactArgs(1),
		Example: "  sdo scrape https://example.com --render --geo-code us\n" +
			"  sdo scrape https://example.com --render --wait-selector '#results'\n" +
			"  sdo scrape https://httpbin.org/post --data-json '{\"q\":\"laptop\"}' -H 'X-Trace: 1' --custom-headers",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := buildScrapeConfig(cmd.Flags(), args[0])
			if err != nil {
				return err
			}
			req, err := buildRequest(cmd.Flags())
			if err != nil {
				return err
			}
			client, err := a.newClient()
			if err != nil {
				return err
			}
			res, err := client.Do(cmd.Context(), config, req)
			if err != nil {
				return err
			}
			meta := resultMeta(res) + " url=" + res.URL
			if loc := res.TargetRedirectedLocation(); loc != "" {
				meta += " location=" + loc
			}
			a.printErr(cmd, "%s", meta)

			body := maskToken(res.Bytes(), a.secret)
			if outFile, _ := cmd.Flags().GetString("output-file"); outFile != "" {
				return writeOutputFile(outFile, body)
			}
			return a.writeBody(cmd, body)
		},
	}
	f := cmd.Flags()
	for _, sf := range scrapeFlags {
		switch sf.kind {
		case boolFlag:
			f.Bool(sf.name, false, sf.usage)
		case intFlag:
			f.Int(sf.name, 0, sf.usage)
		default:
			f.String(sf.name, "", sf.usage)
		}
	}
	f.StringArray("param", nil, "a gateway parameter without a flag of its own, as key=value (repeatable)")
	f.String("output-file", "", "write the body to this file (created with mode 0600) instead of stdout")
	f.Bool("raw", false, rawFlagUsage)

	// The HTTP request the gateway sends to the target.
	f.String("method", "", "HTTP method: GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS (default GET, or POST with a body)")
	f.String("data", "", "request body, sent verbatim")
	f.String("data-json", "", "request body as JSON (sent canonicalized, Content-Type: application/json)")
	f.StringArrayP("header", "H", nil, "request header as 'Key: Value' (repeatable)")
	return cmd
}

// buildScrapeConfig maps the flags to a ScrapeConfig (separated for
// testability). Values are checked by the SDK before anything is sent.
func buildScrapeConfig(f *pflag.FlagSet, url string) (*sdo.ScrapeConfig, error) {
	config := &sdo.ScrapeConfig{URL: url}
	fields := reflect.ValueOf(config).Elem()
	for _, sf := range scrapeFlags {
		field := fields.FieldByName(sf.field)
		switch sf.kind {
		case boolFlag:
			v, _ := f.GetBool(sf.name)
			if field.Kind() == reflect.Pointer {
				// Tri-state (*bool, e.g. BlockResources): left nil unless the
				// flag was given, so the gateway default applies; an explicit
				// --flag=false is sent as false.
				if f.Changed(sf.name) {
					field.Set(reflect.ValueOf(&v))
				}
			} else {
				field.SetBool(v)
			}
		case stringFlag:
			v, _ := f.GetString(sf.name)
			field.SetString(v)
		case intFlag:
			v, _ := f.GetInt(sf.name)
			field.SetInt(int64(v))
		case jsonFlag:
			if raw, _ := f.GetString(sf.name); f.Changed(sf.name) {
				if err := decodeJSONInto(raw, field.Addr().Interface()); err != nil {
					return nil, usageErrorf("--%s: %v; want a JSON array", sf.name, err)
				}
			}
		}
	}
	raw, _ := f.GetStringArray("param")
	extra, err := extraParams(raw)
	if err != nil {
		return nil, err
	}
	config.ExtraParams = extra
	return config, nil
}

// extraParams maps repeatable --param key=value flags to ScrapeConfig's
// ExtraParams: the escape hatch for gateway parameters that have no flag. A
// parameter that has one must be set with it, so nothing is sent twice.
func extraParams(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	params := map[string]string{}
	for _, kv := range raw {
		k, v, found := strings.Cut(kv, "=")
		if !found || k == "" {
			return nil, usageErrorf("invalid --param %q (expected key=value)", kv)
		}
		if strings.EqualFold(k, "token") || strings.EqualFold(k, "url") {
			return nil, usageErrorf("--param %s: the token comes from --token, SDO_TOKEN or 'sdo auth login', "+
				"and the target URL is the argument", k)
		}
		for _, sf := range scrapeFlags {
			if strings.EqualFold(k, sf.wire) {
				return nil, usageErrorf("--param %s: use --%s instead", k, sf.name)
			}
		}
		if _, dup := params[k]; dup {
			return nil, usageErrorf("--param %s is given more than once", k)
		}
		params[k] = v
	}
	return params, nil
}

// buildRequest maps --method, --data, --data-json and --header to the
// sdo.Request the gateway sends to the target; nil when none is given (a plain
// GET). The SDK validates the method and the method/body combination.
func buildRequest(f *pflag.FlagSet) (*sdo.Request, error) {
	method, _ := f.GetString("method")
	headers, _ := f.GetStringArray("header")
	hasData, hasJSON := f.Changed("data"), f.Changed("data-json")
	if hasData && hasJSON {
		return nil, usageErrorf("--data and --data-json are mutually exclusive")
	}
	if method == "" && !hasData && !hasJSON && len(headers) == 0 {
		return nil, nil
	}
	req := &sdo.Request{Method: method}
	if hasData {
		data, _ := f.GetString("data")
		req.Data = []byte(data)
	}
	if hasJSON {
		raw, _ := f.GetString("data-json")
		v, err := decodeJSONArg(raw)
		if err != nil {
			return nil, usageErrorf("--data-json: %v", err)
		}
		req.JSON = v
	}
	for _, h := range headers {
		k, v, ok := strings.Cut(h, ":")
		if k = strings.TrimSpace(k); !ok || k == "" {
			return nil, usageErrorf("invalid --header %q (expected 'Key: Value')", h)
		}
		v = strings.TrimSpace(v)
		if err := checkHeader(k, v); err != nil {
			return nil, err
		}
		if req.Headers == nil {
			req.Headers = map[string]string{}
		}
		req.Headers[k] = v
	}
	return req, nil
}

// clientHeaders are set by the HTTP client from the request itself; a value
// given for one would conflict with the real framing.
var clientHeaders = map[string]bool{"host": true, "content-length": true, "transfer-encoding": true, "connection": true}

// checkHeader applies the SDK's rules for request headers before anything is
// sent, so a bad header is a configuration error (exit 2) rather than a
// retried connection failure: the name is an RFC 9110 token, the value is
// printable ASCII, spaces and tabs. The value is never echoed: it may be a
// credential.
func checkHeader(name, value string) error {
	bad := func(why string) error {
		return &sdo.ConfigError{Message: fmt.Sprintf("invalid --header %.64q: %s", name, why)}
	}
	for i := 0; i < len(name); i++ {
		if !isTokenChar(name[i]) {
			return bad("a header name may contain only letters, digits and !#$%&'*+-.^_`|~")
		}
	}
	if clientHeaders[strings.ToLower(name)] {
		return bad("this header is set by the HTTP client and cannot be given")
	}
	for i := 0; i < len(value); i++ {
		if c := value[i]; c != '\t' && (c < ' ' || c > '~') {
			return bad("the value may contain only printable ASCII characters, spaces and tabs " +
				"(no CR, LF, NUL or other control or non-ASCII characters)")
		}
	}
	return nil
}

// isTokenChar reports whether c is an RFC 9110 tchar.
func isTokenChar(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' ||
		strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0
}

// writeOutputFile writes the body to path, owner-only (0600) like the token
// file: a scraped page can carry session cookies or personal data. It is
// written to a temporary file in the same directory and renamed over path, so
// an existing file is replaced by a 0600 one rather than keeping its wider
// mode. A symlink or anything but a regular file at path is refused: the body
// would otherwise land wherever the link points, or replace a device node.
func writeOutputFile(path string, body []byte) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	case info.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("--output-file %s is a symlink; refusing to follow it (name the target file instead)", path)
	case !info.Mode().IsRegular():
		return fmt.Errorf("--output-file %s exists and is not a regular file", path)
	}
	return writeFileAtomic(path, body)
}

// decodeJSONArg parses exactly one JSON value, keeping numbers exact
// (json.Number) so an id like 12345678901234567890 is sent as written.
func decodeJSONArg(raw string) (any, error) {
	var v any
	if err := decodeJSONInto(raw, &v); err != nil {
		return nil, err
	}
	if v == nil {
		return nil, fmt.Errorf("null is not a request body")
	}
	return v, nil
}

// decodeJSONInto parses exactly one JSON value into dst, keeping numbers exact.
func decodeJSONInto(raw string, dst any) error {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		var ute *json.UnmarshalTypeError
		if errors.As(err, &ute) {
			return fmt.Errorf("unexpected JSON %s", ute.Value)
		}
		return fmt.Errorf("not valid JSON: %v", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("not valid JSON: unexpected data after the value")
	}
	return nil
}
