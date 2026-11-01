// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// main runs the CLI with the process's arguments and exits with its code. It
// calls os.Exit, so it runs in a child process: the test binary itself, re-run
// with SDO_CLI_TEST_MAIN set to the arguments to give it.
func TestMainExitsWithTheCLIsCode(t *testing.T) {
	if args, ok := os.LookupEnv("SDO_CLI_TEST_MAIN"); ok {
		os.Args = append([]string{"sdo"}, strings.Fields(args)...)
		main()
		return
	}
	for _, c := range []struct {
		args, want string
		code       int
	}{
		{"version", "sdo CLI ", 0},
		{"frobnicate", `unknown command "frobnicate"`, 2},
	} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestMainExitsWithTheCLIsCode$")
		cmd.Env = append(os.Environ(), "SDO_CLI_TEST_MAIN="+c.args)
		out, err := cmd.CombinedOutput()
		code := 0
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatalf("%s: %v", c.args, err)
		}
		if code != c.code || !strings.Contains(string(out), c.want) {
			t.Errorf("sdo %s: exit %d, want %d with %q (%q)", c.args, code, c.code, c.want, out)
		}
	}
}
