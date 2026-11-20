package oauth

import (
	"errors"
	"testing"
	"time"

	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

func TestRevokeRefreshToken(t *testing.T) {
	f := newFakeAS(t)
	env := testEnv(f)
	p, _ := newProvider(t, env)
	seedToken(t, env, "the-at", "the-rt", 3600, testNow)

	if err := p.Revoke(t.Context()); err != nil {
		t.Fatal(err)
	}
	form := f.last("/revoke").Form
	if form.Get("token") != "the-rt" || form.Get("token_type_hint") != "refresh_token" {
		t.Errorf("revoke request = %v", form)
	}
	if form.Get("client_id") != "cli-client" || form.Has("client_secret") {
		t.Errorf("a public client sends its client_id and no secret: %v", form)
	}
}

func TestRevokeAccessTokenWhenNoRefreshToken(t *testing.T) {
	f := newFakeAS(t)
	env := testEnv(f)
	p, _ := newProvider(t, env)
	seedToken(t, env, "the-at", "", 3600, testNow)

	if err := p.Revoke(t.Context()); err != nil {
		t.Fatal(err)
	}
	form := f.last("/revoke").Form
	if form.Get("token") != "the-at" || form.Get("token_type_hint") != "access_token" {
		t.Errorf("revoke request = %v", form)
	}
}

func TestRevokeExpiredTokenIsStillRevoked(t *testing.T) {
	f := newFakeAS(t)
	env := testEnv(f)
	p, _ := newProvider(t, env)
	seedToken(t, env, "the-at", "the-rt", 60, testNow.Add(-time.Hour))

	if err := p.Revoke(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.hit("/revoke") != 1 {
		t.Errorf("the refresh token of an expired access token should be revoked")
	}
}

func TestRevokeWithClientSecret(t *testing.T) {
	f := newFakeAS(t)
	env := secretEnv(f, "authorization_code")
	env.OAuth.ClientSecret = "s3cret"
	p, _ := newProvider(t, env)
	seedToken(t, env, "the-at", "the-rt", 3600, testNow)

	if err := p.Revoke(t.Context()); err != nil {
		t.Fatal(err)
	}
	form := f.last("/revoke").Form
	if form.Get("client_id") != "cli-client" || form.Get("client_secret") != "s3cret" {
		t.Errorf("revoke request = %v", form)
	}
}

func TestRevokeWithPrivateKeyJWT(t *testing.T) {
	priv, _ := newECKey(t)
	f := newFakeAS(t)
	env := jwtEnv(f, writeKey(t, priv))
	p, _ := newProvider(t, env)
	seedToken(t, env, "the-at", "", 3600, testNow)

	if err := p.Revoke(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.last("/revoke").Form.Get("client_assertion") == "" {
		t.Error("revocation should authenticate with the signed assertion")
	}
}

func TestRevokeIsANoOp(t *testing.T) {
	t.Run("no cached token", func(t *testing.T) {
		f := newFakeAS(t)
		p, _ := newProvider(t, testEnv(f))
		if err := p.Revoke(t.Context()); err != nil {
			t.Fatal(err)
		}
		if f.totalHits() != 0 {
			t.Errorf("nothing to revoke must not touch the network, hits = %d", f.totalHits())
		}
	})

	t.Run("stale token", func(t *testing.T) {
		f := newFakeAS(t)
		env := testEnv(f)
		p, _ := newProvider(t, env)
		other := *env
		other.OAuth.ClientID = "someone-else"
		seedToken(t, &other, "at", "rt", 3600, testNow)

		if err := p.Revoke(t.Context()); err != nil {
			t.Fatal(err)
		}
		if f.totalHits() != 0 {
			t.Errorf("a token for other settings is not ours to revoke, hits = %d", f.totalHits())
		}
	})

	t.Run("no revocation endpoint", func(t *testing.T) {
		f := newFakeAS(t)
		f.noRevocation = true
		env := testEnv(f)
		p, _ := newProvider(t, env)
		seedToken(t, env, "at", "rt", 3600, testNow)

		if err := p.Revoke(t.Context()); err != nil {
			t.Fatal(err)
		}
		if f.hit("/revoke") != 0 {
			t.Error("there is no endpoint to call")
		}
	})

	t.Run("no client secret", func(t *testing.T) {
		f := newFakeAS(t)
		env := secretEnv(f, "authorization_code")
		p, _ := newProvider(t, env)
		seedToken(t, env, "at", "rt", 3600, testNow)

		if err := p.Revoke(t.Context()); err != nil {
			t.Fatal(err)
		}
		if f.hit("/revoke") != 0 {
			t.Error("revocation needs the secret and must be skipped without it")
		}
	})
}

func TestRevokeServerErrorIsReturned(t *testing.T) {
	f := newFakeAS(t)
	f.revokeStatus = 500
	env := testEnv(f)
	p, _ := newProvider(t, env)
	seedToken(t, env, "at", "rt", 3600, testNow)

	if err := p.Revoke(t.Context()); err == nil {
		t.Fatal("want the server error")
	}
	if _, status, _ := profile.LookupToken("test", env, testNow); status != profile.TokenValid {
		t.Errorf("Revoke must leave the cache for the caller, status = %v", status)
	}
}

func TestRevokeRespectsTimeout(t *testing.T) {
	prev := revokeTimeout
	revokeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { revokeTimeout = prev })

	f := newFakeAS(t)
	f.revokeDelay = 5 * time.Second
	env := testEnv(f)
	p, _ := newProvider(t, env)
	seedToken(t, env, "at", "rt", 3600, testNow)

	start := time.Now()
	err := p.Revoke(t.Context())
	if err == nil {
		t.Fatal("want a timeout error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Revoke took %v, the timeout was not applied", elapsed)
	}
}

func TestRevokeInvalidSettings(t *testing.T) {
	f := newFakeAS(t)
	env := testEnv(f)
	env.OAuth.GrantType = "device_code"
	p, _ := newProvider(t, env)

	if err := p.Revoke(t.Context()); !errors.Is(err, ErrUnsupportedGrant) {
		t.Errorf("err = %v, want ErrUnsupportedGrant", err)
	}
}

func TestRevokeLeaksNothingToLogs(t *testing.T) {
	logs := capture(t)
	f := newFakeAS(t)
	env := testEnv(f)
	p, _ := newProvider(t, env)
	seedToken(t, env, "secret-at", "secret-rt", 3600, testNow)

	if err := p.Revoke(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"secret-at", "secret-rt"} {
		mustNotContain(t, logs.String(), s, "logs")
	}
	mustContain(t, logs.String(), "token revoked", "logs")
}
