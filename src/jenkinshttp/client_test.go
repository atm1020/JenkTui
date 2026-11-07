package jenkinshttp

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/dev/localca"
)

func mustNew(t *testing.T, opts Options) *http.Client {
	t.Helper()
	client, err := New(opts)
	if err != nil {
		t.Fatalf("New(%+v): %v", opts, err)
	}
	return client
}

func TestNewOwnsIndependentTransportWithDefaults(t *testing.T) {
	defaultTransport := http.DefaultTransport.(*http.Transport)
	before := defaultTransport.Clone()

	first := mustNew(t, Options{})
	second := mustNew(t, Options{})
	firstTransport, ok := first.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", first.Transport)
	}
	secondTransport, ok := second.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", second.Transport)
	}

	if firstTransport == defaultTransport || secondTransport == defaultTransport {
		t.Fatal("New reused http.DefaultTransport")
	}
	if firstTransport == secondTransport {
		t.Fatal("two clients share one transport")
	}
	if firstTransport.MaxIdleConnsPerHost != 8 {
		t.Errorf("MaxIdleConnsPerHost = %d, want 8", firstTransport.MaxIdleConnsPerHost)
	}
	if firstTransport.MaxIdleConns != 32 {
		t.Errorf("MaxIdleConns = %d, want 32", firstTransport.MaxIdleConns)
	}
	if firstTransport.MaxConnsPerHost != 0 {
		t.Errorf("MaxConnsPerHost = %d, want unlimited", firstTransport.MaxConnsPerHost)
	}
	if first.Timeout != 0 {
		t.Errorf("Client.Timeout = %v, want zero", first.Timeout)
	}
	if defaultTransport.MaxIdleConnsPerHost != before.MaxIdleConnsPerHost ||
		defaultTransport.MaxIdleConns != before.MaxIdleConns ||
		defaultTransport.MaxConnsPerHost != before.MaxConnsPerHost {
		t.Fatal("New mutated http.DefaultTransport")
	}
}

func TestNewHonorsExplicitOptions(t *testing.T) {
	client := mustNew(t, Options{MaxIdleConnsPerHost: 12, MaxIdleConns: 48})
	transport := client.Transport.(*http.Transport)
	if transport.MaxIdleConnsPerHost != 12 || transport.MaxIdleConns != 48 {
		t.Fatalf("pool options = (%d, %d), want (12, 48)", transport.MaxIdleConnsPerHost, transport.MaxIdleConns)
	}
}

func TestCloseIdleToleratesNilAndOtherTransports(t *testing.T) {
	CloseIdle(nil)
	CloseIdle(&http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	})})
}

func TestClientReachesServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	response, err := mustNew(t, Options{}).Do(request)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q, want ok", body)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestNewAppendsCAToSystemRootsWithoutReplacingThem(t *testing.T) {
	system, err := x509.SystemCertPool()
	if err != nil {
		t.Skipf("no system cert pool on this platform: %v", err)
	}
	certPath := filepath.Join(t.TempDir(), "company-ca.pem")
	if err := os.WriteFile(certPath, caPEM(t), 0o600); err != nil {
		t.Fatal(err)
	}

	client := mustNew(t, Options{CACertPath: certPath})

	config := client.Transport.(*http.Transport).TLSClientConfig
	if config == nil || config.RootCAs == nil {
		t.Fatal("configured CA did not reach the transport")
	}
	if got, want := len(config.RootCAs.Subjects()), len(system.Subjects())+1; got != want { //nolint:staticcheck // Subjects is the only way to count a pool
		t.Errorf("root pool holds %d certificates, want %d (system roots plus the configured CA)", got, want)
	}
	if config.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want TLS 1.2", config.MinVersion)
	}
}

func TestNewRejectsUnusableCA(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.pem")
	if _, err := New(Options{CACertPath: missing}); err == nil || !strings.Contains(err.Error(), missing) {
		t.Errorf("missing file error = %v, want one naming %s", err, missing)
	}

	notPEM := filepath.Join(t.TempDir(), "not-pem.pem")
	if err := os.WriteFile(notPEM, []byte("CA certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Options{CACertPath: notPEM}); err == nil || !strings.Contains(err.Error(), "PEM") {
		t.Errorf("invalid PEM error = %v, want one naming PEM", err)
	}
}

func TestNewWithCAPreservesALPNProtocols(t *testing.T) {
	certPath := filepath.Join(t.TempDir(), "company-ca.pem")
	if err := os.WriteFile(certPath, caPEM(t), 0o600); err != nil {
		t.Fatal(err)
	}

	withoutCA := mustNew(t, Options{}).Transport.(*http.Transport)
	withCA := mustNew(t, Options{CACertPath: certPath}).Transport.(*http.Transport)

	var want []string
	if withoutCA.TLSClientConfig != nil {
		want = withoutCA.TLSClientConfig.NextProtos
	}
	if len(want) == 0 {
		t.Skip("default transport negotiates no ALPN protocols on this platform")
	}
	if got := withCA.TLSClientConfig.NextProtos; !slices.Equal(got, want) {
		t.Errorf("NextProtos with a CA = %v, want %v", got, want)
	}
}

func caPEM(t *testing.T) []byte {
	t.Helper()
	ca, err := localca.New(localca.Options{})
	if err != nil {
		t.Fatalf("localca.New: %v", err)
	}
	return ca.CertPEM()
}
