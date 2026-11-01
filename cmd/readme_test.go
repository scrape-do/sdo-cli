// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// shellWords splits one example command line the way a POSIX shell would for
// the cases the docs use: quotes, a trailing comment, a pipe (the last stage
// is kept) and an output redirection (dropped).
func shellWords(line string) []string {
	var words []string
	var cur strings.Builder
	inWord := false
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote, inWord = c, true
		case c == ' ' || c == '\t':
			if inWord {
				words, inWord = append(words, cur.String()), false
				cur.Reset()
			}
		case c == '#' && !inWord:
			i = len(line)
		case c == '|':
			words, inWord = nil, false
			cur.Reset()
		case c == '>' && !inWord:
			i = len(line)
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}

var placeholder = regexp.MustCompile(`^<(\w+)>$`)

// checkExample fails when an `sdo ...` example names a command, flag or
// argument count the CLI does not have.
func checkExample(t *testing.T, where, line string) {
	t.Helper()
	words := shellWords(line)
	if len(words) == 0 || words[0] != "sdo" {
		return
	}
	for i, w := range words {
		words[i] = placeholder.ReplaceAllString(w, "x1")
	}
	root := NewRootCommand()
	c, rest, err := root.Find(words[1:])
	if err != nil {
		t.Errorf("%s: %q: %v", where, line, err)
		return
	}
	if err := c.ParseFlags(rest); err != nil {
		t.Errorf("%s: %q: %v", where, line, err)
		return
	}
	if c.Args != nil {
		if err := c.Args(c, c.Flags().Args()); err != nil {
			t.Errorf("%s: %q: %v", where, line, err)
		}
	}
}

// Every `sdo` command line in the README's shell blocks, and in each
// command's own examples, uses commands and flags that exist.
func TestDocumentedExamplesParse(t *testing.T) {
	readme, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatal(err)
	}
	inShell, checked := false, 0
	for _, line := range strings.Split(string(readme), "\n") {
		switch {
		case strings.HasPrefix(line, "```bash"):
			inShell = true
		case strings.HasPrefix(line, "```"):
			inShell = false
		case inShell:
			checkExample(t, "README.md", line)
			checked++
		}
	}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, line := range strings.Split(c.Example, "\n") {
			checkExample(t, c.CommandPath()+" example", strings.TrimSpace(line))
			checked++
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(NewRootCommand())
	if checked < 20 {
		t.Errorf("only %d example lines found", checked)
	}
}

func TestShellWords(t *testing.T) {
	cases := map[string]string{
		`sdo scrape https://e.com --wait-selector '#results'   # comment`: `sdo|scrape|https://e.com|--wait-selector|#results`,
		`printenv MY_TOKEN | sdo auth login`:                              `sdo|auth|login`,
		`sdo jobs result <jobID> <taskID> > page.html`:                    `sdo|jobs|result|<jobID>|<taskID>`,
		`sdo scrape x --data-json '{"q":"a b"}' -H "X: 1"`:                `sdo|scrape|x|--data-json|{"q":"a b"}|-H|X: 1`,
	}
	for in, want := range cases {
		if got := strings.Join(shellWords(in), "|"); got != want {
			t.Errorf("shellWords(%q) = %q, want %q", in, got, want)
		}
	}
}
