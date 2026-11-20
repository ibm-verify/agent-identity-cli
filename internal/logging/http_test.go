package logging

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// httpLogger installs a logger at level that writes to the returned buffer.
func httpLogger(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level, ReplaceAttr: replaceAttr})
	slog.SetDefault(slog.New(h))
	return &buf
}

func client() *http.Client {
	return &http.Client{Transport: RoundTripper(nil)}
}

// secrets are values that must never appear in any log.
var secrets = []string{
	"SECRET-CLIENT-SECRET", "SECRET-AUTH-CODE", "SECRET-REFRESH-IN", "SECRET-ASSERTION",
	"SECRET-ACCESS-OUT", "SECRET-REFRESH-OUT", "SECRET-ID-OUT", "SECRET-BEARER", "SECRET-APIKEY",
	"SECRET-COOKIE", "SECRET-VERIFIER",
}

func assertNoSecrets(t *testing.T, log string) {
	t.Helper()
	for _, s := range secrets {
		if strings.Contains(log, s) {
			t.Errorf("secret %q leaked into the log:\n%s", s, log)
		}
	}
}

func tokenServer(t *testing.T, gotBody *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if gotBody != nil {
			*gotBody = string(b)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Correlation-ID", "corr-12345")
		w.Header().Set("Set-Cookie", "session=SECRET-COOKIE")
		_, _ = w.Write([]byte(`{"access_token":"SECRET-ACCESS-OUT","refresh_token":"SECRET-REFRESH-OUT",` +
			`"id_token":"SECRET-ID-OUT","token_type":"Bearer","expires_in":300,"scope":"openid"}`))
	}))
}

func postToken(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	form := "grant_type=authorization_code&code=SECRET-AUTH-CODE&client_secret=SECRET-CLIENT-SECRET" +
		"&refresh_token=SECRET-REFRESH-IN&client_assertion=SECRET-ASSERTION&code_verifier=SECRET-VERIFIER"
	req, err := http.NewRequest(http.MethodPost, url+"/token?code=SECRET-AUTH-CODE&state=visible", strings.NewReader(form))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer SECRET-BEARER")
	req.Header.Set("X-Api-Key", "SECRET-APIKEY")

	resp, err := client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

func TestHTTPInfoLogsSummaryWithCorrelationID(t *testing.T) {
	log := httpLogger(t, slog.LevelInfo)
	srv := tokenServer(t, nil)
	defer srv.Close()

	postToken(t, srv.URL)

	out := log.String()
	for _, want := range []string{
		"msg=\"http request\"", "method=POST", "path=/token", "status=200",
		"duration_ms=", "correlation_id=corr-12345", "host=127.0.0.1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("info log should contain %q:\n%s", want, out)
		}
	}

	// At info there is no query, no headers and no bodies at all.
	for _, forbidden := range []string{"query", "headers", "request_body", "response_body", "state=visible", "Authorization"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("info log must not contain %q:\n%s", forbidden, out)
		}
	}
	assertNoSecrets(t, out)
}

func TestHTTPDebugLogsDetailWithoutSecrets(t *testing.T) {
	log := httpLogger(t, slog.LevelDebug)
	srv := tokenServer(t, nil)
	defer srv.Close()

	postToken(t, srv.URL)

	out := log.String()
	assertNoSecrets(t, out)

	for _, want := range []string{
		"msg=\"http request detail\"", "msg=\"http response body\"",
		"state=visible", "code=[REDACTED]", // query, redacted
		"grant_type=authorization_code", // request body, non-sensitive part kept
		"client_secret=[REDACTED]",
		"request_headers.Authorization=\"Bearer [REDACTED]\"",
		"response_headers.X-Correlation-Id=corr-12345",
		"token_type", "expires_in", // response body, non-sensitive part kept
	} {
		if !strings.Contains(out, want) {
			t.Errorf("debug log should contain %q:\n%s", want, out)
		}
	}
}

func TestHTTPDoesNotChangeWhatTheServerReceivesOrTheCallerReads(t *testing.T) {
	httpLogger(t, slog.LevelDebug)

	var received string
	srv := tokenServer(t, &received)
	defer srv.Close()

	_, body := postToken(t, srv.URL)

	if !strings.Contains(received, "client_secret=SECRET-CLIENT-SECRET") ||
		!strings.Contains(received, "code=SECRET-AUTH-CODE") {
		t.Errorf("the server must receive the original, unredacted request body: %q", received)
	}
	if !strings.Contains(body, "SECRET-ACCESS-OUT") || !strings.Contains(body, `"expires_in":300`) {
		t.Errorf("the caller must read the original, unredacted response: %q", body)
	}
}

func TestHTTPBodyRules(t *testing.T) {
	big := strings.Repeat("a", maxBodyLog+1)
	tests := []struct {
		name, contentType, body, want string
	}{
		{"binary type omitted", "application/octet-stream", "SECRET-ACCESS-OUT-binary", "body omitted: application/octet-stream"},
		{"image omitted", "image/png", "SECRET-ACCESS-OUT-png", "body omitted: image/png"},
		{"oversize omitted", "application/json", `{"v":"` + big + `"}`, "body omitted: larger than 32 KiB"},
		{"plain text redacted", "text/plain", "client_secret=SECRET-CLIENT-SECRET ok", "client_secret=[REDACTED]"},
		{"json suffix type logged", "application/problem+json", `{"detail":"x","password":"SECRET-APIKEY"}`, `password\":\"[REDACTED]`}, // the text handler escapes the quotes
		{"invalid json falls back to text", "application/json", `not json access_token=SECRET-ACCESS-OUT`, "access_token=[REDACTED]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := httpLogger(t, slog.LevelDebug)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()

			resp, err := client().Get(srv.URL + "/x")
			if err != nil {
				t.Fatal(err)
			}
			got, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if string(got) != tt.body {
				t.Errorf("the caller's response was altered (len %d, want %d)", len(got), len(tt.body))
			}
			out := log.String()
			if !strings.Contains(out, tt.want) {
				t.Errorf("log should contain %q:\n%.600s", tt.want, out)
			}
			assertNoSecrets(t, out)
		})
	}
}

func TestHTTPRequestBodyOmittedWhenUnknownOrHuge(t *testing.T) {
	log := httpLogger(t, slog.LevelDebug)
	var gotLen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotLen = len(b)
	}))
	defer srv.Close()

	// A streamed body has no known length, so it must be neither read nor logged.
	pr, pw := io.Pipe()
	go func() {
		_, _ = io.WriteString(pw, `{"client_secret":"SECRET-CLIENT-SECRET"}`)
		pw.Close()
	}()
	req, _ := http.NewRequest(http.MethodPost, srv.URL, pr)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if gotLen == 0 {
		t.Error("the server must still receive the streamed body")
	}
	if !strings.Contains(log.String(), "body omitted: unknown length") {
		t.Errorf("expected an omitted-body note:\n%s", log.String())
	}
	assertNoSecrets(t, log.String())
}

func TestHTTPResponseBodyNotBufferedAhead(t *testing.T) {
	httpLogger(t, slog.LevelDebug)

	first := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, "one\n")
		w.(http.Flusher).Flush()
		close(first)
		<-release
		_, _ = io.WriteString(w, "two\n")
	}))
	defer srv.Close()
	defer close(release)

	resp, err := client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	<-first
	buf := make([]byte, 4)
	n, err := io.ReadFull(resp.Body, buf)
	if err != nil || string(buf[:n]) != "one\n" {
		t.Fatalf("the first chunk must be readable before the response ends: %q %v", buf[:n], err)
	}
}

func TestHTTPErrorsAreLoggedAndReturned(t *testing.T) {
	log := httpLogger(t, slog.LevelInfo)
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // connection refused

	_, err := client().Get(url + "/x?client_secret=SECRET-CLIENT-SECRET")
	if err == nil {
		t.Fatal("expected an error")
	}
	out := log.String()
	if !strings.Contains(out, "http request failed") || !strings.Contains(out, "level=ERROR") {
		t.Errorf("failure should be logged at error level:\n%s", out)
	}
	assertNoSecrets(t, out) // the error text embeds the URL
}

func TestHTTPDisabledLoggerIsPassThrough(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	slog.SetDefault(slog.New(slog.DiscardHandler))

	var received string
	srv := tokenServer(t, &received)
	defer srv.Close()

	_, body := postToken(t, srv.URL)
	if !strings.Contains(received, "SECRET-AUTH-CODE") || !strings.Contains(body, "SECRET-ACCESS-OUT") {
		t.Error("a disabled logger must not alter requests or responses")
	}
}

func TestHTTPRequestIsNotMutated(t *testing.T) {
	httpLogger(t, slog.LevelDebug)

	var seen *http.Request
	next := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		seen = r
		return &http.Response{StatusCode: 204, Header: http.Header{}, Body: http.NoBody}, nil
	})
	body := strings.NewReader("a=b")
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://example.invalid/", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	origBody := req.Body

	if _, err := RoundTripper(next).RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if req.Body != origBody {
		t.Error("the caller's request must not be modified (http.RoundTripper contract)")
	}
	if seen == nil || seen == req {
		t.Error("the wrapped transport should receive a clone when a body was captured")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
