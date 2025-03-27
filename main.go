package main

import (
	"fmt"
	"io"
	"os"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/prompter"
	"github.com/cli/go-gh/v2/pkg/term"
	"github.com/spf13/cobra"
)

type Terminal interface {
	In() io.Reader
	Out() io.Writer
	ErrOut() io.Writer
	IsTerminalOutput() bool
	Size() (int, int, error)
}

type Prompter interface {
	Input(prompt, defaultValue string) (string, error)
}

func compose() (*cobra.Command, error) {
	client, err := api.DefaultGraphQLClient()
	api.NewGraphQLClient(api.ClientOptions{})
	if err != nil {
		return nil, err
	}

	ios := term.FromEnv()

	var pr Prompter
	if ios.IsTerminalOutput() {
		stdin, _ := ios.In().(*os.File)
		stdout, _ := ios.Out().(*os.File)
		stderr, _ := ios.ErrOut().(*os.File)
		if stdin != nil || stdout != nil || stderr != nil {
			pr = prompter.New(stdin, stdout, stderr)
		}
	}

	rootCmd := &cobra.Command{
		Use:   "sponsors <subcommand> [flags]",
		Short: "Manage sponsors",
	}

	rootCmd.AddCommand(NewCmdList(client, ios, pr, nil))

	return rootCmd, nil
}

func main() {
	rc, err := compose()
	if err != nil {
		fmt.Fprintf(os.Stderr, "composition failed: %s\n", err)
	}
	if err := rc.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
