package oauth

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

func TestParseRedirect(t *testing.T) {
	cases := []struct {
		name         string
		raw          string
		listen       string
		display      string
		port         string
		handlerPath  string
		uriForPort42 string
	}{
		{"unset", "", "127.0.0.1", "127.0.0.1", "0", "/callback", "http://127.0.0.1:42/callback"},
		{"bare host", "http://127.0.0.1", "127.0.0.1", "127.0.0.1", "0", "/", "http://127.0.0.1:42"},
		{"explicit zero port", "http://127.0.0.1:0", "127.0.0.1", "127.0.0.1", "0", "/", "http://127.0.0.1:42"},
		{"fixed port and path", "http://127.0.0.1:8765/cb", "127.0.0.1", "127.0.0.1", "8765", "/cb", "http://127.0.0.1:42/cb"},
		{"ipv6", "http://[::1]:8765/cb", "::1", "[::1]", "8765", "/cb", "http://[::1]:42/cb"},
		{"localhost binds loopback v4", "http://localhost:8765/cb", "127.0.0.1", "localhost", "8765", "/cb", "http://localhost:42/cb"},
		{"path kept as written", "http://127.0.0.1:9000/a/b/", "127.0.0.1", "127.0.0.1", "9000", "/a/b/", "http://127.0.0.1:42/a/b/"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := parseRedirect(c.raw)
			if err != nil {
				t.Fatalf("parseRedirect(%q): %v", c.raw, err)
			}
			if p.listenHost != c.listen || p.displayHost != c.display || p.port != c.port {
				t.Errorf("plan = %+v", p)
			}
			if got := p.handlerPath(); got != c.handlerPath {
				t.Errorf("handlerPath = %q, want %q", got, c.handlerPath)
			}
			if got := p.uri(42); got != c.uriForPort42 {
				t.Errorf("uri = %q, want %q", got, c.uriForPort42)
			}
			if got, want := p.host(42), c.display+":42"; got != want {
				t.Errorf("host = %q, want %q", got, want)
			}
		})
	}
}

func TestParseRedirectRejects(t *testing.T) {
	for _, raw := range []string{
		"https://127.0.0.1:8765/cb",
		"http://example.com/cb",
		"http://192.168.1.10:8765/cb",
		"http://0.0.0.0:8765/cb",
		"ftp://127.0.0.1/cb",
		"127.0.0.1:8765/cb",
		"://bad",
		"http://127.0.0.1:99999/cb",
		"http://127.0.0.1:abc/cb",
	} {
		_, err := parseRedirect(raw)
		if !errors.Is(err, ErrInvalidAuthConfig) {
			t.Errorf("parseRedirect(%q) err = %v, want ErrInvalidAuthConfig", raw, err)
			continue
		}
		mustContain(t, err.Error(), raw, "message should name the value")
	}
}

func TestListenEphemeralAndLoopbackOnly(t *testing.T) {
	p, _ := parseRedirect("")
	ln, err := p.listen()
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	addr := ln.Addr().(*net.TCPAddr)
	if !addr.IP.IsLoopback() || addr.Port == 0 {
		t.Errorf("listener = %v, want a loopback address with a real port", addr)
	}
}

func TestListenPortInUseNamesThePort(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := strconv.Itoa(busy.Addr().(*net.TCPAddr).Port)

	p, err := parseRedirect("http://127.0.0.1:" + port + "/cb")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := p.listen()
	if err == nil {
		ln.Close()
		t.Fatal("listen on a busy port should fail, with no fallback to another port")
	}
	mustContain(t, err.Error(), port, "message should name the port")
	mustContain(t, err.Error(), "redirect_uri", "message should say how to fix it")
}

func TestLoginRedirectErrorsAreInvalidConfig(t *testing.T) {
	f := newFakeAS(t)
	env := testEnv(f)
	env.OAuth.RedirectURI = "https://example.com/cb"
	p, _ := newProvider(t, env)

	_, err := p.Login(t.Context(), LoginOptions{})
	if !errors.Is(err, ErrInvalidAuthConfig) {
		t.Fatalf("err = %v, want ErrInvalidAuthConfig", err)
	}
	if f.totalHits() != 0 {
		t.Error("an invalid redirect_uri must be rejected before any network call")
	}
	if _, status, _ := profile.LookupToken("test", env, testNow); status != profile.TokenMissing {
		t.Errorf("nothing should be cached, got %v", status)
	}
	if strings.Contains(err.Error(), "%!") {
		t.Errorf("badly formatted message: %q", err)
	}
}
