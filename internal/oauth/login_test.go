package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	auth "github.com/ibm-verify/verify-auth-go"

	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

func challengeOf(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func portOf(t *testing.T, authURL string) int {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	r, err := url.Parse(u.Query().Get("redirect_uri"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(r.Port())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestLoginAuthorizationCode(t *testing.T) {
	f := newFakeAS(t)
	env := testEnv(f)
	p, out := newProvider(t, env)

	var authURL string
	p.OpenBrowser = browser(t, nil, &authURL)

	tok, err := p.Login(t.Context(), LoginOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if tok.AccessToken != "at-1" || tok.RefreshToken != "rt-1" {
		t.Errorf("token = %+v", tok)
	}
	if want := testNow.Add(time.Hour); !tok.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want %v", tok.ExpiresAt, want)
	}

	u, _ := url.Parse(authURL)
	q := u.Query()
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q", q.Get("code_challenge_method"))
	}
	if q.Get("client_id") != "cli-client" || q.Get("response_type") != "code" {
		t.Errorf("authorization query = %v", q)
	}
	if !strings.HasPrefix(q.Get("redirect_uri"), "http://127.0.0.1:") || !strings.HasSuffix(q.Get("redirect_uri"), "/callback") {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("state") == "" {
		t.Error("no state in the authorization URL")
	}

	form := f.last("/token").Form
	if form.Get("grant_type") != "authorization_code" || form.Get("code") != "the-code" {
		t.Errorf("token request = %v", form)
	}
	if got := challengeOf(form.Get("code_verifier")); got != q.Get("code_challenge") {
		t.Errorf("code_verifier does not match the challenge: %q vs %q", got, q.Get("code_challenge"))
	}
	if form.Get("client_id") != "cli-client" || form.Has("client_secret") || form.Has("client_assertion") {
		t.Errorf("a public client sends its client_id and no credentials: %v", form)
	}
	if form.Get("redirect_uri") != q.Get("redirect_uri") {
		t.Errorf("token redirect_uri = %q, want %q", form.Get("redirect_uri"), q.Get("redirect_uri"))
	}

	cached, status, err := profile.LookupToken("test", env, testNow)
	if err != nil || status != profile.TokenValid || cached.AccessToken != "at-1" {
		t.Errorf("cache: %v %v %+v", err, status, cached)
	}

	mustContain(t, out.String(), authURL, "prompt should print the sign-in URL")
	mustNotContain(t, out.String(), "at-1", "prompts")
	mustNotContain(t, out.String(), "the-code", "prompts")
}

func TestLoginNoSave(t *testing.T) {
	f := newFakeAS(t)
	env := testEnv(f)
	p, _ := newProvider(t, env)
	p.OpenBrowser = browser(t, nil, nil)

	tok, err := p.Login(t.Context(), LoginOptions{NoSave: true, Timeout: 10 * time.Second})
	if err != nil || tok.AccessToken != "at-1" {
		t.Fatalf("%v %+v", err, tok)
	}
	if _, status, _ := profile.LookupToken("test", env, testNow); status != profile.TokenMissing {
		t.Errorf("NoSave must not write the cache, status = %v", status)
	}
}

func TestLoginFixedRedirectURI(t *testing.T) {
	f := newFakeAS(t)
	env := testEnv(f)
	port := freePort(t)
	env.OAuth.RedirectURI = "http://127.0.0.1:" + strconv.Itoa(port) + "/cb"
	p, _ := newProvider(t, env)

	var authURL string
	p.OpenBrowser = browser(t, nil, &authURL)

	if _, err := p.Login(t.Context(), LoginOptions{Timeout: 10 * time.Second}); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(authURL)
	want := "http://127.0.0.1:" + strconv.Itoa(port) + "/cb"
	if got := u.Query().Get("redirect_uri"); got != want {
		t.Errorf("redirect_uri = %q, want %q", got, want)
	}
	if !portFree(port) {
		t.Error("the port should be released after login")
	}
}

func TestLoginNoBrowserPrintsURLAndSkipsOpener(t *testing.T) {
	f := newFakeAS(t)
	p, out := newProvider(t, testEnv(f))

	var opened atomic.Bool
	p.OpenBrowser = func(string) error { opened.Store(true); return nil }

	// The user copies the printed URL into a browser.
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			for _, line := range strings.Split(out.String(), "\n") {
				if strings.HasPrefix(line, "http://") {
					get(callbackURL(t, line, nil))
					return
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	if _, err := p.Login(t.Context(), LoginOptions{NoBrowser: true, Timeout: 10 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if opened.Load() {
		t.Error("NoBrowser must not call the opener")
	}
}

func TestLoginBrowserFailureIsNotAnError(t *testing.T) {
	f := newFakeAS(t)
	p, _ := newProvider(t, testEnv(f))

	open := browser(t, nil, nil)
	p.OpenBrowser = func(u string) error {
		_ = open(u)
		return errors.New("no display")
	}
	if _, err := p.Login(t.Context(), LoginOptions{Timeout: 10 * time.Second}); err != nil {
		t.Fatalf("a failing opener must not fail the login: %v", err)
	}
}

func TestLoginStateAndIssuerChecks(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(q url.Values)
		wantErr bool
	}{
		{"iss matches", func(q url.Values) { q.Set("iss", "ISSUER") }, false},
		{"iss matches with trailing slash", func(q url.Values) { q.Set("iss", "ISSUER/") }, false},
		{"iss missing is accepted", nil, false},
		{"iss mismatch", func(q url.Values) { q.Set("iss", "https://evil.example") }, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeAS(t)
			p, _ := newProvider(t, testEnv(f))
			p.OpenBrowser = browser(t, func(q url.Values) {
				if c.mutate != nil {
					c.mutate(q)
				}
				if q.Get("iss") == "ISSUER" {
					q.Set("iss", f.url())
				}
				if q.Get("iss") == "ISSUER/" {
					q.Set("iss", f.url()+"/")
				}
			}, nil)

			_, err := p.Login(t.Context(), LoginOptions{Timeout: 10 * time.Second})
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if c.wantErr && f.hit("/token") != 0 {
				t.Error("no token request may be made after an issuer mismatch")
			}
		})
	}
}

func TestLoginAccessDenied(t *testing.T) {
	f := newFakeAS(t)
	p, out := newProvider(t, testEnv(f))
	p.OpenBrowser = browser(t, func(q url.Values) {
		q.Del("code")
		q.Set("error", "access_denied")
		q.Set("error_description", "the user said no")
	}, nil)

	_, err := p.Login(t.Context(), LoginOptions{Timeout: 10 * time.Second})
	var ae *auth.Error
	if !errors.As(err, &ae) || ae.Code != "access_denied" {
		t.Fatalf("err = %v, want an access_denied auth.Error", err)
	}
	if f.hit("/token") != 0 {
		t.Error("no token request after a denied login")
	}
	mustNotContain(t, out.String(), "the user said no", "prompts")
}

func TestLoginTimeoutReleasesPort(t *testing.T) {
	f := newFakeAS(t)
	p, _ := newProvider(t, testEnv(f))

	var authURL string
	p.OpenBrowser = func(u string) error { authURL = u; return nil }

	_, err := p.Login(t.Context(), LoginOptions{Timeout: 150 * time.Millisecond})
	if err == nil {
		t.Fatal("want a timeout error")
	}
	mustContain(t, err.Error(), "150ms", "timeout message")
	if !portFree(portOf(t, authURL)) {
		t.Error("the port should be released after a timeout")
	}
}

func TestLoginCancelReleasesPort(t *testing.T) {
	f := newFakeAS(t)
	p, _ := newProvider(t, testEnv(f))

	ctx, cancel := context.WithCancel(t.Context())
	var authURL string
	p.OpenBrowser = func(u string) error { authURL = u; cancel(); return nil }

	_, err := p.Login(ctx, LoginOptions{Timeout: 10 * time.Second})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if !portFree(portOf(t, authURL)) {
		t.Error("the port should be released after cancel")
	}
	if _, status, _ := profile.LookupToken("test", p.Env, testNow); status != profile.TokenMissing {
		t.Errorf("nothing should be cached, got %v", status)
	}
}

// TestLoginIgnoresStrayRequests plays an attacker and a confused browser before
// the real callback. None of them may end the login, and the real one must.
func TestLoginIgnoresStrayRequests(t *testing.T) {
	f := newFakeAS(t)
	p, _ := newProvider(t, testEnv(f))

	type probe struct {
		name   string
		status int
	}
	var probes []probe
	done := make(chan struct{})

	p.OpenBrowser = func(authURL string) error {
		go func() {
			defer close(done)
			good, _ := url.Parse(callbackURL(t, authURL, nil))

			do := func(name, method string, mutate func(u *url.URL, req *http.Request)) {
				u := *good
				req, _ := http.NewRequest(method, "", nil)
				mutate(&u, req)
				req.URL = &u
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Errorf("%s: %v", name, err)
					return
				}
				resp.Body.Close()
				probes = append(probes, probe{name, resp.StatusCode})
			}
			withQuery := func(f func(q url.Values)) func(*url.URL, *http.Request) {
				return func(u *url.URL, _ *http.Request) {
					q := u.Query()
					f(q)
					u.RawQuery = q.Encode()
				}
			}

			do("wrong state", "GET", withQuery(func(q url.Values) { q.Set("state", "nope") }))
			do("missing state", "GET", withQuery(func(q url.Values) { q.Del("state") }))
			do("no code or error", "GET", withQuery(func(q url.Values) { q.Del("code") }))
			do("other path", "GET", func(u *url.URL, _ *http.Request) { u.Path = "/other" })
			do("post", "POST", func(*url.URL, *http.Request) {})
			do("foreign host", "GET", func(_ *url.URL, r *http.Request) { r.Host = "evil.example" })
			do("real", "GET", func(*url.URL, *http.Request) {})
		}()
		return nil
	}

	if _, err := p.Login(t.Context(), LoginOptions{Timeout: 10 * time.Second}); err != nil {
		t.Fatal(err)
	}
	<-done

	want := map[string]int{
		"wrong state":      http.StatusBadRequest,
		"missing state":    http.StatusBadRequest,
		"no code or error": http.StatusBadRequest,
		"other path":       http.StatusNotFound,
		"post":             http.StatusMethodNotAllowed,
		"foreign host":     http.StatusBadRequest,
		"real":             http.StatusOK,
	}
	if len(probes) != len(want) {
		t.Fatalf("probes = %v", probes)
	}
	for _, pr := range probes {
		if pr.status != want[pr.name] {
			t.Errorf("%s: status %d, want %d", pr.name, pr.status, want[pr.name])
		}
	}
	if got := f.hit("/token"); got != 1 {
		t.Errorf("token endpoint hit %d times, want 1", got)
	}
}

func TestCallbackServerAcceptsOneCallback(t *testing.T) {
	ln, err := mustListen()
	if err != nil {
		t.Fatal(err)
	}
	host := ln.Addr().String()
	srv := newCallbackServer(ln, host, "/callback", "s-1", "https://issuer.example")
	defer srv.Close()
	srv.start()

	target := "http://" + host + "/callback?code=c&state=s-1"
	if status, _ := get(target); status != http.StatusOK {
		t.Fatalf("first callback = %d", status)
	}
	if status, _ := get(target); status != http.StatusBadRequest {
		t.Errorf("second callback = %d, want 400", status)
	}
	res, err := srv.wait(t.Context(), time.Second)
	if err != nil || res.params.Get("code") != "c" {
		t.Errorf("result = %+v, %v", res, err)
	}
}

func TestCallbackPageNeverEchoesTheRequest(t *testing.T) {
	ln, err := mustListen()
	if err != nil {
		t.Fatal(err)
	}
	host := ln.Addr().String()
	srv := newCallbackServer(ln, host, "/callback", "s-1", "https://issuer.example")
	defer srv.Close()
	srv.start()

	payload := "<script>alert(1)</script>"
	q := url.Values{"error": {"access_denied"}, "error_description": {payload}, "state": {"s-1"}}
	resp, err := http.Get("http://" + host + "/callback?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := new(strings.Builder)
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		body.Write(buf[:n])
		if err != nil {
			break
		}
	}

	for _, s := range []string{payload, "alert", "access_denied", "s-1"} {
		mustNotContain(t, body.String(), s, "callback page")
	}
	for h, want := range map[string]string{
		"Cache-Control":           "no-store",
		"Referrer-Policy":         "no-referrer",
		"X-Content-Type-Options":  "nosniff",
		"Content-Security-Policy": "default-src 'none'",
	} {
		if got := resp.Header.Get(h); got != want {
			t.Errorf("%s = %q, want %q", h, got, want)
		}
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestLoginLogsAndPromptsLeakNothing(t *testing.T) {
	logs := capture(t)
	f := newFakeAS(t)
	p, out := newProvider(t, testEnv(f))

	var authURL string
	p.OpenBrowser = browser(t, nil, &authURL)

	if _, err := p.Login(t.Context(), LoginOptions{Timeout: 10 * time.Second}); err != nil {
		t.Fatal(err)
	}

	u, _ := url.Parse(authURL)
	state := u.Query().Get("state")
	verifier := f.last("/token").Form.Get("code_verifier")

	for _, secret := range []string{"at-1", "rt-1", "the-code", verifier, state, u.RawQuery} {
		mustNotContain(t, logs.String(), secret, "logs")
	}
	for _, secret := range []string{"at-1", "rt-1", "the-code", verifier} {
		mustNotContain(t, out.String(), secret, "prompts")
	}
	mustContain(t, logs.String(), "login completed", "logs")
}

func TestUnsupportedGrants(t *testing.T) {
	for _, grant := range []string{"device_code", "implicit", "password", "bogus"} {
		t.Run(grant, func(t *testing.T) {
			f := newFakeAS(t)
			env := testEnv(f)
			env.OAuth.GrantType = grant
			p, _ := newProvider(t, env)

			if _, err := p.Login(t.Context(), LoginOptions{}); !errors.Is(err, ErrUnsupportedGrant) {
				t.Errorf("Login err = %v, want ErrUnsupportedGrant", err)
			}
			if _, err := p.Token(t.Context()); !errors.Is(err, ErrUnsupportedGrant) {
				t.Errorf("Token err = %v, want ErrUnsupportedGrant", err)
			}
			if f.totalHits() != 0 {
				t.Error("an unsupported grant must be rejected before any network call")
			}
		})
	}
}

func TestLoginInvalidSettings(t *testing.T) {
	cases := map[string]func(e *profile.Environment){
		"no issuer":               func(e *profile.Environment) { e.OAuth.Issuer = "" },
		"no client id":            func(e *profile.Environment) { e.OAuth.ClientID = "" },
		"unknown auth type":       func(e *profile.Environment) { e.OAuth.AuthType = "tls_client_auth" },
		"private_key_jwt no key":  func(e *profile.Environment) { e.OAuth.AuthType = "private_key_jwt" },
		"client_credentials none": func(e *profile.Environment) { e.OAuth.GrantType = "client_credentials" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeAS(t)
			env := testEnv(f)
			mutate(env)
			p, _ := newProvider(t, env)
			if _, err := p.Login(t.Context(), LoginOptions{}); !errors.Is(err, ErrInvalidAuthConfig) {
				t.Errorf("err = %v, want ErrInvalidAuthConfig", err)
			}
			if f.totalHits() != 0 {
				t.Error("invalid settings must be rejected before any network call")
			}
		})
	}
}

func TestLoginClientSecretMissing(t *testing.T) {
	f := newFakeAS(t)
	p, _ := newProvider(t, secretEnv(f, "authorization_code"))
	p.OpenBrowser = browser(t, nil, nil)

	_, err := p.Login(t.Context(), LoginOptions{Timeout: 10 * time.Second})
	if !errors.Is(err, ErrInvalidAuthConfig) {
		t.Fatalf("err = %v, want ErrInvalidAuthConfig", err)
	}
	mustContain(t, err.Error(), EnvClientSecret, "message should say how to supply the secret")
}

func TestLoginClientSecretAuthorizationCode(t *testing.T) {
	f := newFakeAS(t)
	env := secretEnv(f, "authorization_code")
	env.OAuth.ClientSecret = "s3cret"
	p, _ := newProvider(t, env)
	p.OpenBrowser = browser(t, nil, nil)

	if _, err := p.Login(t.Context(), LoginOptions{Timeout: 10 * time.Second}); err != nil {
		t.Fatal(err)
	}
	form := f.last("/token").Form
	if form.Get("client_id") != "cli-client" || form.Get("client_secret") != "s3cret" {
		t.Errorf("token request = %v", form)
	}
}

func mustListen() (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") }
