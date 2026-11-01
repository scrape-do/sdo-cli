// SPDX-License-Identifier: Apache-2.0

package main

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/scrape-do/sdo-cli/cmd"
	"github.com/spf13/cobra"
)

// main prints the surface, and nothing else. It runs in a child process (the
// test binary itself, re-run with APISURFACE_TEST_MAIN set) so its stdout can
// be read.
func TestMainPrintsTheSurface(t *testing.T) {
	if os.Getenv("APISURFACE_TEST_MAIN") == "1" {
		main()
		os.Exit(0) // before the test framework prints PASS: stdout is main's alone
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainPrintsTheSurface$")
	cmd.Env = append(os.Environ(), "APISURFACE_TEST_MAIN=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); got != render() {
		t.Errorf("main printed something else than render():\n%s", got)
	}
}

// The committed golden is what this generator prints; CI regenerates it too,
// but a stale golden should already fail `go test`.
func TestGoldenIsCurrent(t *testing.T) {
	want, err := os.ReadFile("../../api-surface.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got := render(); got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Errorf("api-surface.txt is stale; regenerate with: go run ./tools/apisurface > api-surface.txt")
	}
}

var lineRe = regexp.MustCompile(`^(install|command|arg|flag|mcp-tool|mcp-param) \S+( \S+)*$`)

// Every line follows the grammar: one symbol, single spaces, sorted, no
// versions, nothing duplicated.
func TestGrammar(t *testing.T) {
	out := render()
	var lines []string
	for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if !strings.HasPrefix(l, "#") {
			lines = append(lines, l)
		}
	}
	if !sort.StringsAreSorted(lines) {
		t.Error("lines are not sorted")
	}
	seen := map[string]bool{}
	for _, l := range lines {
		if !lineRe.MatchString(l) || strings.Contains(l, "  ") {
			t.Errorf("malformed line %q", l)
		}
		if regexp.MustCompile(`\d+\.\d+\.\d+`).MatchString(l) {
			t.Errorf("version-like value in %q", l)
		}
		if seen[l] {
			t.Errorf("duplicate line %q", l)
		}
		seen[l] = true
	}
	for _, want := range []string{
		"install sdo github.com/scrape-do/sdo-cli/cmd/sdo",
		"command sdo.scrape",
		"arg sdo.scrape #0 url [required]",
		"arg sdo.jobs.create #1 url...",
		"flag sdo --token string",
		"flag sdo --version -v bool",
		"flag sdo.scrape --header -H stringArray",
		"mcp-tool scrape",
		"mcp-param scrape url string [required]",
		"mcp-param scrape device string enum=desktop|mobile|tablet",
	} {
		if !seen[want] {
			t.Errorf("missing %q", want)
		}
	}
}

func TestFlagDefaults(t *testing.T) {
	if got := render(); strings.Contains(got, "default=false") || strings.Contains(got, "default=0") {
		t.Error("zero defaults must be omitted")
	}
}

// Hidden commands and hidden flags are not surface; a non-zero default is.
func TestHiddenAndDefaults(t *testing.T) {
	root := &cobra.Command{Use: "x"}
	root.Flags().Int("retries", 3, "")
	root.Flags().Bool("secret", false, "")
	root.Flags().MarkHidden("secret")
	root.AddCommand(&cobra.Command{Use: "internal", Hidden: true, Run: func(*cobra.Command, []string) {}})
	got := strings.Join(commandLines(root), "\n")
	if !strings.Contains(got, "flag x --retries int default=3") {
		t.Errorf("default missing:\n%s", got)
	}
	if strings.Contains(got, "internal") || strings.Contains(got, "secret") {
		t.Errorf("hidden surface listed:\n%s", got)
	}
}

// The install path is a main package whose directory is named like the root
// command, so `go install <installPath>@latest` installs a binary named sdo.
func TestInstallPathBuildsTheSdoBinary(t *testing.T) {
	dir := filepath.Join("..", "..", strings.TrimPrefix(installPath, "github.com/scrape-do/sdo-cli/"))
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, "main.go"), nil, parser.PackageClauseOnly)
	if err != nil || f.Name.Name != "main" {
		t.Fatalf("%s is not a main package: %v", dir, err)
	}
	if got := path.Base(installPath); got != cmd.NewRootCommand().Name() {
		t.Errorf("go install would name the binary %q, not %q", got, cmd.NewRootCommand().Name())
	}
}
