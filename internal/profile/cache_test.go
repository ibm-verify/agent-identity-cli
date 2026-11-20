package profile

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var baseTime = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func cacheEnv() *Environment {
	return &Environment{
		Server: "https://corp.internal",
		OAuth: OAuthConfig{
			Issuer:    "https://idp.example",
			ClientID:  "cid",
			GrantType: "authorization_code",
			Scopes:    []string{"openid", "offline_access"},
		},
	}
}

func setupCache(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func TestNewToken(t *testing.T) {
	env := cacheEnv()

	tok := NewToken(env, "at", "rt", "", 3600, baseTime)
	if want := baseTime.Add(time.Hour); !tok.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", tok.ExpiresAt, want)
	}
	if tok.ExpiresAt.Location() != time.UTC {
		t.Errorf("ExpiresAt should be UTC, got %v", tok.ExpiresAt.Location())
	}
	if tok.TokenType != "Bearer" {
		t.Errorf("TokenType = %q, want Bearer", tok.TokenType)
	}
	if tok.ConfigHash != ConfigHash(env) {
		t.Error("ConfigHash not stamped from env")
	}

	if got := NewToken(env, "at", "", "DPoP", 60, baseTime).TokenType; got != "DPoP" {
		t.Errorf("explicit token type lost: %q", got)
	}

	for _, n := range []int64{0, -5} {
		if tok := NewToken(env, "at", "", "Bearer", n, baseTime); !tok.ExpiresAt.IsZero() {
			t.Errorf("expiresIn %d: ExpiresAt = %v, want zero", n, tok.ExpiresAt)
		}
	}
}

func TestExpired(t *testing.T) {
	tok := &Token{ExpiresAt: baseTime}

	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"well before", baseTime.Add(-time.Hour), false},
		{"just outside skew", baseTime.Add(-31 * time.Second), false},
		{"exactly at skew", baseTime.Add(-30 * time.Second), true},
		{"inside skew", baseTime.Add(-time.Second), true},
		{"at expiry", baseTime, true},
		{"after", baseTime.Add(time.Hour), true},
	}
	for _, c := range cases {
		if got := tok.Expired(c.now); got != c.want {
			t.Errorf("%s: Expired = %v, want %v", c.name, got, c.want)
		}
	}

	if (&Token{}).Expired(baseTime.Add(1000 * time.Hour)) {
		t.Error("a token with no known expiry must never be expired")
	}
}

func TestConfigHash(t *testing.T) {
	base := ConfigHash(cacheEnv())
	if !strings.HasPrefix(base, "sha256:") || len(base) != len("sha256:")+64 {
		t.Fatalf("unexpected hash format: %q", base)
	}
	if ConfigHash(cacheEnv()) != base {
		t.Error("hash is not deterministic")
	}

	changes := map[string]func(e *Environment){
		"server":    func(e *Environment) { e.Server = "https://other" },
		"issuer":    func(e *Environment) { e.OAuth.Issuer = "https://other" },
		"client_id": func(e *Environment) { e.OAuth.ClientID = "other" },
		"grant":     func(e *Environment) { e.OAuth.GrantType = "device_code" },
		"scopes":    func(e *Environment) { e.OAuth.Scopes = []string{"openid"} },
	}
	for name, mutate := range changes {
		e := cacheEnv()
		mutate(e)
		if ConfigHash(e) == base {
			t.Errorf("changing %s did not change the hash", name)
		}
	}

	same := map[string]func(e *Environment){
		"scope order":   func(e *Environment) { e.OAuth.Scopes = []string{"offline_access", "openid"} },
		"scope dupes":   func(e *Environment) { e.OAuth.Scopes = []string{"openid", "openid", "offline_access"} },
		"default grant": func(e *Environment) { e.OAuth.GrantType = "" },
		"auth_type":     func(e *Environment) { e.OAuth.AuthType = "private_key_jwt" },
		"private_jwk":   func(e *Environment) { e.OAuth.PrivateJWK = "@/tmp/jwk.json" },
		"client_secret": func(e *Environment) { e.OAuth.ClientSecret = "s3cret" },
		"redirect_uri":  func(e *Environment) { e.OAuth.RedirectURI = "http://127.0.0.1:9999" },
		"tls":           func(e *Environment) { e.TLS = TLSConfig{InsecureSkipVerify: true, CACert: "/ca.pem"} },
	}
	for name, mutate := range same {
		e := cacheEnv()
		mutate(e)
		if got := ConfigHash(e); got != base {
			t.Errorf("changing %s should not change the hash", name)
		}
	}

	// Field boundaries are preserved.
	a := &Environment{Server: "a", OAuth: OAuthConfig{Issuer: "bc"}}
	b := &Environment{Server: "ab", OAuth: OAuthConfig{Issuer: "c"}}
	if ConfigHash(a) == ConfigHash(b) {
		t.Error("values running into the next field must hash differently")
	}

	// Nil and empty scopes agree, and the input is not mutated.
	e := cacheEnv()
	e.OAuth.Scopes = []string{"b", "a", "a"}
	ConfigHash(e)
	if strings.Join(e.OAuth.Scopes, ",") != "b,a,a" {
		t.Errorf("ConfigHash mutated its input: %v", e.OAuth.Scopes)
	}
	if ConfigHash(&Environment{}) != ConfigHash(nil) {
		t.Error("nil environment should hash like an empty one")
	}
}

func TestSaveLookupRoundTrip(t *testing.T) {
	setupCache(t)
	env := cacheEnv()
	tok := NewToken(env, "at", "rt", "Bearer", 3600, baseTime)

	if err := SaveToken("prod", tok); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	got, status, err := LookupToken("prod", env, baseTime)
	if err != nil || status != TokenValid {
		t.Fatalf("LookupToken = %v, %v", status, err)
	}
	if got.AccessToken != tok.AccessToken || got.RefreshToken != tok.RefreshToken ||
		got.ConfigHash != tok.ConfigHash || got.TokenType != tok.TokenType || !got.ExpiresAt.Equal(tok.ExpiresAt) {
		t.Errorf("round trip changed the token: %+v vs %+v", got, tok)
	}
}

func TestSavedJSONLayout(t *testing.T) {
	setupCache(t)
	if err := SaveToken("c", NewToken(cacheEnv(), "at", "", "Bearer", 60, baseTime)); err != nil {
		t.Fatal(err)
	}
	path, _ := TokenPath("c")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{`"config_hash"`, `"access_token"`, `"token_type"`, `"expires_at": "2026-01-02T03:05:05Z"`} {
		if !strings.Contains(s, want) {
			t.Errorf("saved JSON missing %s:\n%s", want, s)
		}
	}
	if strings.Contains(s, "refresh_token") {
		t.Errorf("empty refresh_token should be omitted:\n%s", s)
	}

	// An unknown expiry is omitted, and still reads back as a valid token.
	if err := SaveToken("c", NewToken(cacheEnv(), "at", "", "Bearer", 0, baseTime)); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "expires_at") {
		t.Errorf("zero expires_at should be omitted:\n%s", data)
	}
	if _, status, _ := LookupToken("c", cacheEnv(), baseTime.Add(1000*time.Hour)); status != TokenValid {
		t.Errorf("unknown-expiry token status = %v, want TokenValid", status)
	}
}

func TestSaveTokenPermissionsAndNoTempFiles(t *testing.T) {
	setupCache(t)
	if err := SaveToken("prod", NewToken(cacheEnv(), "at", "", "Bearer", 60, baseTime)); err != nil {
		t.Fatal(err)
	}
	path, _ := TokenPath("prod")

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file perm = %o, want 600", fi.Mode().Perm())
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("cache dir perm = %o, want 700", di.Mode().Perm())
	}

	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("leftover temp file %s", e.Name())
		}
	}
}

func TestLookupStatuses(t *testing.T) {
	setupCache(t)
	env := cacheEnv()

	// Missing.
	if tok, status, err := LookupToken("none", env, baseTime); tok != nil || status != TokenMissing || err != nil {
		t.Errorf("missing: %v, %v, %v", tok, status, err)
	}

	// Valid, then expired (token returned so the caller can use the refresh token).
	if err := SaveToken("a", NewToken(env, "at", "rt", "Bearer", 3600, baseTime)); err != nil {
		t.Fatal(err)
	}
	if _, status, _ := LookupToken("a", env, baseTime); status != TokenValid {
		t.Errorf("valid: status = %v", status)
	}
	tok, status, err := LookupToken("a", env, baseTime.Add(2*time.Hour))
	if err != nil || status != TokenExpired || tok == nil || tok.RefreshToken != "rt" {
		t.Errorf("expired: %+v, %v, %v", tok, status, err)
	}

	// Stale: the context changed after the token was minted.
	changed := cacheEnv()
	changed.OAuth.ClientID = "different"
	if tok, status, err := LookupToken("a", changed, baseTime); tok != nil || status != TokenStale || err != nil {
		t.Errorf("stale: %v, %v, %v", tok, status, err)
	}
}

func TestLookupCorruptFiles(t *testing.T) {
	setupCache(t)
	env := cacheEnv()

	for name, content := range map[string]string{
		"notjson":   "this is not json",
		"empty":     "",
		"noaccess":  `{"config_hash":"x"}`,
		"truncated": `{"access_token": "abc`,
	} {
		path, err := TokenPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		tok, status, err := LookupToken(name, env, baseTime)
		if tok != nil || status != TokenMissing || err != nil {
			t.Errorf("%s: %v, %v, %v", name, tok, status, err)
		}
	}

	// A corrupt file is overwritten by the next save.
	if err := SaveToken("notjson", NewToken(env, "at", "", "Bearer", 60, baseTime)); err != nil {
		t.Fatal(err)
	}
	if _, status, _ := LookupToken("notjson", env, baseTime); status != TokenValid {
		t.Errorf("after overwrite: status = %v", status)
	}
}

func TestInvalidNamesRejectedEverywhere(t *testing.T) {
	setupCache(t)
	env := cacheEnv()
	tok := NewToken(env, "at", "", "Bearer", 60, baseTime)

	for _, name := range []string{"", "../x", "a/b", `a\b`, ".hidden", strings.Repeat("a", 65)} {
		if _, err := TokenPath(name); !errors.Is(err, ErrInvalidContextName) {
			t.Errorf("TokenPath(%q) err = %v", name, err)
		}
		if err := SaveToken(name, tok); !errors.Is(err, ErrInvalidContextName) {
			t.Errorf("SaveToken(%q) err = %v", name, err)
		}
		if _, _, err := LookupToken(name, env, baseTime); !errors.Is(err, ErrInvalidContextName) {
			t.Errorf("LookupToken(%q) err = %v", name, err)
		}
		if err := DeleteToken(name); !errors.Is(err, ErrInvalidContextName) {
			t.Errorf("DeleteToken(%q) err = %v", name, err)
		}
		if err := RenameToken(name, "ok"); !errors.Is(err, ErrInvalidContextName) {
			t.Errorf("RenameToken(%q, ok) err = %v", name, err)
		}
		if err := RenameToken("ok", name); !errors.Is(err, ErrInvalidContextName) {
			t.Errorf("RenameToken(ok, %q) err = %v", name, err)
		}
	}

	// Nothing was written anywhere under the state directory.
	dir, _ := Dir()
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("rejected names created state: %v", err)
	}
}

func TestDeleteToken(t *testing.T) {
	setupCache(t)
	env := cacheEnv()

	// Missing directory and file are fine.
	if err := DeleteToken("a"); err != nil {
		t.Errorf("DeleteToken on empty state: %v", err)
	}

	if err := SaveToken("a", NewToken(env, "at", "", "Bearer", 60, baseTime)); err != nil {
		t.Fatal(err)
	}
	if err := DeleteToken("a"); err != nil {
		t.Fatalf("DeleteToken: %v", err)
	}
	if _, status, _ := LookupToken("a", env, baseTime); status != TokenMissing {
		t.Errorf("status after delete = %v", status)
	}
	if err := DeleteToken("a"); err != nil {
		t.Errorf("second DeleteToken: %v", err)
	}
}

func TestDeleteAllTokens(t *testing.T) {
	setupCache(t)
	env := cacheEnv()

	if err := DeleteAllTokens(); err != nil {
		t.Errorf("DeleteAllTokens with no cache dir: %v", err)
	}

	for _, n := range []string{"a", "b"} {
		if err := SaveToken(n, NewToken(env, "at", "", "Bearer", 60, baseTime)); err != nil {
			t.Fatal(err)
		}
	}
	dir, _ := CacheDir()
	other := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(other, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(dir, "sub.json")
	if err := os.Mkdir(subdir, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := DeleteAllTokens(); err != nil {
		t.Fatalf("DeleteAllTokens: %v", err)
	}
	for _, n := range []string{"a", "b"} {
		if _, status, _ := LookupToken(n, env, baseTime); status != TokenMissing {
			t.Errorf("%s still cached", n)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("non-json file removed: %v", err)
	}
	if _, err := os.Stat(subdir); err != nil {
		t.Errorf("directory removed: %v", err)
	}
}

func TestRenameToken(t *testing.T) {
	setupCache(t)
	env := cacheEnv()

	// Missing source is fine.
	if err := RenameToken("old", "new"); err != nil {
		t.Errorf("RenameToken with no source: %v", err)
	}

	if err := SaveToken("old", NewToken(env, "old-at", "", "Bearer", 60, baseTime)); err != nil {
		t.Fatal(err)
	}
	if err := SaveToken("new", NewToken(env, "stale-at", "", "Bearer", 60, baseTime)); err != nil {
		t.Fatal(err)
	}

	if err := RenameToken("old", "new"); err != nil {
		t.Fatalf("RenameToken: %v", err)
	}
	tok, status, _ := LookupToken("new", env, baseTime)
	if status != TokenValid || tok.AccessToken != "old-at" {
		t.Errorf("target not replaced: %+v, %v", tok, status)
	}
	if _, status, _ := LookupToken("old", env, baseTime); status != TokenMissing {
		t.Errorf("source still present: %v", status)
	}

	if err := RenameToken("new", "new"); err != nil {
		t.Errorf("rename to self: %v", err)
	}
	if _, status, _ := LookupToken("new", env, baseTime); status != TokenValid {
		t.Errorf("rename to self lost the file: %v", status)
	}
}

// capture routes slog output to a buffer for the duration of the test.
func capture(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestNoTokenLeaks(t *testing.T) {
	setupCache(t)
	logs := capture(t)
	env := cacheEnv()
	const at, rt = "SECRET-ACCESS-TOKEN", "SECRET-REFRESH-TOKEN"

	var errTexts []string
	note := func(err error) {
		if err != nil {
			errTexts = append(errTexts, err.Error())
		}
	}

	note(SaveToken("a", NewToken(env, at, rt, "Bearer", 3600, baseTime)))
	for _, now := range []time.Time{baseTime, baseTime.Add(2 * time.Hour)} {
		_, _, err := LookupToken("a", env, now)
		note(err)
	}
	changed := cacheEnv()
	changed.Server = "https://other"
	_, _, err := LookupToken("a", changed, baseTime)
	note(err)

	path, _ := TokenPath("corrupt")
	_ = os.WriteFile(path, []byte("{"+at), 0o600)
	_, _, err = LookupToken("corrupt", env, baseTime)
	note(err)

	note(RenameToken("a", "b"))
	note(DeleteToken("b"))
	note(DeleteAllTokens())

	all := logs.String() + strings.Join(errTexts, "\n")
	for _, secret := range []string{at, rt} {
		if strings.Contains(all, secret) {
			t.Errorf("token value %q leaked into logs or errors:\n%s", secret, all)
		}
	}
	if !strings.Contains(logs.String(), "token cache") {
		t.Errorf("expected cache log lines, got:\n%s", logs.String())
	}
}
