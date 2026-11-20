package input

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

const (
	secretValue = "s3cr3t-value-123"
	yamlLogin   = "server: https://srv.example.com\noauth:\n  issuer: https://idp.example.com\n  client_id: cid\n"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func stdinWith(t *testing.T, content string) *os.File {
	t.Helper()
	return mustOpen(t, write(t, t.TempDir(), "stdin", content))
}

func mustOpen(t *testing.T, p string) *os.File {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestRead(t *testing.T) {
	dir := t.TempDir()
	file := write(t, dir, "in.yaml", yamlLogin)

	d, err := Read(file, nil)
	if err != nil || d.BaseDir != dir || string(d.Bytes) != yamlLogin {
		t.Fatalf("file: %+v, %v", d, err)
	}

	d, err = Read("-", stdinWith(t, yamlLogin))
	if err != nil || d.BaseDir != "" || string(d.Bytes) != yamlLogin {
		t.Fatalf("stdin: %+v, %v", d, err)
	}

	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{"missing", func() error { _, err := Read(filepath.Join(dir, "none"), nil); return err }, profile.ErrSourceUnavailable},
		{"directory", func() error { _, err := Read(dir, nil); return err }, profile.ErrSourceUnavailable},
		{"empty", func() error { _, err := Read(write(t, dir, "e", ""), nil); return err }, ErrInvalidInput},
		{"blank", func() error { _, err := Read(write(t, dir, "b", " \n\t\n"), nil); return err }, ErrInvalidInput},
		{"too large", func() error {
			_, err := Read(write(t, dir, "big", strings.Repeat("a", MaxSize+1)), nil)
			return err
		}, ErrInvalidInput},
		{"terminal", func() error {
			tty, err := os.Open(os.DevNull) // a character device, like a terminal
			if err != nil {
				t.Fatal(err)
			}
			defer tty.Close()
			_, err = Read("-", tty)
			return err
		}, ErrInvalidInput},
	}
	for _, tc := range tests {
		if err := tc.run(); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
}

func TestParseLogin(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "ca.pem", "pem")
	write(t, dir, "secret.txt", secretValue)

	const body = `
context: prod
server: https://srv.example.com
unknown_property: ignored
tls:
  ca_cert: "@./ca.pem"
oauth:
  issuer: https://idp.example.com
  client_id: cid
  client_secret: "@secret.txt"
  scopes: [openid]
`
	l, err := ParseLogin(&Data{Bytes: []byte(body), BaseDir: dir}, "")
	if err != nil {
		t.Fatal(err)
	}
	if l.Context != "prod" || l.Server != "https://srv.example.com" || l.OAuth.ClientID != "cid" {
		t.Errorf("unexpected: %+v", l)
	}
	if want := profile.Source("@" + filepath.Join(dir, "ca.pem")); l.TLS.CACert != want {
		t.Errorf("ca_cert = %q, want %q", l.TLS.CACert, want)
	}
	if want := profile.SecretSource("@" + filepath.Join(dir, "secret.txt")); l.OAuth.ClientSecret != want {
		t.Errorf("client_secret = %q, want %q", l.OAuth.ClientSecret, want)
	}
	if got := l.InlineSecrets(); len(got) != 0 {
		t.Errorf("InlineSecrets = %v, want none", got)
	}

	// JSON gives the same result.
	j := `{"server":"https://srv.example.com","oauth":{"issuer":"i","client_id":"c"}}`
	if l, err = ParseLogin(&Data{Bytes: []byte(j)}, ""); err != nil || l.Context != "default" {
		t.Errorf("json: %+v, %v", l, err)
	}
}

func TestParseLoginContextPrecedence(t *testing.T) {
	d := &Data{Bytes: []byte("context: fromfile\n" + yamlLogin)}
	for _, tc := range []struct{ flag, want string }{{"fromflag", "fromflag"}, {"", "fromfile"}} {
		if l, err := ParseLogin(d, tc.flag); err != nil || l.Context != tc.want {
			t.Errorf("flag %q: %+v, %v", tc.flag, l, err)
		}
	}
	if _, err := ParseLogin(d, "bad name"); !errors.Is(err, profile.ErrInvalidContextName) {
		t.Errorf("bad name: %v", err)
	}
}

func TestParseLoginInvalid(t *testing.T) {
	dir := t.TempDir()
	tests := map[string]string{
		"no server":    "oauth: {issuer: i, client_id: c}",
		"bad scheme":   "server: ftp://h\noauth: {issuer: i, client_id: c}",
		"userinfo":     "server: https://u:p@h\noauth: {issuer: i, client_id: c}",
		"no issuer":    "server: https://h\noauth: {client_id: c}",
		"no client id": "server: https://h\noauth: {issuer: i}",
		"wrong type":   "server: https://h\noauth: [1]",
		"bad syntax":   "server: [",
		"unquoted @":   "server: https://h\noauth: {issuer: i, client_id: c, client_secret: @x}",
	}
	for name, body := range tests {
		if _, err := ParseLogin(&Data{Bytes: []byte(body), BaseDir: dir}, ""); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: got %v, want ErrInvalidInput", name, err)
		}
	}

	for name, ref := range map[string]string{"missing": "@nofile", "directory": "@.", "bare @": "@"} {
		body := yamlLogin + "  client_secret: \"" + ref + "\"\n"
		if _, err := ParseLogin(&Data{Bytes: []byte(body), BaseDir: dir}, ""); !errors.Is(err, profile.ErrSourceUnavailable) {
			t.Errorf("%s: got %v, want ErrSourceUnavailable", name, err)
		}
	}
}

func TestInlineSecretsAndNoLeaks(t *testing.T) {
	body := yamlLogin + "  client_secret: " + secretValue + "\n  private_jwk: '{\"kty\":\"EC\"}'\n"
	l, err := ParseLogin(&Data{Bytes: []byte(body)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(l.InlineSecrets(), ","); got != "oauth.private_jwk,oauth.client_secret" {
		t.Errorf("InlineSecrets = %q", got)
	}
	if l.OAuth.ClientSecret != secretValue {
		t.Errorf("inline value changed: %q", l.OAuth.ClientSecret)
	}

	// Errors must not quote values from the data.
	_, err = ParseLogin(&Data{Bytes: []byte("server: https://h\noauth:\n  client_id: " + secretValue + "\n  issuer: [" + secretValue + "]\n")}, "")
	if err == nil || strings.Contains(err.Error(), secretValue) {
		t.Errorf("error leaks or is missing: %v", err)
	}
	_, err = ParseLogin(&Data{Bytes: []byte("server: https://" + secretValue + "@h\noauth: {issuer: i, client_id: c}")}, "")
	if err == nil || strings.Contains(err.Error(), secretValue) {
		t.Errorf("server error leaks or is missing: %v", err)
	}
}

func TestSavedShapeRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	dir := t.TempDir()
	write(t, dir, "secret.txt", secretValue)

	body := yamlLogin + "  client_secret: \"@secret.txt\"\n"
	l, err := ParseLogin(&Data{Bytes: []byte(body), BaseDir: dir}, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := profile.NewConfig()
	if err := cfg.Set(l.Context, &l.Environment); err != nil {
		t.Fatal(err)
	}
	loaded, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	env, err := loaded.Get(l.Context)
	if err != nil {
		t.Fatal(err)
	}
	if want := profile.SecretSource("@" + filepath.Join(dir, "secret.txt")); env.OAuth.ClientSecret != want {
		t.Errorf("saved client_secret = %q, want %q", env.OAuth.ClientSecret, want)
	}
}

func TestLoginTemplate(t *testing.T) {
	y, err := LoginTemplate("")
	if err != nil || !strings.Contains(string(y), "# ") {
		t.Fatalf("yaml: %v", err)
	}
	j, err := LoginTemplate("json")
	if err != nil || strings.Contains(string(j), "#") {
		t.Fatalf("json: %v", err)
	}

	// Both decode, and the example files are not checked.
	var fromYAML, fromJSON Login
	if err := decode(y, &fromYAML); err != nil {
		t.Fatal(err)
	}
	if err := decode(j, &fromJSON); err != nil {
		t.Fatal(err)
	}
	if fromYAML.Server == "" || fromYAML.OAuth.ClientID == "" || fromYAML.TLS.CACert == "" {
		t.Errorf("template is missing properties: %+v", fromYAML)
	}
	if fromYAML.Server != fromJSON.Server || fromYAML.OAuth.PrivateJWK != fromJSON.OAuth.PrivateJWK || len(fromJSON.OAuth.Scopes) != len(fromYAML.OAuth.Scopes) {
		t.Errorf("yaml and json differ: %+v vs %+v", fromYAML, fromJSON)
	}

	if _, err := LoginTemplate("xml"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("unknown format: %v", err)
	}
}
