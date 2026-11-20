package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Contexts == nil || len(cfg.Contexts) != 0 || cfg.CurrentContext != "" {
		t.Fatalf("expected empty config, got %+v", cfg)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	cfg := NewConfig()
	cfg.CurrentContext = "prod"
	cfg.Contexts["prod"] = &Environment{
		Server: "https://corp.internal",
		TLS:    TLSConfig{CACert: "/etc/ssl/certs/corp-root-ca.pem"},
		OAuth: OAuthConfig{
			Issuer:     "https://corp.internal",
			ClientID:   "c020b99d",
			GrantType:  "client_credentials",
			AuthType:   "private_key_jwt",
			PrivateJWK: "@/Users/vivek/.ssh/agentid-prod-jwk.json",
			Scopes:     []string{"agent:admin"},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	env := got.Contexts["prod"]
	if got.CurrentContext != "prod" || env == nil {
		t.Fatalf("unexpected config: %+v", got)
	}
	if env.Server != "https://corp.internal" || env.OAuth.ClientID != "c020b99d" ||
		env.OAuth.AuthType != "private_key_jwt" || len(env.OAuth.Scopes) != 1 ||
		env.TLS.CACert != "/etc/ssl/certs/corp-root-ca.pem" {
		t.Fatalf("round trip mismatch: %+v", env)
	}
}

func TestSavePermissionsAndNoTempFiles(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	if err := NewConfig().Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	dir := filepath.Join(xdg, ".ibm-agentid")
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir perm = %o, want 700", di.Mode().Perm())
	}

	fi, err := os.Stat(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file perm = %o, want 600", fi.Mode().Perm())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected only config.yaml, got %d entries", len(entries))
	}
}

func TestDirDefaultsToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".ibm-agentid"); dir != want {
		t.Errorf("Dir = %q, want %q", dir, want)
	}
}
