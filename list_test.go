package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/prompter"
	"github.com/google/shlex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCmdList(t *testing.T) {
	tests := []struct {
		name    string
		cli     string
		wants   ListOptions
		wantErr string
	}{
		{
			name: "no arg",
			cli:  "",
			wants: ListOptions{
				Username: "",
			},
		},
		{
			name: "normal",
			cli:  "johndoe",
			wants: ListOptions{
				Username: "johndoe",
			},
		}, {
			name: "normal json",
			cli:  "--json name,login johndoe",
			wants: ListOptions{
				Username: "johndoe",
			},
		}, {
			name:    "failure json",
			cli:     "--json blah johndoe",
			wantErr: "unknown JSON field: \"blah\" (available fields: login, name)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argv, err := shlex.Split(tt.cli)
			assert.NoError(t, err)

			var listOpts *ListOptions
			cmd := NewCmdList(
				nil, nil, nil,
				func(opts *ListOptions) error {
					listOpts = opts
					return nil
				},
			)
			cmd.SetArgs(argv)
			cmd.SetIn(&bytes.Buffer{})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})

			_, err = cmd.ExecuteC()
			if tt.wantErr != "" {
				require.Equal(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)

			require.Equal(t, tt.wants.Username, listOpts.Username)
		})
	}
}

func Test_listRun(t *testing.T) {
	defaultHTTPStubs := func(t *testing.T, mt *mockTransport) {
		mt.respBody = `
				{
					"data": {
						"user": {
							"sponsors": {
								"edges": [
									{
										"node": {
											"login": "foo",
											"name": "Foo"
										}
									},
									{
										"node": {
											"login": "bar",
											"name": "Bar"
										}
									}
								]
							}
						}
					}
				}`
	}

	emptyRespHTTPStubs := func(t *testing.T, mt *mockTransport) {
		mt.respBody = `
				{
					"data": {
						"user": {
							"sponsors": {
								"edges": []
							}
						}
					}
				}`
	}

	tests := []struct {
		name          string
		tty           bool
		opts          *ListOptions
		httpStubs     func(*testing.T, *mockTransport)
		prompterStubs func(*testing.T, *prompter.PrompterMock)
		wantStdout    []string
		wantStderr    string
		wantErr       string
	}{
		{
			name: "normal tty",
			tty:  true,
			opts: &ListOptions{
				Username: "johndoe",
			},
			httpStubs: defaultHTTPStubs,
			wantStdout: []string{
				"SPONSOR",
				"foo",
				"bar",
			},
		}, {
			name:      "normal tty, no-username",
			tty:       true,
			opts:      &ListOptions{},
			httpStubs: defaultHTTPStubs,
			prompterStubs: func(t *testing.T, pm *prompter.PrompterMock) {
				pm.RegisterInput("Which user do you want to target?", func(_, def string) (string, error) {
					assert.Empty(t, def)
					return "johndoe", nil
				})
			},
			wantStdout: []string{
				"SPONSOR",
				"foo",
				"bar",
			},
		}, {
			name: "normal no-tty",
			tty:  false,
			opts: &ListOptions{
				Username: "johndoe",
			},
			httpStubs: defaultHTTPStubs,
			wantStdout: []string{
				"foo",
				"bar",
			},
		}, {
			name: "normal json",
			tty:  false,
			opts: &ListOptions{
				Username: "johndoe",
				Fields:   []string{"login"},
			},
			httpStubs:  defaultHTTPStubs,
			wantStdout: []string{"[{\"login\":\"foo\"},{\"login\":\"bar\"}]"},
		}, {
			name: "normal json all fields",
			tty:  false,
			opts: &ListOptions{
				Username: "johndoe",
				Fields:   listFields,
			},
			httpStubs:  defaultHTTPStubs,
			wantStdout: []string{"[{\"login\":\"foo\",\"name\":\"Foo\"},{\"login\":\"bar\",\"name\":\"Bar\"}]"},
		}, {
			name: "failure tty, prompt error",
			tty:  true,
			opts: &ListOptions{},
			prompterStubs: func(t *testing.T, pm *prompter.PrompterMock) {
				pm.RegisterInput("Which user do you want to target?", func(_, def string) (string, error) {
					assert.Empty(t, def)
					return "", errors.New("prompt error")
				})
			},
			wantErr: "prompt error",
		}, {
			name:    "failure no-tty, no-username",
			tty:     false,
			opts:    &ListOptions{},
			wantErr: "username not provided",
		}, {
			name: "normal tty, no sponsor",
			tty:  true,
			opts: &ListOptions{
				Username: "johndoe",
			},
			httpStubs:  emptyRespHTTPStubs,
			wantStderr: "no sponsor found\n",
		}, {
			name: "normal no-tty, no sponsor",
			tty:  false,
			opts: &ListOptions{
				Username: "johndoe",
			},
			httpStubs: emptyRespHTTPStubs,
		}, {
			name: "api error",
			tty:  true,
			opts: &ListOptions{
				Username: "johndoe",
			},
			httpStubs: func(_ *testing.T, mt *mockTransport) {
				mt.respBody = `{"data":{}, "errors": [{"message": "some gql error"}]}`
			},
			wantErr: "GraphQL: some gql error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockTransport := &mockTransport{}
			client, err := api.NewGraphQLClient(api.ClientOptions{
				Host:      "foo",
				AuthToken: "bar",
				Transport: mockTransport,
			})
			require.NoError(t, err)

			pm := &prompter.PrompterMock{}
			if tt.prompterStubs != nil {
				tt.prompterStubs(t, pm)
			}
			tt.opts.Prompter = pm

			ios := &mockTerminal{
				width:  999,
				height: 999,
			}
			ios.isTTY = tt.tty

			tt.opts.IOs = ios
			tt.opts.Client = client

			if tt.httpStubs != nil {
				tt.httpStubs(t, mockTransport)
			}

			err = listRun(tt.opts)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)

			expectedStdout := ""
			if len(tt.wantStdout) > 0 {
				expectedStdout = fmt.Sprintf("%s\n", strings.Join(tt.wantStdout, "\n"))
			}
			assert.Equal(t, expectedStdout, ios.stdout.String())
			assert.Equal(t, tt.wantStderr, ios.stderr.String())
		})
	}
}

type mockTransport struct {
	respBody       string
	respStatusCode int
}

func (t *mockTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	if t.respStatusCode != 0 {
		rec.WriteHeader(t.respStatusCode)
	}
	_, _ = rec.WriteString(t.respBody)
	return rec.Result(), nil
}

type mockTerminal struct {
	stdin  bytes.Buffer
	stdout bytes.Buffer
	stderr bytes.Buffer
	isTTY  bool
	width  int
	height int
}

func (m *mockTerminal) In() io.Reader {
	return &m.stdin
}

func (m *mockTerminal) Out() io.Writer {
	return &m.stdout
}

func (m *mockTerminal) ErrOut() io.Writer {
	return &m.stderr
}

func (m *mockTerminal) IsTerminalOutput() bool {
	return m.isTTY
}

func (m *mockTerminal) Size() (int, int, error) {
	return m.width, m.height, nil
}
