package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/jsonpretty"
	"github.com/cli/go-gh/v2/pkg/tableprinter"
	"github.com/shurcooL/githubv4"
	"github.com/spf13/cobra"
)

const defaultListLimit = 30

var listFields = []string{
	"login",
	"name",
}

var listFieldsMap = func() map[string]struct{} {
	m := make(map[string]struct{}, len(listFields))
	for _, f := range listFields {
		m[f] = struct{}{}
	}
	return m
}()

type ListOptions struct {
	Client   *api.GraphQLClient
	IOs      Terminal
	Prompter Prompter

	Username  string
	FieldsRaw string
	Fields    []string
}

func NewCmdList(
	client *api.GraphQLClient,
	ios Terminal,
	prompter Prompter,
	runF func(*ListOptions) error,
) *cobra.Command {
	opts := &ListOptions{
		Client:   client,
		IOs:      ios,
		Prompter: prompter,
	}

	cmd := &cobra.Command{
		Use:     "list [<user>]",
		Short:   "List sponsors",
		Long:    `List sponsors of a given user.`,
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return errors.New("too many arguments")
			} else if len(args) == 1 {
				opts.Username = args[0]
			}

			if opts.FieldsRaw != "" {
				fields := strings.Split(opts.FieldsRaw, ",")
				for _, f := range fields {
					if _, ok := listFieldsMap[f]; !ok {
						return fmt.Errorf("unknown JSON field: %q (available fields: %s)", f, strings.Join(listFields, ", "))
					}
				}
				opts.Fields = fields
			}

			if runF != nil {
				return runF(opts)
			}

			return listRun(opts)
		},
	}

	// We can't use StringSliceVar method since it supports multiple assignments
	// like: --json a,b --json c
	cmd.Flags().StringVar(&opts.FieldsRaw, "json", "", "JSON fields")

	return cmd
}

func listRun(opts *ListOptions) error {
	username := opts.Username

	if username == "" {
		if !opts.IOs.IsTerminalOutput() {
			return errors.New("username not provided")
		}
		value, err := opts.Prompter.Input("Which user do you want to target?", "")
		if err != nil {
			return err
		}
		username = value
	}

	sponsors, err := listSponsors(opts.Client, username, defaultListLimit)
	if err != nil {
		return err
	}

	if opts.Fields != nil {
		data := make([]any, 0, len(sponsors))
		for _, sponsor := range sponsors {
			m := make(map[string]any, 2)
			for _, f := range opts.Fields {
				switch f {
				case "login":
					m["login"] = sponsor.Login
				case "name":
					m["name"] = sponsor.Name
				}
			}
			data = append(data, m)
		}

		buf := &bytes.Buffer{}
		if err := json.NewEncoder(buf).Encode(data); err != nil {
			return err
		}

		if opts.IOs.IsTerminalOutput() {
			jsonpretty.Format(opts.IOs.Out(), buf, "  ", true)
			return nil
		}

		io.Copy(opts.IOs.Out(), buf)
		return nil
	}

	if len(sponsors) == 0 {
		if opts.IOs.IsTerminalOutput() {
			fmt.Fprintln(opts.IOs.ErrOut(), "no sponsor found")
			return nil
		}
		return nil
	}

	width, _, _ := opts.IOs.Size()
	headers := []string{"SPONSOR"}
	table := tableprinter.New(opts.IOs.Out(), opts.IOs.IsTerminalOutput(), width)
	table.AddHeader(headers)
	for _, sponsor := range sponsors {
		table.AddField(sponsor.Login)
		table.EndRow()
	}

	err = table.Render()
	if err != nil {
		return err
	}

	return nil
}

type sponsor struct {
	Login string
	Name  string
}

func listSponsors(client *api.GraphQLClient, username string, limit uint) ([]sponsor, error) {
	var query struct {
		User struct {
			Sponsors struct {
				Edges []struct {
					Node struct {
						User struct {
							Login githubv4.String
							Name  githubv4.String
						} `graphql:"... on User"`
						Org struct {
							Login githubv4.String
							Name  githubv4.String
						} `graphql:"... on Organization"`
					}
				}
			} `graphql:"sponsors(first: $limit, orderBy: { direction: ASC, field: LOGIN })"`
		} `graphql:"user(login: $login)"`
	}

	variables := map[string]any{
		"login": githubv4.String(username),
		"limit": githubv4.Int(limit),
	}

	err := client.Query("UserSponsorList", &query, variables)
	if err != nil {
		return nil, err
	}

	result := make([]sponsor, 0, len(query.User.Sponsors.Edges))
	for _, edge := range query.User.Sponsors.Edges {
		if edge.Node.User.Login != "" {
			result = append(result, sponsor{
				Login: string(edge.Node.User.Login),
				Name:  string(edge.Node.User.Name),
			})
		} else if edge.Node.Org.Login != "" {
			result = append(result, sponsor{
				Login: string(edge.Node.Org.Login),
				Name:  string(edge.Node.Org.Name),
			})
		}
	}
	return result, nil
}
