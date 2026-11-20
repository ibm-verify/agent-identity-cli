package oauth

import (
	"crypto/tls"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

func tlsServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	caPath := filepath.Join(t.TempDir(), "ca.pem")
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(caPath, block, 0o600); err != nil {
		t.Fatal(err)
	}
	return srv, caPath
}

func TestHTTPClientCACert(t *testing.T) {
	srv, caPath := tlsServer(t)

	t.Run("fails without the CA", func(t *testing.T) {
		hc, err := NewHTTPClient(profile.TLSConfig{})
		if err != nil {
			t.Fatal(err)
		}
		if resp, err := hc.Get(srv.URL); err == nil {
			resp.Body.Close()
			t.Fatal("an unknown CA must be rejected")
		}
	})

	t.Run("works with the CA", func(t *testing.T) {
		hc, err := NewHTTPClient(profile.TLSConfig{CACert: profile.Source("@" + caPath)})
		if err != nil {
			t.Fatal(err)
		}
		resp, err := hc.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	})
}

func TestHTTPClientInlineCACert(t *testing.T) {
	srv, caPath := tlsServer(t)
	pemBytes, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatal(err)
	}

	hc, err := NewHTTPClient(profile.TLSConfig{CACert: profile.Source(pemBytes)})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := hc.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	_, err = NewHTTPClient(profile.TLSConfig{CACert: "no certificates here"})
	if err == nil {
		t.Fatal("want an error")
	}
	mustContain(t, err.Error(), "tls.ca_cert", "message should name the property")
}

func TestHTTPClientInsecureSkipVerifyWarns(t *testing.T) {
	logs := capture(t)
	srv, _ := tlsServer(t)

	hc, err := NewHTTPClient(profile.TLSConfig{InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := hc.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	mustContain(t, logs.String(), "verification is disabled", "logs")
}

func TestHTTPClientDefaultsDoNotWarn(t *testing.T) {
	logs := capture(t)
	if _, err := NewHTTPClient(profile.TLSConfig{}); err != nil {
		t.Fatal(err)
	}
	mustNotContain(t, logs.String(), "verification is disabled", "logs")
}

func TestHTTPClientBadCA(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(empty, []byte("no certificates here"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "absent.pem")

	for name, path := range map[string]string{"no certificates": empty, "unreadable": missing} {
		t.Run(name, func(t *testing.T) {
			_, err := NewHTTPClient(profile.TLSConfig{CACert: profile.Source("@" + path)})
			if err == nil {
				t.Fatal("want an error")
			}
			mustContain(t, err.Error(), path, "message should name the file")
		})
	}
}

func TestHTTPClientMinimumTLSVersion(t *testing.T) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.TLS = &tls.Config{MaxVersion: tls.VersionTLS11}
	srv.StartTLS()
	defer srv.Close()

	// Trust the server's certificate so only the protocol version can fail.
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(caPath, block, 0o600); err != nil {
		t.Fatal(err)
	}
	hc, err := NewHTTPClient(profile.TLSConfig{CACert: profile.Source("@" + caPath)})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := hc.Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("TLS 1.1 must be refused")
	}
	mustContain(t, err.Error(), "version", "the failure should be about the protocol version")
}

func TestHTTPClientLogsRequestsWithoutSecrets(t *testing.T) {
	logs := capture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hc, err := NewHTTPClient(profile.TLSConfig{})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/thing?access_token=leaky", nil)
	req.Header.Set("Authorization", "Bearer leaky-bearer")
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	mustContain(t, logs.String(), "/thing", "requests should be logged")
	mustNotContain(t, logs.String(), "leaky", "logs")
}
