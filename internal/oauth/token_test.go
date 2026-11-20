package oauth

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

func TestTokenValidCacheMakesNoNetworkCall(t *testing.T) {
	f := newFakeAS(t)
	env := testEnv(f)
	p, _ := newProvider(t, env)
	seedToken(t, env, "cached-at", "cached-rt", 3600, testNow)

	tok, err := p.Token(t.Context())
	if err != nil || tok.AccessToken != "cached-at" {
		t.Fatalf("%v %+v", err, tok)
	}
	if f.totalHits() != 0 {
		t.Errorf("a valid cache must not touch the network, hits = %d", f.totalHits())
	}
}

func TestTokenUnknownExpiryIsValid(t *testing.T) {
	f := newFakeAS(t)
	env := testEnv(f)
	p, _ := newProvider(t, env)
	seedToken(t, env, "cached-at", "", 0, testNow.Add(-100*time.Hour))

	tok, err := p.Token(t.Context())
	if err != nil || tok.AccessToken != "cached-at" || f.totalHits() != 0 {
		t.Fatalf("%v %+v hits=%d", err, tok, f.totalHits())
	}
}

func TestTokenMissingAndStaleNeedLogin(t *testing.T) {
	f := newFakeAS(t)
	env := testEnv(f)
	p, _ := newProvider(t, env)

	if _, err := p.Token(t.Context()); !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("missing: err = %v, want ErrLoginRequired", err)
	}
	mustContain(t, errString(p.Token(t.Context())), "login", "message")

	// A token minted for different settings is stale.
	other := *env
	other.OAuth.ClientID = "someone-else"
	seedToken(t, &other, "at", "rt", 3600, testNow)
	if _, err := p.Token(t.Context()); !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("stale: err = %v, want ErrLoginRequired", err)
	}
	if f.totalHits() != 0 {
		t.Errorf("hits = %d, want none", f.totalHits())
	}
}

func errString(_ *profile.Token, err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestTokenRefreshWithRotation(t *testing.T) {
	f := newFakeAS(t)
	env := testEnv(f)
	p, _ := newProvider(t, env)
	seedToken(t, env, "old-at", "old-rt", 60, testNow.Add(-2*time.Hour))

	tok, err := p.Token(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "at-1" || tok.RefreshToken != "rt-1" {
		t.Errorf("token = %+v", tok)
	}
	form := f.last("/token").Form
	if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "old-rt" || form.Get("client_id") != "cli-client" {
		t.Errorf("refresh request = %v", form)
	}
	if form.Has("client_secret") || form.Has("client_assertion") {
		t.Errorf("a public client sends no credentials on refresh: %v", form)
	}

	cached, status, _ := profile.LookupToken("test", env, testNow)
	if status != profile.TokenValid || cached.RefreshToken != "rt-1" {
		t.Errorf("cache after refresh: %v %+v", status, cached)
	}
}

func TestTokenRefreshKeepsRefreshTokenWhenNotRotated(t *testing.T) {
	f := newFakeAS(t)
	f.tokenFn = func(*recorded) (int, map[string]any) {
		return http.StatusOK, map[string]any{"access_token": "new-at", "token_type": "Bearer", "expires_in": 3600}
	}
	env := testEnv(f)
	p, _ := newProvider(t, env)
	seedToken(t, env, "old-at", "old-rt", 60, testNow.Add(-2*time.Hour))

	tok, err := p.Token(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "new-at" || tok.RefreshToken != "old-rt" {
		t.Errorf("token = %+v, want the old refresh token kept", tok)
	}
}

func TestTokenRefreshInvalidGrantNeedsLogin(t *testing.T) {
	f := newFakeAS(t)
	f.tokenFn = func(*recorded) (int, map[string]any) {
		return http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "expired"}
	}
	env := testEnv(f)
	p, _ := newProvider(t, env)
	seedToken(t, env, "old-at", "old-rt", 60, testNow.Add(-2*time.Hour))

	if _, err := p.Token(t.Context()); !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("err = %v, want ErrLoginRequired", err)
	}
}

func TestTokenRefreshServerErrorIsReturnedAndCacheKept(t *testing.T) {
	f := newFakeAS(t)
	f.tokenFn = func(*recorded) (int, map[string]any) {
		return http.StatusInternalServerError, map[string]any{"error": "server_error"}
	}
	env := testEnv(f)
	p, _ := newProvider(t, env)
	seedToken(t, env, "old-at", "old-rt", 60, testNow.Add(-2*time.Hour))

	_, err := p.Token(t.Context())
	if err == nil || errors.Is(err, ErrLoginRequired) {
		t.Fatalf("err = %v, want the server error and not ErrLoginRequired", err)
	}
	cached, status, _ := profile.LookupToken("test", env, testNow.Add(-2*time.Hour))
	if status != profile.TokenValid && status != profile.TokenExpired || cached == nil || cached.RefreshToken != "old-rt" {
		t.Errorf("the cache must be left alone: %v %+v", status, cached)
	}
}

func TestTokenExpiredWithoutRefreshTokenNeedsLogin(t *testing.T) {
	f := newFakeAS(t)
	env := testEnv(f)
	p, _ := newProvider(t, env)
	seedToken(t, env, "old-at", "", 60, testNow.Add(-2*time.Hour))

	if _, err := p.Token(t.Context()); !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("err = %v, want ErrLoginRequired", err)
	}
	if f.totalHits() != 0 {
		t.Errorf("hits = %d, want none", f.totalHits())
	}
}

func TestTokenClientCredentialsSecret(t *testing.T) {
	t.Run("inline secret in the context", func(t *testing.T) {
		f := newFakeAS(t)
		env := secretEnv(f, "client_credentials")
		env.OAuth.ClientSecret = "s3cret"
		p, _ := newProvider(t, env)

		tok, err := p.Token(t.Context())
		if err != nil || tok.AccessToken != "at-1" {
			t.Fatalf("%v %+v", err, tok)
		}
		form := f.last("/token").Form
		if form.Get("grant_type") != "client_credentials" || form.Get("client_id") != "cli-client" || form.Get("client_secret") != "s3cret" {
			t.Errorf("request = %v", form)
		}
		if _, status, _ := profile.LookupToken("test", env, testNow); status != profile.TokenValid {
			t.Errorf("token should be cached, status = %v", status)
		}
	})

	t.Run("secret from a file in the context", func(t *testing.T) {
		f := newFakeAS(t)
		env := secretEnv(f, "client_credentials")
		path := filepath.Join(t.TempDir(), "secret")
		if err := os.WriteFile(path, []byte("file-secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		env.OAuth.ClientSecret = profile.SecretSource("@" + path)
		p, _ := newProvider(t, env)

		if _, err := p.Token(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := f.last("/token").Form.Get("client_secret"); got != "file-secret" {
			t.Errorf("client_secret = %q, want the file's contents without the trailing newline", got)
		}
	})

	t.Run("unreadable secret file is an error", func(t *testing.T) {
		f := newFakeAS(t)
		env := secretEnv(f, "client_credentials")
		missing := filepath.Join(t.TempDir(), "absent")
		env.OAuth.ClientSecret = profile.SecretSource("@" + missing)
		p, _ := newProvider(t, env)

		_, err := p.Token(t.Context())
		if !errors.Is(err, profile.ErrSourceUnavailable) {
			t.Fatalf("err = %v, want ErrSourceUnavailable", err)
		}
		mustContain(t, err.Error(), missing, "message should name the file")
		if f.hit("/token") != 0 {
			t.Error("no token request without a secret")
		}
	})

	t.Run("secret from the environment", func(t *testing.T) {
		f := newFakeAS(t)
		p, _ := newProvider(t, secretEnv(f, "client_credentials"))
		t.Setenv(EnvClientSecret, "env-secret")

		if _, err := p.Token(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := f.last("/token").Form.Get("client_secret"); got != "env-secret" {
			t.Errorf("client_secret = %q", got)
		}
	})

	t.Run("the context wins over the environment", func(t *testing.T) {
		f := newFakeAS(t)
		env := secretEnv(f, "client_credentials")
		env.OAuth.ClientSecret = "saved-secret"
		p, _ := newProvider(t, env)
		t.Setenv(EnvClientSecret, "env-secret")

		if _, err := p.Token(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := f.last("/token").Form.Get("client_secret"); got != "saved-secret" {
			t.Errorf("client_secret = %q, want the context's secret", got)
		}
	})

	t.Run("no secret needs login", func(t *testing.T) {
		f := newFakeAS(t)
		p, _ := newProvider(t, secretEnv(f, "client_credentials"))

		_, err := p.Token(t.Context())
		if !errors.Is(err, ErrLoginRequired) {
			t.Fatalf("err = %v, want ErrLoginRequired", err)
		}
		mustContain(t, err.Error(), EnvClientSecret, "message should say how to supply the secret")
	})

	t.Run("cached token reused", func(t *testing.T) {
		f := newFakeAS(t)
		env := secretEnv(f, "client_credentials")
		env.OAuth.ClientSecret = "s3cret"
		p, _ := newProvider(t, env)

		if _, err := p.Token(t.Context()); err != nil {
			t.Fatal(err)
		}
		before := f.totalHits()
		if _, err := p.Token(t.Context()); err != nil {
			t.Fatal(err)
		}
		if f.totalHits() != before {
			t.Error("the second call should use the cache")
		}
	})

	t.Run("expired token is minted again", func(t *testing.T) {
		f := newFakeAS(t)
		env := secretEnv(f, "client_credentials")
		env.OAuth.ClientSecret = "s3cret"
		p, _ := newProvider(t, env)
		seedToken(t, env, "old", "", 60, testNow.Add(-time.Hour))

		tok, err := p.Token(t.Context())
		if err != nil || tok.AccessToken != "at-1" {
			t.Fatalf("%v %+v", err, tok)
		}
	})
}

func writeKey(t *testing.T, k *jose.JSONWebKey) string {
	t.Helper()
	data, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "key.jwk")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func jwtEnv(f *fakeAS, keyPath string) *profile.Environment {
	env := testEnv(f)
	env.OAuth.AuthType = "private_key_jwt"
	env.OAuth.GrantType = "client_credentials"
	env.OAuth.PrivateJWK = profile.SecretSource("@" + keyPath)
	return env
}

func TestTokenPrivateKeyJWT(t *testing.T) {
	priv, pub := newECKey(t)
	f := newFakeAS(t)
	env := jwtEnv(f, writeKey(t, priv))
	p, _ := newProvider(t, env)

	tok, err := p.Token(t.Context())
	if err != nil || tok.AccessToken != "at-1" {
		t.Fatalf("%v %+v", err, tok)
	}

	form := f.last("/token").Form
	if form.Get("client_assertion_type") != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" {
		t.Errorf("client_assertion_type = %q", form.Get("client_assertion_type"))
	}
	if form.Get("client_secret") != "" {
		t.Error("a private_key_jwt request must not carry a client_secret")
	}

	assertion := form.Get("client_assertion")
	jws, err := jose.ParseSigned(assertion, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatalf("assertion: %v", err)
	}
	if kid := jws.Signatures[0].Header.KeyID; kid != "kid-1" {
		t.Errorf("kid = %q", kid)
	}
	payload, err := jws.Verify(pub)
	if err != nil {
		t.Fatalf("signature does not verify with the public key: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["iss"] != "cli-client" || claims["sub"] != "cli-client" {
		t.Errorf("claims = %v", claims)
	}
}

func TestTokenPrivateKeyJWTRefreshAndAuthorizationCode(t *testing.T) {
	priv, _ := newECKey(t)
	f := newFakeAS(t)
	env := jwtEnv(f, writeKey(t, priv))
	env.OAuth.GrantType = "authorization_code"
	p, _ := newProvider(t, env)
	seedToken(t, env, "old", "old-rt", 60, testNow.Add(-time.Hour))

	if _, err := p.Token(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.last("/token").Form.Get("client_assertion") == "" {
		t.Error("refresh should authenticate with the signed assertion")
	}
}

func TestPrivateKeyJWTBadKeys(t *testing.T) {
	priv, pub := newECKey(t)

	noKid := *priv
	noKid.KeyID = ""
	noAlg := *priv
	noAlg.Algorithm = ""

	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage")
	_ = os.WriteFile(garbage, []byte("not json"), 0o600)

	cases := map[string]struct {
		path   string
		member string
	}{
		"missing kid":     {writeKey(t, &noKid), "kid"},
		"missing alg":     {writeKey(t, &noAlg), "alg"},
		"public key only": {writeKey(t, pub), "private"},
		"not json":        {garbage, "JSON Web Key"},
		"unreadable":      {filepath.Join(dir, "absent"), "absent"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeAS(t)
			p, _ := newProvider(t, jwtEnv(f, c.path))

			_, err := p.Token(t.Context())
			if err == nil {
				t.Fatal("want an error")
			}
			mustContain(t, err.Error(), c.path, "message should name the key file")
			mustContain(t, err.Error(), c.member, "message")
			if f.hit("/token") != 0 {
				t.Error("no token request with an unusable key")
			}
		})
	}
}

func TestPrivateKeyJWTErrorNeverEchoesKeyMaterial(t *testing.T) {
	priv, _ := newECKey(t)
	noKid := *priv
	noKid.KeyID = ""
	path := writeKey(t, &noKid)
	data, _ := os.ReadFile(path)

	f := newFakeAS(t)
	logs := capture(t)
	p, _ := newProvider(t, jwtEnv(f, path))
	_, err := p.Token(t.Context())
	if err == nil {
		t.Fatal("want an error")
	}

	var jwk map[string]any
	_ = json.Unmarshal(data, &jwk)
	d, _ := jwk["d"].(string)
	if d == "" {
		t.Fatal("test key has no private member")
	}
	mustNotContain(t, err.Error(), d, "error")
	mustNotContain(t, logs.String(), d, "logs")
}

func inlineKey(t *testing.T, k *jose.JSONWebKey) profile.SecretSource {
	t.Helper()
	data, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	return profile.SecretSource(data)
}

func TestTokenPrivateKeyJWTInlineKey(t *testing.T) {
	priv, pub := newECKey(t)
	f := newFakeAS(t)
	env := jwtEnv(f, "")
	env.OAuth.PrivateJWK = inlineKey(t, priv)
	p, _ := newProvider(t, env)

	if _, err := p.Token(t.Context()); err != nil {
		t.Fatal(err)
	}
	jws, err := jose.ParseSigned(f.last("/token").Form.Get("client_assertion"), []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jws.Verify(pub); err != nil {
		t.Fatalf("assertion does not verify: %v", err)
	}
}

func TestPrivateKeyJWTInlineKeyErrorsNameTheProperty(t *testing.T) {
	priv, _ := newECKey(t)
	noKid := *priv
	noKid.KeyID = ""

	cases := map[string]struct {
		key    profile.SecretSource
		member string
	}{
		"missing kid": {inlineKey(t, &noKid), "kid"},
		"not json":    {"not json", "JSON Web Key"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeAS(t)
			env := jwtEnv(f, "")
			env.OAuth.PrivateJWK = c.key
			p, _ := newProvider(t, env)

			_, err := p.Token(t.Context())
			if err == nil {
				t.Fatal("want an error")
			}
			mustContain(t, err.Error(), "oauth.private_jwk", "message should name the property")
			mustContain(t, err.Error(), c.member, "message")
			mustNotContain(t, err.Error(), "not json", "message must not echo the value")
			if d := string(c.key); strings.Contains(err.Error(), d) {
				t.Error("error must not echo the inline key")
			}
		})
	}
}

func TestTokenLeaksNothingToLogs(t *testing.T) {
	logs := capture(t)
	f := newFakeAS(t)
	env := secretEnv(f, "client_credentials")
	env.OAuth.ClientSecret = "very-secret-value"
	p, _ := newProvider(t, env)

	if _, err := p.Token(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"very-secret-value", "at-1"} {
		mustNotContain(t, logs.String(), s, "logs")
	}
	if !strings.Contains(logs.String(), "/token") {
		t.Errorf("requests should be logged: %q", logs.String())
	}
}

func TestTokenCacheWriteFailureReturnsTokenAndError(t *testing.T) {
	f := newFakeAS(t)
	env := secretEnv(f, "client_credentials")
	env.OAuth.ClientSecret = "s3cret"
	p, _ := newProvider(t, env)

	// Make the cache directory a file so the cache cannot be written.
	dir, err := profile.CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	tok, err := p.Token(t.Context())
	if err == nil {
		t.Fatal("want a cache write error")
	}
	if tok == nil || tok.AccessToken != "at-1" {
		t.Errorf("the token should still be returned: %+v", tok)
	}
}
