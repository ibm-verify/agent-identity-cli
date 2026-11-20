package oauth

import (
	"crypto/tls"
	"crypto/x509"
	"log/slog"
	"net/http"
	"time"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
	"github.com/ibm-verify/agent-identity-cli/internal/logging"
	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

const requestTimeout = 30 * time.Second

// caOrigin names where a CA bundle came from for a message: the file path, or
// "tls.ca_cert" for an inline value.
func caOrigin(src profile.Source) string {
	if src.IsFile() {
		return src.Path()
	}
	return "tls.ca_cert"
}

// NewHTTPClient builds the HTTP client for a context. It applies the context's
// TLS settings and logs every request through the logging package.
func NewHTTPClient(settings profile.TLSConfig) (*http.Client, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}

	if settings.CACert != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		pem, err := settings.CACert.Read()
		if err != nil {
			return nil, err
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, i18n.Errorf("auth.ca_no_certs", map[string]any{"Where": caOrigin(settings.CACert)})
		}
		tlsCfg.RootCAs = pool
	}

	if settings.InsecureSkipVerify {
		tlsCfg.InsecureSkipVerify = true //nolint:gosec // deliberate, documented for local development
		slog.Warn("TLS certificate verification is disabled for this context")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsCfg

	return &http.Client{
		Transport: logging.RoundTripper(transport),
		Timeout:   requestTimeout,
	}, nil
}
