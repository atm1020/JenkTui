package localca

import (
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func mustNew(t *testing.T) *Authority {
	t.Helper()
	ca, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return ca
}

func TestIssuedCertificateIsTrustedOnlyViaTheCA(t *testing.T) {
	ca := mustNew(t)
	server, err := ca.TLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}), LeafOptions{})
	if err != nil {
		t.Fatalf("TLSServer: %v", err)
	}
	defer server.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	response, err := ca.Client().Do(request)
	if err != nil {
		t.Fatalf("request with the CA trusted failed: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "ok" {
		t.Fatalf("body = %q, err = %v, want %q", body, err, "ok")
	}

	response, err = http.DefaultClient.Do(request)
	if response != nil {
		defer response.Body.Close()
	}
	var unknown x509.UnknownAuthorityError
	if !errors.As(err, &unknown) {
		t.Fatalf("default client error = %v, want an unknown-authority error", err)
	}
}

func TestLeafCoversOnlyTheNamedHosts(t *testing.T) {
	ca := mustNew(t)
	server, err := ca.TLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		LeafOptions{Hosts: []string{"jenkins.internal"}})
	if err != nil {
		t.Fatalf("TLSServer: %v", err)
	}
	defer server.Close()

	// The CA is trusted, so only the missing SAN can fail the handshake.
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	response, err := ca.Client().Do(request)
	if response != nil {
		defer response.Body.Close()
	}
	var hostnameErr x509.HostnameError
	if !errors.As(err, &hostnameErr) {
		t.Fatalf("error = %v, want a hostname error", err)
	}
}

func TestExpiredLeafIsReportedAsExpired(t *testing.T) {
	ca := mustNew(t)
	server, err := ca.TLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), LeafOptions{
		NotBefore: time.Now().Add(-48 * time.Hour),
		NotAfter:  time.Now().Add(-24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("TLSServer: %v", err)
	}
	defer server.Close()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	response, err := ca.Client().Do(request)
	if response != nil {
		defer response.Body.Close()
	}
	var invalid x509.CertificateInvalidError
	if !errors.As(err, &invalid) || invalid.Reason != x509.Expired {
		t.Fatalf("error = %v, want an expired-certificate error", err)
	}
}

func TestDefaultLeafCoversLocalhostAndLoopback(t *testing.T) {
	leaf, err := mustNew(t).IssueLeaf(LeafOptions{})
	if err != nil {
		t.Fatalf("IssueLeaf: %v", err)
	}
	cert, err := x509.ParseCertificate(leaf.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := cert.VerifyHostname("localhost"); err != nil {
		t.Errorf("default leaf does not cover localhost: %v", err)
	}
	if err := cert.VerifyHostname("127.0.0.1"); err != nil {
		t.Errorf("default leaf does not cover 127.0.0.1: %v", err)
	}
}

func TestWriteAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ca := mustNew(t)
	certPath := filepath.Join(dir, "nested", "ca.pem")
	keyPath := filepath.Join(dir, "nested", "ca-key.pem")
	if _, err := ca.WriteCertPEM(certPath); err != nil {
		t.Fatalf("WriteCertPEM: %v", err)
	}
	if _, err := ca.WriteKeyPEM(keyPath); err != nil {
		t.Fatalf("WriteKeyPEM: %v", err)
	}

	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("CA key mode = %o, want 600", mode)
	}

	reloaded, err := Load(certPath, keyPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	server, err := reloaded.TLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), LeafOptions{})
	if err != nil {
		t.Fatalf("TLSServer: %v", err)
	}
	defer server.Close()

	// The original authority must trust a leaf the reloaded copy issued.
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	response, err := ca.Client().Do(request)
	if err != nil {
		t.Fatalf("reloaded CA issued a leaf the original does not trust: %v", err)
	}
	defer response.Body.Close()
}

func TestLoadRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	notPEM := filepath.Join(dir, "not-pem.pem")
	if err := os.WriteFile(notPEM, []byte("CA certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(notPEM, notPEM); err == nil {
		t.Error("Load accepted a file that is not PEM")
	}
	if _, err := Load(filepath.Join(dir, "absent.pem"), notPEM); err == nil {
		t.Error("Load accepted a missing certificate")
	}
}
