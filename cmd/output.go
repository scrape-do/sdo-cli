// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

// printErr writes one line of metadata to stderr, redacted and with control
// characters made visible (see terminalSafe).
func (a *app) printErr(cmd *cobra.Command, format string, args ...any) {
	fmt.Fprintln(cmd.ErrOrStderr(), terminalSafe(a.redact(fmt.Sprintf(format, args...))))
}

// terminalSafe makes text from the API or a target (a URL, a task state, an
// error message) safe to print on a terminal: every C0 control character but
// newline and tab, DEL, every C1 control character (U+0080-U+009F) and every
// byte that is not valid UTF-8 (a lone 0x9B is CSI on some terminals) is
// written as a Go escape such as \x1b. Otherwise an escape sequence in a
// page's error text could retitle the window, rewrite earlier output or worse.
func terminalSafe(s string) string {
	clean := utf8.ValidString(s)
	for _, r := range s {
		if !clean {
			break
		}
		clean = !controlRune(r)
	}
	if clean {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case controlRune(r) && r < 0x80:
			fmt.Fprintf(&b, `\x%02x`, r)
		case controlRune(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// controlRune reports a control character terminalSafe escapes.
func controlRune(r rune) bool {
	return r < 0x20 && r != '\n' && r != '\t' || r == 0x7f || 0x80 <= r && r <= 0x9f
}

// errBinaryToTerminal refuses to write a body with NUL or ESC bytes to a
// terminal, as curl does: such a body is binary data, or text carrying escape
// sequences, and either can mess up the terminal.
var errBinaryToTerminal = errors.New("binary output can mess up your terminal; " +
	"redirect or pipe it (or use --output-file with 'sdo scrape'), or pass --raw to print it anyway")

// writeBody writes a response body to stdout. When stdout is a terminal and
// the body holds a NUL or ESC byte, it writes nothing and returns
// errBinaryToTerminal unless --raw was given.
func (a *app) writeBody(cmd *cobra.Command, body []byte) error {
	out := cmd.OutOrStdout()
	if raw, _ := cmd.Flags().GetBool("raw"); !raw && bytes.ContainsAny(body, "\x00\x1b") {
		if f, ok := out.(*os.File); ok && a.isTerminal(int(f.Fd())) {
			return errBinaryToTerminal
		}
	}
	_, err := out.Write(body)
	return err
}

// rawFlagUsage is the help text of --raw on every command that prints a body.
const rawFlagUsage = "write the body to a terminal even if it contains NUL or ESC bytes (binary data or escape sequences)"

// printJSON writes v — data from the API: a job, a task result, a plugin's
// output — as indented JSON.
//
// Like page content, this data is the user's: only the token itself is
// masked, in every string, keys included. Credential shapes are left alone
// (a scraped link such as "https://shop.example/reset?token=abc" is the
// page's own data and must reach a script unaltered); shape redaction is for
// the CLI's own messages (errors and the metadata lines, see printErr).
// Masking the decoded strings rather than the encoded text keeps the output
// valid JSON.
func (a *app) printJSON(w io.Writer, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // numbers pass through exactly as the API sent them
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // URLs keep their '&' rather than \u0026
	enc.SetIndent("", "  ")
	secret := a.knownSecret()
	return enc.Encode(mapStrings(tree, func(s string) string { return string(maskToken([]byte(s), secret)) }))
}

// mapStrings applies f to every string in a decoded JSON tree, keys included.
func mapStrings(v any, f func(string) string) any {
	switch x := v.(type) {
	case string:
		return f(x)
	case []any:
		for i, e := range x {
			x[i] = mapStrings(e, f)
		}
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[f(k)] = mapStrings(e, f)
		}
		return out
	}
	return v
}
