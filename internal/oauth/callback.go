package oauth

import (
	"context"
	"crypto/subtle"
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
)

const callbackHeaderTimeout = 10 * time.Second

type pageKind int

const (
	pageSuccess pageKind = iota
	pageFailure
	pageRejected
)

// callbackResult is the one accepted callback.
type callbackResult struct {
	params         url.Values
	issuerMismatch bool // the callback carried an iss that is not the issuer
}

// callbackServer receives the browser redirect on a loopback listener. It
// accepts exactly one callback that carries the expected state; everything else
// is rejected and ignored, so a stray request cannot end the login.
type callbackServer struct {
	ln      net.Listener
	srv     *http.Server
	host    string // the only Host header value accepted
	path    string
	state   string
	issuer  string
	handled atomic.Bool
	result  chan callbackResult
}

// newCallbackServer serves the callback at path on ln. host is the host and
// port from the redirect URI, which is what the browser sends as Host.
func newCallbackServer(ln net.Listener, host, path, state, issuer string) *callbackServer {
	s := &callbackServer{
		ln:     ln,
		host:   strings.ToLower(host),
		path:   path,
		state:  state,
		issuer: strings.TrimRight(issuer, "/"),
		result: make(chan callbackResult, 1),
	}
	s.srv = &http.Server{Handler: s, ReadHeaderTimeout: callbackHeaderTimeout}
	return s
}

func (s *callbackServer) start() {
	go func() { _ = s.srv.Serve(s.ln) }()
}

// wait blocks until a callback is accepted, the timeout passes, or ctx ends.
func (s *callbackServer) wait(ctx context.Context, timeout time.Duration) (callbackResult, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case res := <-s.result:
		return res, nil
	case <-timer.C:
		return callbackResult{}, i18n.Errorf("auth.login_timeout", map[string]any{"Duration": timeout.String()})
	case <-ctx.Done():
		return callbackResult{}, ctx.Err()
	}
}

// Close shuts the server down and releases the port.
func (s *callbackServer) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.srv.Shutdown(ctx); err != nil {
		_ = s.srv.Close()
	}
	// Serve may not have started yet; make sure the port is released.
	_ = s.ln.Close()
}

func (s *callbackServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// A Host that is not the redirect URI's host is a DNS-rebinding attempt or a
	// misdirected request.
	if strings.ToLower(r.Host) != s.host {
		s.page(w, http.StatusBadRequest, pageRejected)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.page(w, http.StatusMethodNotAllowed, pageRejected)
		return
	}
	if r.URL.Path != s.path {
		s.page(w, http.StatusNotFound, pageRejected)
		return
	}

	q := r.URL.Query()
	stateOK := subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(s.state)) == 1
	if !stateOK || (q.Get("code") == "" && q.Get("error") == "") {
		s.page(w, http.StatusBadRequest, pageRejected)
		return
	}
	if !s.handled.CompareAndSwap(false, true) {
		s.page(w, http.StatusBadRequest, pageRejected)
		return
	}

	iss := q.Get("iss")
	mismatch := iss != "" && strings.TrimRight(iss, "/") != s.issuer
	failed := mismatch || q.Get("error") != ""

	slog.Debug("sign-in callback received",
		"has_code", q.Get("code") != "",
		"error", q.Get("error"),
		"issuer_mismatch", mismatch)

	if failed {
		s.page(w, http.StatusBadRequest, pageFailure)
	} else {
		s.page(w, http.StatusOK, pageSuccess)
	}
	s.result <- callbackResult{params: q, issuerMismatch: mismatch}
}

// page writes a fixed, localized page. It never echoes anything from the
// request, so a crafted URL cannot inject content.
func (s *callbackServer) page(w http.ResponseWriter, status int, kind pageKind) {
	var title, body string
	switch kind {
	case pageSuccess:
		title, body = i18n.T("auth.page_success_title", nil), i18n.T("auth.page_success_body", nil)
	case pageFailure:
		title, body = i18n.T("auth.page_failure_title", nil), i18n.T("auth.page_failure_body", nil)
	default:
		title, body = i18n.T("auth.page_rejected_title", nil), i18n.T("auth.page_rejected_body", nil)
	}

	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>%s</title></head><body><h1>%s</h1><p>%s</p></body></html>`,
		html.EscapeString(title), html.EscapeString(title), html.EscapeString(body))
}
