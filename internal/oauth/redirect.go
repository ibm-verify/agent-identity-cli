package oauth

import (
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
)

const defaultCallbackPath = "/callback"

// redirectPlan says where to listen for the sign-in callback and how to write
// the redirect URI that is sent to the authorization server.
type redirectPlan struct {
	listenHost  string // address to bind: 127.0.0.1 or ::1
	displayHost string // host as it appears in the redirect URI: 127.0.0.1, [::1] or localhost
	port        string // "0" for an ephemeral port
	rawPath     string // path as written, possibly empty
}

// parseRedirect turns oauth.redirect_uri into a plan. Only http loopback
// redirects are accepted, because that is the only kind the CLI can receive.
func parseRedirect(raw string) (*redirectPlan, error) {
	if raw == "" {
		return &redirectPlan{listenHost: "127.0.0.1", displayHost: "127.0.0.1", port: "0", rawPath: defaultCallbackPath}, nil
	}

	invalid := func() error {
		return i18n.Wrap(ErrInvalidAuthConfig, "auth.redirect_invalid", map[string]any{"URI": raw})
	}

	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return nil, invalid()
	}

	plan := &redirectPlan{port: "0", rawPath: u.EscapedPath()}
	switch strings.ToLower(u.Hostname()) {
	case "127.0.0.1":
		plan.listenHost, plan.displayHost = "127.0.0.1", "127.0.0.1"
	case "::1":
		plan.listenHost, plan.displayHost = "::1", "[::1]"
	case "localhost":
		plan.listenHost, plan.displayHost = "127.0.0.1", "localhost"
	default:
		return nil, invalid()
	}

	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 65535 {
			return nil, invalid()
		}
		plan.port = strconv.Itoa(n)
	}
	return plan, nil
}

// handlerPath is the path the callback handler matches. An empty path in the
// redirect URI matches "/".
func (r *redirectPlan) handlerPath() string {
	if r.rawPath == "" {
		return "/"
	}
	if p, err := url.PathUnescape(r.rawPath); err == nil {
		return p
	}
	return r.rawPath
}

// listen binds the loopback address. It never falls back to another port,
// because the redirect URI may be registered with the server.
func (r *redirectPlan) listen() (net.Listener, error) {
	addr := net.JoinHostPort(r.listenHost, r.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, i18n.Wrap(err, "auth.listen_failed", map[string]any{"Address": addr, "Error": err.Error()})
	}
	return ln, nil
}

// host returns the host and port the browser puts in the Host header: the one
// in the redirect URI, with the port the listener got.
func (r *redirectPlan) host(port int) string {
	return r.displayHost + ":" + strconv.Itoa(port)
}

// uri returns the redirect URI to send, with the port the listener got.
func (r *redirectPlan) uri(port int) string {
	return "http://" + r.host(port) + r.rawPath
}
