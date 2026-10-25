// Package jenkinsclient owns the HTTP clients used to connect to Jenkins.
package jenkinsclient

import "net/http"

const (
	defaultMaxIdleConnsPerHost = 8
	defaultMaxIdleConns        = 32
)

// Options tunes the connection pool. Zero values take the package defaults.
type Options struct {
	MaxIdleConnsPerHost int
	MaxIdleConns        int
}

// New returns an HTTP client with its own transport and connection pool.
func New(opts Options) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if opts.MaxIdleConnsPerHost <= 0 {
		opts.MaxIdleConnsPerHost = defaultMaxIdleConnsPerHost
	}
	if opts.MaxIdleConns <= 0 {
		opts.MaxIdleConns = defaultMaxIdleConns
	}
	transport.MaxIdleConnsPerHost = opts.MaxIdleConnsPerHost
	transport.MaxIdleConns = opts.MaxIdleConns
	return &http.Client{Transport: transport}
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
