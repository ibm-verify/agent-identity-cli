package oauth

import (
	"testing"
)

func TestDiscoveryTrailingSlashOnIssuer(t *testing.T) {
	f := newFakeAS(t)
	env := testEnv(f)
	env.OAuth.Issuer = f.url() + "/"
	env.OAuth.AuthType = "client_secret_post"
	env.OAuth.GrantType = "client_credentials"
	env.OAuth.ClientSecret = "s3cret"
	p, _ := newProvider(t, env)

	if _, err := p.Token(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.hit("/.well-known/openid-configuration") != 1 {
		t.Error("discovery should resolve with a trailing slash on the configured issuer")
	}
}

func TestDiscoveryIssuerMismatchIsRejected(t *testing.T) {
	f := newFakeAS(t)
	f.issuerOverride = "https://other.example"
	env := secretEnv(f, "client_credentials")
	env.OAuth.ClientSecret = "s3cret"
	p, _ := newProvider(t, env)

	_, err := p.Token(t.Context())
	if err == nil {
		t.Fatal("want an issuer mismatch error")
	}
	mustContain(t, err.Error(), "https://other.example", "message should name the reported issuer")
	mustContain(t, err.Error(), f.url(), "message should name the configured issuer")
	if f.hit("/token") != 0 {
		t.Error("no token request against a server that reports another issuer")
	}
}

func TestDiscoveryMissingAuthorizationEndpoint(t *testing.T) {
	f := newFakeAS(t)
	f.omitAuthzEP = true
	p, _ := newProvider(t, testEnv(f))
	p.OpenBrowser = browser(t, nil, nil)

	_, err := p.Login(t.Context(), LoginOptions{})
	if err == nil {
		t.Fatal("want a missing endpoint error")
	}
	mustContain(t, err.Error(), "authorization_endpoint", "message")
}

func TestDiscoveryNotNeededForClientCredentialsWithoutAuthorizationEndpoint(t *testing.T) {
	f := newFakeAS(t)
	f.omitAuthzEP = true
	env := secretEnv(f, "client_credentials")
	env.OAuth.ClientSecret = "s3cret"
	p, _ := newProvider(t, env)

	if _, err := p.Token(t.Context()); err != nil {
		t.Fatalf("client_credentials does not use the authorization endpoint: %v", err)
	}
}

func TestDiscoveryFailureNamesTheIssuer(t *testing.T) {
	f := newFakeAS(t)
	env := secretEnv(f, "client_credentials")
	env.OAuth.Issuer = f.url() + "/missing"
	env.OAuth.ClientSecret = "s3cret"
	p, _ := newProvider(t, env)

	_, err := p.Token(t.Context())
	if err == nil {
		t.Fatal("want a discovery error")
	}
	mustContain(t, err.Error(), f.url()+"/missing", "message")
}

func TestDiscoveryIsSkippedWhenCacheIsValid(t *testing.T) {
	f := newFakeAS(t)
	env := testEnv(f)
	p, _ := newProvider(t, env)
	seedToken(t, env, "at", "rt", 3600, testNow)

	if _, err := p.Token(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.hit("/.well-known/openid-configuration") != 0 {
		t.Error("a valid cached token must not trigger discovery")
	}
}

func TestSafeURLDropsQueryAndFragment(t *testing.T) {
	got := safeURL("https://idp.example/authorize?state=abc&code_challenge=xyz#frag")
	if got != "https://idp.example/authorize" {
		t.Errorf("safeURL = %q", got)
	}
}
