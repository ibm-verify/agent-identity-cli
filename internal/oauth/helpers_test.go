package oauth

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/ibm-verify/agent-identity-cli/internal/logging"
	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

var testNow = time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.DiscardHandler))
	os.Exit(m.Run())
}

// syncBuffer is a bytes.Buffer that can be written and read from different
// goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// capture sends the real logger, with its redaction, to a buffer at debug level.
func capture(t *testing.T) *syncBuffer {
	t.Helper()
	for _, k := range []string{
		"IBM_AGENTID_LOG_LEVEL", "IBM_AGENTID_LOG_OUTPUT", "IBM_AGENTID_LOG_FILE",
		"IBM_AGENTID_LOG_FORMAT", "IBM_AGENTID_LOG_MAX_SIZE", "IBM_AGENTID_LOG_MAX_FILES",
	} {
		t.Setenv(k, "")
	}
	buf := &syncBuffer{}
	prev := slog.Default()
	cleanup := logging.Init(logging.Options{Level: "debug", Output: "stderr", Stderr: buf, Args: []string{"test"}})
	t.Cleanup(func() {
		cleanup()
		slog.SetDefault(prev)
	})
	return buf
}

// fakeAS is an authorization server that records what it receives.
type fakeAS struct {
	srv *httptest.Server

	mu       sync.Mutex
	requests []recorded
	hits     map[string]int

	issuerOverride string // when set, discovery reports this issuer
	noRevocation   bool
	omitAuthzEP    bool

	// tokenFn decides the token endpoint response. The default issues access
	// token "at-<n>" with refresh token "rt-<n>".
	tokenFn func(r *recorded) (status int, body map[string]any)

	revokeStatus int           // 0 means 200
	revokeDelay  time.Duration // how long the revocation endpoint stalls

	n atomic.Int64
}

type recorded struct {
	Path string
	Form url.Values
}

func newFakeAS(t *testing.T) *fakeAS {
	t.Helper()
	f := &fakeAS{hits: map[string]int{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAS) url() string { return f.srv.URL }

func (f *fakeAS) hit(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

func (f *fakeAS) totalHits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.hits {
		n += c
	}
	return n
}

func (f *fakeAS) last(path string) *recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.requests) - 1; i >= 0; i-- {
		if f.requests[i].Path == path {
			r := f.requests[i]
			return &r
		}
	}
	return nil
}

func (f *fakeAS) record(path string, form url.Values) *recorded {
	rec := &recorded{Path: path, Form: form}
	f.mu.Lock()
	f.requests = append(f.requests, *rec)
	f.mu.Unlock()
	return rec
}

func (f *fakeAS) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.hits[r.URL.Path]++
	f.mu.Unlock()

	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		issuer := f.srv.URL
		if f.issuerOverride != "" {
			issuer = f.issuerOverride
		}
		doc := map[string]any{"issuer": issuer, "token_endpoint": f.srv.URL + "/token"}
		if !f.omitAuthzEP {
			doc["authorization_endpoint"] = f.srv.URL + "/authorize"
		}
		if !f.noRevocation {
			doc["revocation_endpoint"] = f.srv.URL + "/revoke"
		}
		writeJSON(w, http.StatusOK, doc)

	case "/token":
		_ = r.ParseForm()
		rec := f.record("/token", r.PostForm)
		if f.tokenFn != nil {
			status, body := f.tokenFn(rec)
			writeJSON(w, status, body)
			return
		}
		n := strconv.FormatInt(f.n.Add(1), 10)
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token":  "at-" + n,
			"refresh_token": "rt-" + n,
			"token_type":    "Bearer",
			"expires_in":    3600,
		})

	case "/revoke":
		_ = r.ParseForm()
		f.record("/revoke", r.PostForm)
		if f.revokeDelay > 0 {
			select {
			case <-time.After(f.revokeDelay):
			case <-r.Context().Done():
				return
			}
		}
		status := f.revokeStatus
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)

	default:
		http.NotFound(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// testEnv is a public authorization-code context against the fake server.
func testEnv(f *fakeAS) *profile.Environment {
	return &profile.Environment{
		Server: "https://app.example",
		OAuth: profile.OAuthConfig{
			Issuer:   f.url(),
			ClientID: "cli-client",
			Scopes:   []string{"openid", "offline_access"},
		},
	}
}

func secretEnv(f *fakeAS, grant string) *profile.Environment {
	env := testEnv(f)
	env.OAuth.AuthType = "client_secret_post"
	env.OAuth.GrantType = grant
	return env
}

func newProvider(t *testing.T, env *profile.Environment) (*Provider, *syncBuffer) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(EnvClientSecret, "")
	out := &syncBuffer{}
	return &Provider{
		Context:     "test",
		Env:         env,
		Out:         out,
		Now:         func() time.Time { return testNow },
		OpenBrowser: func(string) error { return errors.New("no browser in tests") },
	}, out
}

// seedToken writes a cached token for env, minted at issuedAt with a lifetime.
func seedToken(t *testing.T, env *profile.Environment, access, refresh string, expiresIn int64, issuedAt time.Time) {
	t.Helper()
	tok := profile.NewToken(env, access, refresh, "Bearer", expiresIn, issuedAt)
	if err := profile.SaveToken("test", tok); err != nil {
		t.Fatal(err)
	}
}

// callbackURL returns the redirect URL for an authorization URL, carrying the
// given callback parameters.
func callbackURL(t *testing.T, authURL string, mutate func(q url.Values)) string {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if err != nil {
		t.Fatal(err)
	}
	cb := url.Values{"code": {"the-code"}, "state": {q.Get("state")}}
	if mutate != nil {
		mutate(cb)
	}
	redirect.RawQuery = cb.Encode()
	return redirect.String()
}

// get performs a GET and returns the status and body.
func get(rawURL string) (int, string) {
	resp, err := http.Get(rawURL)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// browser returns an OpenBrowser hook that plays the user: it records the
// authorization URL and follows the redirect back to the CLI.
func browser(t *testing.T, mutate func(q url.Values), seen *string) func(string) error {
	t.Helper()
	return func(authURL string) error {
		if seen != nil {
			*seen = authURL
		}
		target := callbackURL(t, authURL, mutate)
		go get(target)
		return nil
	}
}

// freePort returns a loopback port that is free right now.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// portFree reports whether the loopback port can be bound.
func portFree(port int) bool {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func newECKey(t *testing.T) (private, public *jose.JSONWebKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	private = &jose.JSONWebKey{Key: priv, KeyID: "kid-1", Algorithm: "ES256", Use: "sig"}
	pub := private.Public()
	return private, &pub
}

func mustContain(t *testing.T, haystack, needle, what string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("%s: want %q in %q", what, needle, haystack)
	}
}

func mustNotContain(t *testing.T, haystack, needle, what string) {
	t.Helper()
	if needle != "" && strings.Contains(haystack, needle) {
		t.Errorf("%s: %q must not appear in %q", what, needle, haystack)
	}
}
