package resource

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDoerSendsTheBearerToken(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
	}))
	defer srv.Close()

	// With no HTTP client in the connection, the default one is used.
	d, err := Conn{Server: srv.URL, Token: "tok"}.Doer()
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	resp, err := d.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if got != "Bearer tok" {
		t.Errorf("Authorization: %q", got)
	}

	// The connection's own client is used when there is one.
	used := false
	hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		used = true
		return httptest.NewRecorder().Result(), nil
	})}
	d, _ = Conn{Server: srv.URL, Token: "tok", HTTP: hc}.Doer()
	req, _ = http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	resp, err = d.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if !used {
		t.Error("the connection's HTTP client was not used")
	}
}

func TestDoerNeedsAToken(t *testing.T) {
	if _, err := (Conn{Server: "http://x"}).Doer(); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("no token: %v", err)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
