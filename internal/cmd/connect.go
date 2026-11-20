package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
	"github.com/ibm-verify/agent-identity-cli/internal/input"
	"github.com/ibm-verify/agent-identity-cli/internal/oauth"
	"github.com/ibm-verify/agent-identity-cli/internal/profile"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

// The stateless mode: a server and a bearer token from the environment, with
// no saved context. See internal/resource/spec.md §7.1.
const (
	envServer = "IBM_AGENTID_SERVER"
	envToken  = "IBM_AGENTID_TOKEN"
)

// connection is what a resource call needs, and how it was made.
type connection struct {
	resource.Conn
	stateless bool
}

// connect resolves the server and a valid token for a resource command. An
// explicit --context uses that saved context. Otherwise, when IBM_AGENTID_SERVER
// or IBM_AGENTID_TOKEN is set, both are used and no state is read. Otherwise
// the saved context is resolved as for every command.
func connect(cmd *cobra.Command, opts *options) (*connection, error) {
	server, token := os.Getenv(envServer), os.Getenv(envToken)
	if opts.context == "" && (server != "" || token != "") {
		return statelessConnection(server, token)
	}
	return savedConnection(cmd, opts)
}

func statelessConnection(server, token string) (*connection, error) {
	if server == "" {
		return nil, i18n.Wrap(input.ErrInvalidInput, "resource.env_missing", map[string]any{"Missing": envServer})
	}
	if token == "" {
		return nil, i18n.Wrap(input.ErrInvalidInput, "resource.env_missing", map[string]any{"Missing": envToken})
	}
	if !input.ValidServer(server) {
		return nil, i18n.Wrap(input.ErrInvalidInput, "resource.invalid_env_server", map[string]any{"Name": envServer})
	}
	if strings.ContainsFunc(token, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return nil, i18n.Wrap(input.ErrInvalidInput, "resource.invalid_token", map[string]any{"Name": envToken})
	}

	hc, err := oauth.NewHTTPClient(profile.TLSConfig{})
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(server)
	slog.Info("stateless mode", "host", u.Host)
	return &connection{
		Conn:      resource.Conn{Server: strings.TrimRight(server, "/"), Token: token, HTTP: hc},
		stateless: true,
	}, nil
}

func savedConnection(cmd *cobra.Command, opts *options) (*connection, error) {
	cfg, err := profile.Load()
	if err != nil {
		return nil, err
	}
	name, env, err := cfg.Resolve(opts.context)
	if errors.Is(err, profile.ErrContextNotFound) {
		return nil, i18n.Wrap(err, "resource.no_context", map[string]any{"Name": name})
	}
	if err != nil {
		return nil, err
	}

	errOut := cmd.ErrOrStderr()
	p := &oauth.Provider{Context: name, Env: env, Out: errOut}
	tok, err := p.Token(cmd.Context())
	if tok == nil {
		return nil, err
	}
	if err != nil {
		// The token could not be cached, but it is good for this command.
		fmt.Fprintln(errOut, err)
	}

	hc, err := oauth.NewHTTPClient(env.TLS)
	if err != nil {
		return nil, err
	}
	slog.Info("resource context", "context", name)
	return &connection{
		Conn: resource.Conn{Server: strings.TrimRight(env.Server, "/"), Token: tok.AccessToken, HTTP: hc},
	}, nil
}

// explain adds a hint to a rejected token in stateless mode, where the CLI
// cannot refresh it, and returns err unchanged.
func (c *connection) explain(cmd *cobra.Command, err error) error {
	if err != nil && c.stateless && errors.Is(err, resource.ErrUnauthorized) {
		fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("resource.token_hint", map[string]any{"Name": envToken}))
	}
	return err
}
