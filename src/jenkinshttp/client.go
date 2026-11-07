// Package jenkinshttp owns the HTTP clients used to connect to Jenkins.
package jenkinshttp

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
)

const (
	defaultMaxIdleConnsPerHost = 8
	defaultMaxIdleConns        = 32
)

// Errors returned for an unreadable or invalid CA certificate.
var (
	ErrCAUnreadable = errors.New("CA certificate cannot be read")
	ErrCANotPEM     = errors.New("CA certificate is not valid PEM")
)

// Options tunes the connection pool. Zero values take the package defaults.
type Options struct {
	MaxIdleConnsPerHost int
	MaxIdleConns        int

	// CACertPath is a PEM-encoded CA certificate or bundle to trust in addition
	// to the system roots. Empty means system roots only.
	CACertPath string
}

// New returns an HTTP client with its own transport and connection pool.
// It fails rather than silently falling back to system roots when a configured
// CA certificate cannot be read or parsed.
func New(opts Options) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if opts.MaxIdleConnsPerHost <= 0 {
		opts.MaxIdleConnsPerHost = defaultMaxIdleConnsPerHost
	}
	if opts.MaxIdleConns <= 0 {
		opts.MaxIdleConns = defaultMaxIdleConns
	}
	transport.MaxIdleConnsPerHost = opts.MaxIdleConnsPerHost
	transport.MaxIdleConns = opts.MaxIdleConns
	if opts.CACertPath != "" {
		roots, err := trustStore(opts.CACertPath)
		if err != nil {
			return nil, err
		}
		// Modify the cloned config rather than replacing it: the clone carries
		// the ALPN protocols that enable HTTP/2, and a fresh tls.Config would
		// silently drop them.
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		transport.TLSClientConfig.RootCAs = roots
		if transport.TLSClientConfig.MinVersion == 0 {
			transport.TLSClientConfig.MinVersion = tls.VersionTLS12
		}
	}
	return &http.Client{Transport: transport}, nil
}

// trustStore adds the certificates in certPath to the system roots.
func trustStore(certPath string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %w", ErrCAUnreadable, certPath, err)
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("%w: %q", ErrCANotPEM, certPath)
	}
	return roots, nil
}

// CloseIdle releases the client's pooled connections. It is safe on a nil
// client or a client backed by a different transport implementation.
func CloseIdle(client *http.Client) {
	if client == nil {
		return
	}
	if transport, ok := client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}
