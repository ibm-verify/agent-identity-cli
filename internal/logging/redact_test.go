package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestIsSensitiveKey(t *testing.T) {
	sensitive := []string{
		"token", "access_token", "refresh_token", "id_token", "client_secret",
		"client_assertion", "assertion", "code", "device_code", "code_verifier",
		"password", "secret", "authorization", "proxy_authorization", "cookie",
		"set_cookie", "key", "private_key", "jwk", "private_jwk",
		// normalization: case, dashes, group qualifiers
		"Authorization", "Proxy-Authorization", "Set-Cookie", "CLIENT_SECRET",
		"oauth.client_secret", "oauth.tls.key",
		// suffix rule
		"x_api_key", "X-Api-Key", "session_token", "db_password", "saml_assertion", "signing_key",
	}
	for _, k := range sensitive {
		if !isSensitiveKey(k) {
			t.Errorf("%q should be sensitive", k)
		}
	}

	harmless := []string{
		"token_type", "grant_type", "scope", "expires_in", "status", "host", "path",
		"method", "context", "Content-Type", "X-Correlation-ID", "keyboard", "monkey", "correlation_id",
	}
	for _, k := range harmless {
		if isSensitiveKey(k) {
			t.Errorf("%q should not be sensitive", k)
		}
	}
}

func TestRedactValueKeepsAuthScheme(t *testing.T) {
	tests := []struct{ key, value, want string }{
		{"Authorization", "Bearer abc123", "Bearer [REDACTED]"},
		{"authorization", "basic dXNlcjpwYXNz", "basic [REDACTED]"},
		{"Proxy-Authorization", "Basic xyz", "Basic [REDACTED]"},
		{"Authorization", "opaque-credential", "[REDACTED]"}, // unknown scheme: mask everything
		{"Authorization", "", "[REDACTED]"},
		{"client_secret", "Bearer looks-like-a-scheme", "[REDACTED]"},
	}
	for _, tt := range tests {
		if got := redactValue(tt.key, tt.value); got != tt.want {
			t.Errorf("redactValue(%q, %q) = %q, want %q", tt.key, tt.value, got, tt.want)
		}
	}
}

func TestRedactText(t *testing.T) {
	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJ1c2VyIn0.c2lnbmF0dXJl"

	tests := []struct {
		name, in string
		want     string // substring that must be present
		gone     string // substring that must be absent
	}{
		{"bearer", "Authorization: Bearer abcDEF123.xyz", "Bearer [REDACTED]", "abcDEF123"},
		{"basic", "sent Basic dXNlcjpwYXNzd29yZA== to server", "Basic [REDACTED]", "dXNlcjpw"},
		{"jwt", "token is " + jwt + " ok", "[JWT REDACTED]", "eyJzdWIi"},
		{"kv in url", "POST http://h/token?code=SECRETCODE&state=abc", "code=[REDACTED]&state=abc", "SECRETCODE"},
		{"kv client_secret", "client_secret=hunter2 grant_type=x", "client_secret=[REDACTED] grant_type=x", "hunter2"},
		{"json pair", `{"access_token":"SECRETAT","token_type":"Bearer"}`, `"access_token":"[REDACTED]"`, "SECRETAT"},
		{"json pair spaced", `{"refresh_token" : "SECRETRT"}`, `"refresh_token":"[REDACTED]"`, "SECRETRT"},
		{"suffix key", "x-api-key=ABC123", "x-api-key=[REDACTED]", "ABC123"},
		{"plain text untouched", "context resolved from environment", "context resolved from environment", ""},
		{"non-sensitive pair untouched", "grant_type=client_credentials", "grant_type=client_credentials", ""},
		{"empty", "", "", ""},
	}
	for _, tt := range tests {
		got := redactText(tt.in)
		if tt.want != "" && !strings.Contains(got, tt.want) {
			t.Errorf("%s: %q should contain %q", tt.name, got, tt.want)
		}
		if tt.gone != "" && strings.Contains(got, tt.gone) {
			t.Errorf("%s: %q must not contain %q", tt.name, got, tt.gone)
		}
	}
}

func TestRedactForm(t *testing.T) {
	got := redactForm("grant_type=authorization_code&code=SECRETCODE&client_secret=S3&redirect_uri=http%3A%2F%2F127.0.0.1%3A5000&scope=openid+offline_access")

	for _, secret := range []string{"SECRETCODE", "S3"} {
		if strings.Contains(got, secret) {
			t.Errorf("%q leaked: %s", secret, got)
		}
	}
	for _, keep := range []string{"grant_type=authorization_code", "code=[REDACTED]", "client_secret=[REDACTED]", "scope=openid+offline_access"} {
		if !strings.Contains(got, keep) {
			t.Errorf("%q should be kept in %s", keep, got)
		}
	}

	// Repeated keys are all redacted.
	got = redactForm("code=A1&code=B2")
	if strings.Contains(got, "A1") || strings.Contains(got, "B2") {
		t.Errorf("repeated key leaked: %s", got)
	}

	if redactForm("") != "" {
		t.Error("empty form should stay empty")
	}
}

func TestRedactJSON(t *testing.T) {
	in := `{
		"access_token": "SECRETAT",
		"token_type": "Bearer",
		"expires_in": 300,
		"user": {"name": "ada", "password": "pw1", "api_key": "AK1"},
		"clients": [{"client_id": "c1", "client_secret": "CS1"}, {"client_id": "c2"}],
		"note": "sent Bearer abcDEF123 earlier",
		"jwk": {"kty": "RSA", "d": "PRIVATE"}
	}`
	got, ok := redactJSON([]byte(in))
	if !ok {
		t.Fatal("valid JSON rejected")
	}

	for _, leaked := range []string{"SECRETAT", "pw1", "AK1", "CS1", "abcDEF123", "PRIVATE"} {
		if strings.Contains(got, leaked) {
			t.Errorf("%q leaked: %s", leaked, got)
		}
	}
	for _, keep := range []string{`"token_type":"Bearer"`, `"expires_in":300`, `"name":"ada"`, `"client_id":"c1"`, `"client_id":"c2"`} {
		if !strings.Contains(got, keep) {
			t.Errorf("%s should be kept in %s", keep, got)
		}
	}
	if strings.Contains(got, "\n") {
		t.Errorf("output should be compact: %q", got)
	}
}

func TestRedactJSONRejectsNonJSON(t *testing.T) {
	for _, in := range []string{`not json`, `{"a":1} trailing`, `{"a":`, ``} {
		if _, ok := redactJSON([]byte(in)); ok {
			t.Errorf("%q should not parse", in)
		}
	}
	// Arrays and scalars at the top level are valid documents.
	if got, ok := redactJSON([]byte(`[{"password":"x"}]`)); !ok || strings.Contains(got, `"x"`) {
		t.Errorf("top-level array: %q ok=%v", got, ok)
	}
}

// logOnce runs fn against a text logger using replaceAttr and returns the output.
func logOnce(fn func(l *slog.Logger)) string {
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug, ReplaceAttr: replaceAttr})
	fn(slog.New(h))
	return buf.String()
}

func TestReplaceAttr(t *testing.T) {
	out := logOnce(func(l *slog.Logger) {
		l.Info("exchanging code=SECRET-MSG",
			"client_secret", "SECRET-S1",
			"access_token", "SECRET-T1",
			"Authorization", "Bearer SECRET-B1",
			"token_type", "Bearer",
			"url", "http://h/cb?code=SECRET-C1&state=ok",
			"count", 3,
			slog.Group("oauth", slog.String("client_secret", "SECRET-S2"), slog.String("issuer", "https://i")),
			"error", errors.New(`Post "http://h/token?refresh_token=SECRET-R1": refused`),
		)
	})

	for _, leaked := range []string{"SECRET-MSG", "SECRET-S1", "SECRET-T1", "SECRET-B1", "SECRET-C1", "SECRET-S2", "SECRET-R1"} {
		if strings.Contains(out, leaked) {
			t.Errorf("%q leaked in: %s", leaked, out)
		}
	}
	for _, keep := range []string{"token_type=Bearer", "state=ok", "count=3", "oauth.issuer=https://i", "Bearer [REDACTED]"} {
		if !strings.Contains(out, keep) {
			t.Errorf("%q should be present in: %s", keep, out)
		}
	}
}

func TestReplaceAttrKeepsTimeAndLevel(t *testing.T) {
	out := logOnce(func(l *slog.Logger) { l.Info("hello") })
	if !strings.Contains(out, "time=") || !strings.Contains(out, "level=INFO") || !strings.Contains(out, "msg=hello") {
		t.Errorf("standard attributes damaged: %s", out)
	}
}
