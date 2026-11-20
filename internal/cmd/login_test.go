package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

const clientSecret = "s3cr3t-value-123"

// fakeAS is a minimal authorization server for client_credentials.
func fakeAS(t *testing.T) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":         srv.URL,
				"token_endpoint": srv.URL + "/token",
			})
		case "/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "the-access-token", "token_type": "Bearer", "expires_in": 3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// loginData returns client_credentials data for the fake server.
func loginData(issuer, extra string) string {
	return "server: https://app.example.com\n" +
		"oauth:\n" +
		"  issuer: " + issuer + "\n" +
		"  client_id: cid\n" +
		"  grant_type: client_credentials\n" +
		"  auth_type: client_secret_post\n" + extra
}

// run executes the CLI and returns stdout, stderr and the error.
func run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRoot("test")
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err := root.ExecuteContext(t.Context())
	return out.String(), errOut.String(), err
}

func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("IBM_AGENTID_CLIENT_SECRET", "")
	t.Setenv("IBM_AGENTID_CONTEXT", "")
	return dir
}

func writeData(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "login.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoginTemplate(t *testing.T) {
	isolate(t)
	for format, marker := range map[string]string{"yaml": "# Base URL", "json": `"server"`} {
		out, errOut, err := run(t, "login", "--template", "-o", format)
		if err != nil || !strings.Contains(out, marker) || errOut != "" {
			t.Errorf("%s: out=%q err=%q %v", format, out, errOut, err)
		}
	}
	if _, _, err := run(t, "login", "--template", "-o", "xml"); err == nil {
		t.Error("unknown format: want an error")
	}
}

func TestLoginRequiresData(t *testing.T) {
	isolate(t)
	if _, _, err := run(t, "login"); err == nil {
		t.Error("want an error without -d")
	}
}

func TestLoginSavesContext(t *testing.T) {
	isolate(t)
	as := fakeAS(t)
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte(clientSecret), 0o600); err != nil {
		t.Fatal(err)
	}
	data := writeData(t, loginData(as.URL, "  client_secret: \"@"+secret+"\"\n"))

	out, errOut, err := run(t, "login", "-d", data, "--context", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errOut, `"prod"`) || strings.Contains(errOut, "warning") {
		t.Errorf("stderr = %q", errOut)
	}

	cfg, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentContext != "prod" || cfg.Contexts["prod"] == nil {
		t.Errorf("config = %+v", cfg)
	}
	env := cfg.Contexts["prod"]
	if env.OAuth.ClientSecret != profile.SecretSource("@"+secret) {
		t.Errorf("client_secret = %q", env.OAuth.ClientSecret)
	}
	if _, status, _ := profile.LookupToken("prod", env, testNow()); status != profile.TokenValid {
		t.Errorf("token status = %v, want valid", status)
	}
}

func TestLoginNoSavePrintToken(t *testing.T) {
	dir := isolate(t)
	as := fakeAS(t)
	data := writeData(t, loginData(as.URL, "  client_secret: "+clientSecret+"\n"))

	out, errOut, err := run(t, "login", "-d", data, "--no-save", "--print-token")
	if err != nil {
		t.Fatal(err)
	}
	if out != "the-access-token\n" {
		t.Errorf("stdout = %q", out)
	}
	if strings.Contains(errOut, "warning") || strings.Contains(errOut+out, clientSecret) {
		t.Errorf("stderr = %q", errOut)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("--no-save left state behind: %v", entries)
	}
}

func TestLoginInlineSecretWarns(t *testing.T) {
	isolate(t)
	as := fakeAS(t)
	data := writeData(t, loginData(as.URL, "  client_secret: "+clientSecret+"\n"))

	out, errOut, err := run(t, "login", "-d", data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut, "oauth.client_secret") {
		t.Errorf("want a warning naming the property, got %q", errOut)
	}
	if strings.Contains(out+errOut, clientSecret) {
		t.Error("the secret value leaked into the output")
	}
}

func TestLoginFlagBeatsDataContext(t *testing.T) {
	isolate(t)
	as := fakeAS(t)
	data := writeData(t, "context: fromfile\n"+loginData(as.URL, "  client_secret: "+clientSecret+"\n"))

	if _, _, err := run(t, "login", "-d", data, "--context", "fromflag"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := profile.Load()
	if cfg.Contexts["fromflag"] == nil || cfg.Contexts["fromfile"] != nil {
		t.Errorf("contexts = %v", cfg.Names())
	}
}

func TestLoginFailureSavesNothing(t *testing.T) {
	dir := isolate(t)
	as := fakeAS(t)
	// No client secret, so the login fails before any context is saved.
	data := writeData(t, loginData(as.URL, ""))

	if _, _, err := run(t, "login", "-d", data); err == nil {
		t.Fatal("want an error")
	}
	if _, err := os.Stat(filepath.Join(dir, ".ibm-agentid", "config.yaml")); err == nil {
		t.Error("config.yaml was written after a failed login")
	}
}

func TestLoginSaveFailureDeletesToken(t *testing.T) {
	dir := isolate(t)
	as := fakeAS(t)
	data := writeData(t, loginData(as.URL, "  client_secret: "+clientSecret+"\n"))

	// A directory where config.yaml should be makes the save fail after the
	// login has cached its token.
	if err := os.MkdirAll(filepath.Join(dir, ".ibm-agentid", "config.yaml"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := run(t, "login", "-d", data); err == nil {
		t.Fatal("want an error")
	}
	tokenPath, err := profile.TokenPath("default")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tokenPath); err == nil {
		t.Error("the cached token was not deleted")
	}
}

func testNow() time.Time { return time.Now() }
