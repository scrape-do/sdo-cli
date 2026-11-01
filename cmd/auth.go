// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

func newAuthCmd(a *app) *cobra.Command {
	login := &cobra.Command{
		Use:   "login",
		Short: "Store your API token in the per-user config file (mode 0600)",
		Long: "Reads the token from stdin — without echo when stdin is a terminal — and\n" +
			"writes it to the per-user config file with owner-only permissions, so it\n" +
			"need not live in shell history or in an exported environment variable that\n" +
			"every child process inherits.\n\n" +
			"Token precedence: --token > SDO_TOKEN > config file.",
		Example: "  sdo auth login                       # prompts, input hidden\n" +
			"  printenv MY_TOKEN | sdo auth login   # from a pipe",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			token, source := trimToken(a.token), "--token flag"
			if token == "" {
				raw, err := a.readToken(cmd.InOrStdin(), cmd.ErrOrStderr())
				if err != nil {
					return err
				}
				token, source = trimToken(raw), "input"
			}
			if token == "" {
				return errors.New("no token provided")
			}
			if err := checkToken(token, source); err != nil {
				return err
			}
			path, err := writeTokenToConfigFile(token)
			if err != nil {
				return err
			}
			// Never echo the token itself back to the terminal.
			fmt.Fprintf(cmd.OutOrStdout(), "Token saved to %s (mode 0600).\n", path)
			return nil
		},
	}
	status := &cobra.Command{
		Use:   "status",
		Short: "Show which source the token would come from (never prints the token)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, _ := configFilePath()
			_, source, err := a.resolveToken()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "token source: %s\nconfig file:  %s\n", source, path)
			return nil
		},
	}
	auth := &cobra.Command{Use: "auth", Short: "Manage the stored API token"}
	groupCommand(auth)
	auth.AddCommand(login, status)
	return auth
}

// readToken reads one token from in: from an interactive terminal without
// echoing it, otherwise as the first line of a pipe or file.
func (a *app) readToken(in io.Reader, prompt io.Writer) (string, error) {
	if f, ok := in.(*os.File); ok && a.isTerminal(int(f.Fd())) {
		fmt.Fprint(prompt, "Scrape.do API token (input hidden): ")
		b, err := a.readPassword(int(f.Fd()))
		fmt.Fprintln(prompt)
		if err != nil {
			return "", fmt.Errorf("read token: %w", err)
		}
		return string(b), nil
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("no token on stdin: run 'sdo auth login' in a terminal, " +
			"or pipe it in, e.g. 'printenv MY_TOKEN | sdo auth login'")
	}
	return line, nil
}
