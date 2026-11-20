package oauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

// LoginOptions tunes an interactive login.
type LoginOptions struct {
	// NoBrowser prints the sign-in URL and does not try to open it.
	NoBrowser bool

	// NoSave returns the token without writing the cache.
	NoSave bool

	// Timeout is how long to wait for the sign-in callback. The default is 5 minutes.
	Timeout time.Duration
}

// Login gets a new token for the context and, unless opts.NoSave is set,
// caches it. authorization_code opens the browser and waits for the callback on
// a loopback listener. client_credentials needs no interaction.
//
// If the token was obtained but could not be cached, it is returned together
// with the error.
func (p *Provider) Login(ctx context.Context, opts LoginOptions) (*profile.Token, error) {
	grant, err := p.validate()
	if err != nil {
		return nil, err
	}

	oc := p.env().OAuth
	slog.Info("login started", "context", p.Context, "grant", grant, "issuer", oc.Issuer, "client_id", oc.ClientID)

	var tok *profile.Token
	if grant == grantClientCredentials {
		tok, err = p.mint(ctx)
	} else {
		tok, err = p.interactiveLogin(ctx, opts)
	}
	if errors.Is(err, errNoSecret) {
		return nil, p.secretMissing()
	}
	if err != nil {
		return nil, err
	}

	slog.Info("login completed", "context", p.Context, "grant", grant)
	if opts.NoSave {
		return tok, nil
	}
	return p.save(tok)
}

// interactiveLogin runs the authorization code + PKCE flow.
func (p *Provider) interactiveLogin(ctx context.Context, opts LoginOptions) (*profile.Token, error) {
	oc := p.env().OAuth

	plan, err := parseRedirect(oc.RedirectURI)
	if err != nil {
		return nil, err
	}
	hc, err := p.httpClient()
	if err != nil {
		return nil, err
	}
	cfg, err := discover(ctx, hc, oc.Issuer, true)
	if err != nil {
		return nil, err
	}
	ca, err := p.clientAuth(cfg)
	if err != nil {
		return nil, err
	}

	// The listener comes first because the redirect URI carries its port.
	ln, err := plan.listen()
	if err != nil {
		return nil, err
	}
	defer ln.Close() // the server closes it too; this covers the early returns
	port := ln.Addr().(*net.TCPAddr).Port
	slog.Debug("listening for the sign-in callback", "address", ln.Addr().String())

	client, err := p.sdkClient(cfg, ca, plan.uri(port))
	if err != nil {
		return nil, err
	}
	az, err := client.AuthorizeWithBrowserFlow(ctx, nil)
	if err != nil {
		return nil, err
	}

	srv := newCallbackServer(ln, plan.host(port), plan.handlerPath(), az.State, oc.Issuer)
	defer srv.Close()
	srv.start()

	slog.Debug("authorization URL built", "url", safeURL(az.AuthCodeURL))
	p.prompt(az.AuthCodeURL, opts.NoBrowser)

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultLoginTimeout
	}
	res, err := srv.wait(ctx, timeout)
	if err != nil {
		return nil, err
	}
	if res.issuerMismatch {
		return nil, i18n.Errorf("auth.callback_issuer_mismatch", map[string]any{"Issuer": oc.Issuer})
	}

	tr, err := client.TokenWithAuthCode(ctx, az, res.params)
	if err != nil {
		return nil, err
	}
	return p.tokenFromResponse(tr, ""), nil
}

// prompt tells the user what is happening and opens the browser. A browser
// that cannot be opened is not an error: the URL has been printed, and the wait
// carries on.
func (p *Provider) prompt(authURL string, noBrowser bool) {
	out := p.out()
	if noBrowser {
		fmt.Fprintln(out, i18n.T("auth.prompt_visit", nil))
	} else {
		fmt.Fprintln(out, i18n.T("auth.prompt_opening", nil))
	}
	fmt.Fprintln(out, authURL)

	if !noBrowser {
		if err := p.opener()(authURL); err != nil {
			slog.Debug("could not open the browser", "error", err)
		} else {
			slog.Debug("browser opened")
		}
	}
	fmt.Fprintln(out, i18n.T("auth.prompt_waiting", nil))
}
