package profile

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func newTestConfig(t *testing.T) *Config {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(EnvContext, "")
	return NewConfig()
}

func env(server string) *Environment {
	return &Environment{Server: server, OAuth: OAuthConfig{Issuer: server, ClientID: "cid"}}
}

func reload(t *testing.T) *Config {
	t.Helper()
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestSetPersists(t *testing.T) {
	cfg := newTestConfig(t)
	if err := cfg.Set("dev", env("https://dev")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	e, err := reload(t).Get("dev")
	if err != nil {
		t.Fatalf("Get after reload: %v", err)
	}
	if e.Server != "https://dev" {
		t.Errorf("server = %q, want https://dev", e.Server)
	}

	// Upsert replaces and persists.
	if err := cfg.Set("dev", env("https://dev2")); err != nil {
		t.Fatalf("Set upsert: %v", err)
	}
	if e, _ := reload(t).Get("dev"); e == nil || e.Server != "https://dev2" {
		t.Errorf("upsert not persisted: %+v", e)
	}
}

func TestSetValidation(t *testing.T) {
	cfg := newTestConfig(t)

	for _, name := range []string{"", ".hidden", "-x", "a/b", "a b", "../x", strings.Repeat("a", 65)} {
		err := cfg.Set(name, env("s"))
		if !errors.Is(err, ErrInvalidContextName) {
			t.Errorf("Set(%q) err = %v, want ErrInvalidContextName", name, err)
		}
	}
	for _, name := range []string{"a", "dev", "prod-1", "team.a_b", strings.Repeat("a", 64)} {
		if err := cfg.Set(name, env("s")); err != nil {
			t.Errorf("Set(%q) unexpected error: %v", name, err)
		}
	}

	if err := cfg.Set("nilenv", nil); err == nil {
		t.Error("Set with nil environment should fail")
	}
	if _, err := cfg.Get("nilenv"); !errors.Is(err, ErrContextNotFound) {
		t.Errorf("nil-environment context should not be registered, got %v", err)
	}
}

func TestSetOnZeroValueConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := &Config{}
	if err := cfg.Set("dev", env("s")); err != nil {
		t.Fatalf("Set: %v", err)
	}
}

func TestErrorMessageIsLocalizedAndMatchable(t *testing.T) {
	cfg := newTestConfig(t)
	_, err := cfg.Get("nope")
	if !errors.Is(err, ErrContextNotFound) {
		t.Fatalf("err = %v, want ErrContextNotFound", err)
	}
	if !strings.Contains(err.Error(), `"nope"`) {
		t.Errorf("message %q should name the context", err.Error())
	}
}

func TestNamesSorted(t *testing.T) {
	cfg := newTestConfig(t)
	for _, n := range []string{"zeta", "alpha", "mid"} {
		if err := cfg.Set(n, env(n)); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"alpha", "mid", "zeta"}
	if got := cfg.Names(); !reflect.DeepEqual(got, want) {
		t.Errorf("Names = %v, want %v", got, want)
	}
}

func TestUsePersists(t *testing.T) {
	cfg := newTestConfig(t)
	if err := cfg.Set("dev", env("s")); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Use("dev"); err != nil {
		t.Fatalf("Use: %v", err)
	}
	if got := reload(t).CurrentContext; got != "dev" {
		t.Errorf("persisted current-context = %q, want dev", got)
	}
	if err := cfg.Use("missing"); !errors.Is(err, ErrContextNotFound) {
		t.Errorf("Use(missing) err = %v", err)
	}
	if got := reload(t).CurrentContext; got != "dev" {
		t.Errorf("failed Use changed disk state: %q", got)
	}
}

func TestDeletePersistsAndClearsCurrent(t *testing.T) {
	cfg := newTestConfig(t)
	for _, n := range []string{"a", "b"} {
		if err := cfg.Set(n, env(n)); err != nil {
			t.Fatal(err)
		}
	}
	if err := cfg.Use("a"); err != nil {
		t.Fatal(err)
	}

	if err := cfg.Delete("b"); err != nil {
		t.Fatalf("Delete(b): %v", err)
	}
	got := reload(t)
	if got.CurrentContext != "a" {
		t.Errorf("deleting a non-current context changed current-context to %q", got.CurrentContext)
	}
	if _, err := got.Get("b"); !errors.Is(err, ErrContextNotFound) {
		t.Errorf("b still on disk: %v", err)
	}

	if err := cfg.Delete("a"); err != nil {
		t.Fatalf("Delete(a): %v", err)
	}
	if got := reload(t); got.CurrentContext != "" || len(got.Contexts) != 0 {
		t.Errorf("after deleting current: %+v", got)
	}

	if err := cfg.Delete("a"); !errors.Is(err, ErrContextNotFound) {
		t.Errorf("Delete(missing) err = %v", err)
	}
}

func TestRename(t *testing.T) {
	cfg := newTestConfig(t)
	for _, n := range []string{"old", "other"} {
		if err := cfg.Set(n, env(n)); err != nil {
			t.Fatal(err)
		}
	}
	if err := cfg.Use("old"); err != nil {
		t.Fatal(err)
	}

	if err := cfg.Rename("old", "new"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	got := reload(t)
	if got.CurrentContext != "new" {
		t.Errorf("current-context = %q, want new", got.CurrentContext)
	}
	if e, err := got.Get("new"); err != nil || e.Server != "old" {
		t.Errorf("renamed context lost its settings: %+v, %v", e, err)
	}
	if _, err := got.Get("old"); !errors.Is(err, ErrContextNotFound) {
		t.Errorf("old name still present: %v", err)
	}

	// Renaming a non-current context leaves the pointer alone.
	if err := cfg.Rename("other", "other2"); err != nil {
		t.Fatal(err)
	}
	if got := reload(t).CurrentContext; got != "new" {
		t.Errorf("current-context = %q, want new", got)
	}
}

func TestRenameErrors(t *testing.T) {
	cfg := newTestConfig(t)
	for _, n := range []string{"a", "b"} {
		if err := cfg.Set(n, env(n)); err != nil {
			t.Fatal(err)
		}
	}

	if err := cfg.Rename("missing", "x"); !errors.Is(err, ErrContextNotFound) {
		t.Errorf("missing source err = %v", err)
	}
	if err := cfg.Rename("a", "b"); !errors.Is(err, ErrContextExists) {
		t.Errorf("existing target err = %v", err)
	}
	if err := cfg.Rename("a", "bad name"); !errors.Is(err, ErrInvalidContextName) {
		t.Errorf("invalid target err = %v", err)
	}
	if err := cfg.Rename("a", "a"); err != nil {
		t.Errorf("rename to self err = %v", err)
	}
	if got := reload(t); !reflect.DeepEqual(got.Names(), []string{"a", "b"}) {
		t.Errorf("failed renames changed state: %v", got.Names())
	}
}

func TestResolvePrecedence(t *testing.T) {
	cfg := newTestConfig(t)
	for _, n := range []string{"default", "cur", "envctx", "flagctx"} {
		if err := cfg.Set(n, env(n)); err != nil {
			t.Fatal(err)
		}
	}

	if got := cfg.ResolveName(""); got != DefaultContext {
		t.Errorf("fallback = %q, want default", got)
	}

	if err := cfg.Use("cur"); err != nil {
		t.Fatal(err)
	}
	if got := cfg.ResolveName(""); got != "cur" {
		t.Errorf("current-context = %q, want cur", got)
	}

	t.Setenv(EnvContext, "envctx")
	if got := cfg.ResolveName(""); got != "envctx" {
		t.Errorf("env = %q, want envctx", got)
	}

	if got := cfg.ResolveName("flagctx"); got != "flagctx" {
		t.Errorf("flag = %q, want flagctx", got)
	}

	name, e, err := cfg.Resolve("flagctx")
	if err != nil || name != "flagctx" || e.Server != "flagctx" {
		t.Errorf("Resolve = %q, %+v, %v", name, e, err)
	}
}

func TestResolveNotFound(t *testing.T) {
	cfg := newTestConfig(t)
	name, e, err := cfg.Resolve("")
	if !errors.Is(err, ErrContextNotFound) || e != nil || name != DefaultContext {
		t.Errorf("Resolve = %q, %+v, %v", name, e, err)
	}
}
