// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/scrape-do/sdo-cli/internal/explain"
	"github.com/scrape-do/sdo-cli/internal/version"
	sdo "github.com/scrape-do/sdo-go"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Exit codes. Scripts branch on these, so they are part of the CLI contract.
const (
	exitOK      = 0
	exitFailure = 1 // the request or the local I/O failed
	exitUsage   = 2 // bad command line, or input the SDK rejected before sending
	exitAuth    = 3 // no token, or the gateway refused it
	exitPending = 4 // `sdo jobs result`: the task has not finished yet
)

var errNoToken = errors.New("no API token: pass --token, set SDO_TOKEN, or run 'sdo auth login'")

// usageError marks a command-line mistake (unknown command or flag, wrong
// argument count, conflicting flags) so Main can exit with exitUsage.
type usageError struct{ error }

func (e usageError) Unwrap() error { return e.error }

func usageErrorf(format string, args ...any) error {
	return usageError{fmt.Errorf(format, args...)}
}

// app is the state of one CLI invocation: the global flags, the client built
// from them, and the terminal hooks. Main builds a fresh one per call, so
// concurrent invocations (tests) share nothing.
type app struct {
	token            string        // --token
	insecureHTTP     bool          // --insecure-http
	clientTimeout    time.Duration // --client-timeout; 0 = SDO_TIMEOUT or the SDK default
	maxResponseBytes int64         // --max-response-bytes; 0 = the SDK default

	// Set once a client is built, so every later output can be redacted.
	client *sdo.Client
	secret string

	isTerminal   func(fd int) bool
	readPassword func(fd int) ([]byte, error)
}

func newApp() *app {
	return &app{isTerminal: term.IsTerminal, readPassword: term.ReadPassword}
}

// NewRootCommand returns the sdo command tree, for tools that inspect it
// (tools/apisurface). Running the CLI goes through Main.
func NewRootCommand() *cobra.Command { return newApp().rootCmd() }

func (a *app) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "sdo",
		Short:         "Scrape.do CLI — scrape the web, call plugins, serve MCP",
		Version:       version.CLI,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	groupCommand(root)
	pf := root.PersistentFlags()
	// No backquotes in usage strings: pflag shows back-quoted text as the
	// flag's value name ("--token sdo auth login").
	pf.StringVar(&a.token, "token", "", "Scrape.do API token (or set SDO_TOKEN, or run 'sdo auth login')")
	pf.BoolVar(&a.insecureHTTP, "insecure-http", false,
		"allow a plain-http SDO_BASE_URL / SDO_ASYNC_BASE_URL on a non-loopback host (sends the token in cleartext)")
	pf.Var(&secondsValue{&a.clientTimeout}, "client-timeout",
		"how long the client waits for each HTTP attempt, in seconds (default 70, or SDO_TIMEOUT)")
	pf.Var(&bytesValue{&a.maxResponseBytes}, "max-response-bytes",
		"largest response body to accept, after decompression (default 33554432, i.e. 32 MiB)")
	root.AddCommand(newScrapeCmd(a), newPluginCmd(a), newJobsCmd(a), newAccountCmd(a), newUsageCmd(a),
		newMCPCmd(a), newAuthCmd(a), newVersionCmd())
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err} })
	// Add cobra's own help and completion commands now (Execute would add them
	// later) so their argument errors are usage errors too.
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
	markArgErrors(root)
	return root
}

// groupCommand makes a command that only holds subcommands reject an unknown
// subcommand. Cobra's default prints the help and exits 0, so a typo such as
// `sdo jobs stauts` looked like success to a script.
func groupCommand(c *cobra.Command) {
	c.Args = cobra.ArbitraryArgs
	c.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}
		msg := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
		if s := cmd.SuggestionsFor(args[0]); len(s) > 0 {
			msg += "; did you mean " + strings.Join(s, " or ") + "?"
		}
		return usageError{errors.New(msg)}
	}
}

// markArgErrors wraps every argument validator so a wrong argument count is
// reported as a usage error.
func markArgErrors(c *cobra.Command) {
	if validate := c.Args; validate != nil {
		c.Args = func(cmd *cobra.Command, args []string) error {
			if err := validate(cmd, args); err != nil {
				return usageError{err}
			}
			return nil
		}
	}
	for _, sub := range c.Commands() {
		markArgErrors(sub)
	}
}

// Main runs the CLI with args and returns the process exit code.
//
// The root command silences cobra's own error printing so that every failure
// is reported exactly once, here, in one format, redacted.
func Main(args []string, stdout, stderr io.Writer) int {
	return newApp().run(args, os.Stdin, stdout, stderr)
}

func (a *app) run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := a.rootCmd()
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	cmd, err := root.ExecuteC()
	if err == nil {
		return exitOK
	}
	fmt.Fprintf(stderr, "sdo: %s\n", terminalSafe(a.describe(err)))
	code := exitCode(err)
	if code == exitUsage && errors.As(err, new(usageError)) {
		fmt.Fprintf(stderr, "Run '%s --help' for usage.\n", cmd.CommandPath())
	}
	return code
}

func exitCode(err error) int {
	var apiErr *sdo.APIError
	switch {
	case errors.Is(err, errNoToken), errors.As(err, &apiErr) && apiErr.IsAuth():
		return exitAuth
	case errors.As(err, new(usageError)), errors.As(err, new(*sdo.ConfigError)):
		return exitUsage
	case errors.As(err, new(taskPendingError)):
		return exitPending
	default:
		return exitFailure
	}
}

// newClient builds the client for this invocation: token by --token >
// SDO_TOKEN > config file, identified as sdo-cli/<version>. The SDK reads
// SDO_BASE_URL, SDO_ASYNC_BASE_URL and, unless --client-timeout is given,
// SDO_TIMEOUT itself.
func (a *app) newClient() (*sdo.Client, error) {
	token, _, err := a.resolveToken()
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, errNoToken
	}
	a.secret = token
	opts := []sdo.Option{sdo.WithUserAgent(version.UserAgent)}
	if a.insecureHTTP {
		opts = append(opts, sdo.WithAllowInsecureHTTP())
	}
	if a.clientTimeout > 0 {
		opts = append(opts, sdo.WithTimeout(a.clientTimeout))
	}
	if a.maxResponseBytes > 0 {
		opts = append(opts, sdo.WithMaxResponseBytes(a.maxResponseBytes))
	}
	c, err := sdo.New(token, opts...)
	if err != nil {
		return nil, err
	}
	a.client = c
	return c, nil
}

// cliNames names the SDK's fields the way a CLI user sets them.
var cliNames = explain.Names{
	Fields:       fieldFlags(),
	EnableRender: "pass --render",
}

// describe renders err for the terminal, redacted: in the CLI's terms rather
// than the Go SDK's (flags and environment variables, not fields and
// options), with a hint where the fix is not in the message.
func (a *app) describe(err error) string {
	msg := err.Error()
	if errors.As(err, new(*sdo.ConfigError)) {
		msg = explain.Config(msg, cliNames)
	} else {
		msg = explain.Options(msg)
	}
	var apiErr *sdo.APIError
	if errors.As(err, &apiErr) {
		// Scrape.do-Request-Cost is the authoritative charge; absent is unknown.
		if cost, ok := apiErr.Cost(); ok {
			msg += fmt.Sprintf(" cost=%d", cost)
		}
	}
	var te *sdo.TransportError
	if errors.As(err, &te) && te.Timeout {
		msg += "\n(the client stopped waiting; raise its per-attempt timeout with --client-timeout or SDO_TIMEOUT, in seconds)"
	}
	if errors.As(err, new(*sdo.ResponseTooLargeError)) && !strings.Contains(msg, "--max-response-bytes") {
		msg += "\n(raise the limit with --max-response-bytes if you expect responses this large)"
	}
	if strings.Contains(msg, "plain http") && !strings.Contains(msg, "--insecure-http") {
		msg += "\n(to send the token over plain http anyway, pass --insecure-http)"
	}
	return a.redact(msg)
}

// redact masks the token and credential-shaped values in the CLI's own
// messages (errors, metadata lines); data output masks only the token (see
// printJSON and maskToken). Before a client exists it still masks the token
// given on the command line.
func (a *app) redact(text string) string {
	if a.client != nil {
		return a.client.Redact(text)
	}
	return sdo.RedactSecrets(string(maskToken([]byte(text), a.knownSecret())))
}

func (a *app) knownSecret() string {
	if a.secret != "" {
		return a.secret
	}
	return trimToken(a.token)
}

// maskToken replaces the token, literally and percent-encoded as it travels on
// the wire, with "***" and leaves every other byte alone. It is for page
// content (a scraped body, a task result): the token can only be there if the
// gateway echoed it, while a credential-shaped string such as "?token=abc" is
// the page's own data and must reach the user unaltered. Tokens shorter than 8
// characters are left alone, as in the SDK, so a short test token cannot shred
// ordinary text.
func maskToken(b []byte, token string) []byte {
	if len(token) < 8 {
		return b
	}
	for _, form := range []string{token, percentEncode(token)} {
		if bytes.Contains(b, []byte(form)) {
			b = bytes.ReplaceAll(b, []byte(form), []byte("***"))
		}
	}
	return b
}

// percentEncode is RFC 3986 encoding: unreserved characters literal, every
// other byte %XX in upper-case hex — the form the SDK puts in the query.
func percentEncode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9',
			c == '-', c == '.', c == '_', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}
