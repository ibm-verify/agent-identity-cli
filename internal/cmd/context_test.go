package cmd

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

// seed saves contexts a and b (a has an inline secret and a valid token) with
// a as the current one.
func seed(t *testing.T) {
	t.Helper()
	cfg := profile.NewConfig()
	for name, server := range map[string]string{"a": "https://a.example.com", "b": "https://b.example.com"} {
		env := &profile.Environment{
			Server: server,
			OAuth:  profile.OAuthConfig{Issuer: "https://idp.example.com", ClientID: "cid"},
		}
		if name == "a" {
			env.OAuth.ClientSecret = profile.SecretSource(clientSecret)
			env.OAuth.PrivateJWK = "@/keys/jwk.json"
		}
		if err := cfg.Set(name, env); err != nil {
			t.Fatal(err)
		}
	}
	if err := cfg.Use("a"); err != nil {
		t.Fatal(err)
	}
	tok := profile.NewToken(cfg.Contexts["a"], "at", "rt", "Bearer", 3600, time.Now())
	if err := profile.SaveToken("a", tok); err != nil {
		t.Fatal(err)
	}
}

func tokenExists(t *testing.T, name string) bool {
	t.Helper()
	p, err := profile.TokenPath(name)
	if err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(p)
	return err == nil
}

func TestContextListEmpty(t *testing.T) {
	isolate(t)
	out, errOut, err := run(t, "context", "list")
	if err != nil || out != "" || !strings.Contains(errOut, "login") {
		t.Errorf("out=%q err=%q %v", out, errOut, err)
	}
}

func TestContextList(t *testing.T) {
	isolate(t)
	seed(t)
	out, _, err := run(t, "context", "list")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("want a header and two rows, got %q", out)
	}
	if f := strings.Fields(lines[1]); len(f) != 4 || f[0] != "*" || f[1] != "a" || f[3] != "valid" {
		t.Errorf("row a = %q", lines[1])
	}
	if f := strings.Fields(lines[2]); len(f) != 3 || f[0] != "b" || f[2] != "none" {
		t.Errorf("row b = %q", lines[2])
	}
}

func TestContextUse(t *testing.T) {
	isolate(t)
	seed(t)
	if _, _, err := run(t, "context", "use", "b"); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := profile.Load(); cfg.CurrentContext != "b" {
		t.Errorf("current = %q", cfg.CurrentContext)
	}
	if _, _, err := run(t, "context", "use", "nope"); !errors.Is(err, profile.ErrContextNotFound) {
		t.Errorf("unknown context: %v", err)
	}
}

func TestContextCurrent(t *testing.T) {
	isolate(t)
	seed(t)
	for _, tc := range []struct {
		env  string
		args []string
		want string
	}{
		{"", nil, "a\n"},
		{"b", nil, "b\n"},
		{"b", []string{"--context", "a"}, "a\n"},
	} {
		t.Setenv("IBM_AGENTID_CONTEXT", tc.env)
		out, _, err := run(t, append([]string{"context", "current"}, tc.args...)...)
		if err != nil || out != tc.want {
			t.Errorf("env=%q args=%v: out=%q %v", tc.env, tc.args, out, err)
		}
	}
}

func TestContextView(t *testing.T) {
	isolate(t)
	seed(t)
	out, _, err := run(t, "context", "view")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "server: https://a.example.com") || !strings.Contains(out, "private_jwk: '@/keys/jwk.json'") && !strings.Contains(out, "private_jwk: \"@/keys/jwk.json\"") {
		t.Errorf("view = %q", out)
	}
	if strings.Contains(out, clientSecret) || !strings.Contains(out, "(inline)") {
		t.Errorf("an inline secret must be hidden: %q", out)
	}

	// The saved file still holds the secret as written.
	if cfg, _ := profile.Load(); cfg.Contexts["a"].OAuth.ClientSecret != clientSecret {
		t.Error("view changed the saved secret")
	}

	if out, _, err = run(t, "context", "view", "b"); err != nil || !strings.Contains(out, "b.example.com") {
		t.Errorf("view b: %q %v", out, err)
	}
	if _, _, err = run(t, "context", "view", "nope"); !errors.Is(err, profile.ErrContextNotFound) {
		t.Errorf("unknown: %v", err)
	}
}

func TestContextRename(t *testing.T) {
	isolate(t)
	seed(t)
	if _, _, err := run(t, "context", "rename", "a", "c"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := profile.Load()
	if cfg.Contexts["a"] != nil || cfg.Contexts["c"] == nil || cfg.CurrentContext != "c" {
		t.Errorf("contexts=%v current=%q", cfg.Names(), cfg.CurrentContext)
	}
	if tokenExists(t, "a") || !tokenExists(t, "c") {
		t.Error("the cached token did not move with the context")
	}

	if _, _, err := run(t, "context", "rename", "c", "b"); !errors.Is(err, profile.ErrContextExists) {
		t.Errorf("existing name: %v", err)
	}
	if _, _, err := run(t, "context", "rename", "c", "bad name"); !errors.Is(err, profile.ErrInvalidContextName) {
		t.Errorf("bad name: %v", err)
	}
}

func TestContextDeleteNeedsConfirmation(t *testing.T) {
	isolate(t)
	seed(t)

	// No input, a "no" and a stray answer all keep the context.
	for _, answer := range []string{"", "n\n", "maybe\n"} {
		_, errOut, err := runWithInput(t, answer, "context", "delete", "a")
		if err == nil || !strings.Contains(errOut, `"a"`) {
			t.Errorf("answer %q: err=%v prompt=%q", answer, err, errOut)
		}
		if cfg, _ := profile.Load(); cfg.Contexts["a"] == nil || !tokenExists(t, "a") {
			t.Fatalf("answer %q deleted the context", answer)
		}
	}

	if _, _, err := runWithInput(t, "Y\n", "context", "delete", "a"); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := profile.Load(); cfg.Contexts["a"] != nil || tokenExists(t, "a") {
		t.Error("a yes answer did not delete the context")
	}
}

func TestContextDeleteUnknownDoesNotAsk(t *testing.T) {
	isolate(t)
	seed(t)
	_, errOut, err := runWithInput(t, "", "context", "delete", "nope")
	if !errors.Is(err, profile.ErrContextNotFound) || strings.Contains(errOut, "[y/N]") {
		t.Errorf("err=%v stderr=%q", err, errOut)
	}
}

func TestContextDelete(t *testing.T) {
	isolate(t)
	seed(t)
	if _, _, err := run(t, "context", "delete", "a", "--yes"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := profile.Load()
	if cfg.Contexts["a"] != nil || cfg.CurrentContext != "" {
		t.Errorf("contexts=%v current=%q", cfg.Names(), cfg.CurrentContext)
	}
	if tokenExists(t, "a") {
		t.Error("the cached token was not deleted")
	}
	if _, _, err := run(t, "context", "delete", "a", "-y"); !errors.Is(err, profile.ErrContextNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
}
