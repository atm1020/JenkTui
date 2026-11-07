// Package localca generates private CAs and server certificates for development
// and tests. Generated keys are unprotected and must not be used in production.
package localca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"
)

// DefaultValidity is the default certificate lifetime.
const DefaultValidity = 365 * 24 * time.Hour

// DefaultHosts lists the default server certificate names and addresses.
var DefaultHosts = []string{"localhost", "127.0.0.1", "::1"}

// Options describes the authority certificate.
type Options struct {
	CommonName string    // default "JenkTui Development CA"
	NotBefore  time.Time // default one hour ago, tolerating small clock skew
	NotAfter   time.Time // default NotBefore plus DefaultValidity
}

// LeafOptions describes a server certificate signed by the authority.
type LeafOptions struct {
	CommonName string    // default the first host
	Hosts      []string  // DNS names and IP addresses; default DefaultHosts
	NotBefore  time.Time // default one hour ago
	NotAfter   time.Time // default NotBefore plus DefaultValidity
}

// Authority is a certificate authority held in memory.
type Authority struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
	keyPEM  []byte
}

// New creates a self-signed authority.
func New(opts Options) (*Authority, error) {
	notBefore, notAfter := validity(opts.NotBefore, opts.NotAfter)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate CA key: %w", err)
	}
	serial, err := serialNumber()
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: orDefault(opts.CommonName, "JenkTui Development CA")},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create CA certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse CA certificate: %w", err)
	}
	keyPEM, err := encodeKey(key)
	if err != nil {
		return nil, err
	}
	return &Authority{
		cert:    cert,
		key:     key,
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		keyPEM:  keyPEM,
	}, nil
}

// CertPEM returns the PEM-encoded authority certificate.
func (a *Authority) CertPEM() []byte { return a.certPEM }

// KeyPEM returns the PEM-encoded authority private key.
func (a *Authority) KeyPEM() []byte { return a.keyPEM }

// Certificate returns the parsed authority certificate.
func (a *Authority) Certificate() *x509.Certificate { return a.cert }

// CertPool returns a pool trusting only this authority.
func (a *Authority) CertPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(a.cert)
	return pool
}

// WriteCertPEM writes the authority certificate, creating parent directories.
func (a *Authority) WriteCertPEM(path string) (string, error) {
	return writeFile(path, a.certPEM, 0o644)
}

// WriteKeyPEM writes the authority private key with owner-only permissions.
func (a *Authority) WriteKeyPEM(path string) (string, error) {
	return writeFile(path, a.keyPEM, 0o600)
}

// IssueLeafPEM returns a PEM-encoded server certificate and its private key.
func (a *Authority) IssueLeafPEM(opts LeafOptions) (certPEM, keyPEM []byte, err error) {
	hosts := opts.Hosts
	if len(hosts) == 0 {
		hosts = DefaultHosts
	}
	notBefore, notAfter := validity(opts.NotBefore, opts.NotAfter)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate leaf key: %w", err)
	}
	serial, err := serialNumber()
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: orDefault(opts.CommonName, hosts[0])},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
			continue
		}
		template.DNSNames = append(template.DNSNames, host)
	}
	der, err := x509.CreateCertificate(rand.Reader, template, a.cert, &key.PublicKey, a.key)
	if err != nil {
		return nil, nil, fmt.Errorf("create leaf certificate: %w", err)
	}
	keyPEM, err = encodeKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), keyPEM, nil
}

// IssueLeaf returns a certificate ready for a tls.Config.
func (a *Authority) IssueLeaf(opts LeafOptions) (tls.Certificate, error) {
	certPEM, keyPEM, err := a.IssueLeafPEM(opts)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}

// WriteLeaf writes a leaf certificate and key, returning both paths.
func (a *Authority) WriteLeaf(opts LeafOptions, certPath, keyPath string) (string, string, error) {
	certPEM, keyPEM, err := a.IssueLeafPEM(opts)
	if err != nil {
		return "", "", err
	}
	writtenCert, err := writeFile(certPath, certPEM, 0o644)
	if err != nil {
		return "", "", err
	}
	writtenKey, err := writeFile(keyPath, keyPEM, 0o600)
	if err != nil {
		return "", "", err
	}
	return writtenCert, writtenKey, nil
}

// TLSServer starts an httptest server presenting a leaf signed by this
// authority. The caller closes the returned server.
func (a *Authority) TLSServer(handler http.Handler, opts LeafOptions) (*httptest.Server, error) {
	leaf, err := a.IssueLeaf(opts)
	if err != nil {
		return nil, err
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{leaf}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	return server, nil
}

// Client returns an HTTP client that trusts only this authority.
func (a *Authority) Client() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	}
	transport.TLSClientConfig.RootCAs = a.CertPool()
	transport.TLSClientConfig.MinVersion = tls.VersionTLS12
	return &http.Client{Transport: transport}
}

// Load reads an authority written by WriteCertPEM and WriteKeyPEM.
func Load(certPath, keyPath string) (*Authority, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("read CA certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read CA key: %w", err)
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, errors.New("CA certificate is not valid PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA certificate: %w", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, errors.New("CA key is not valid PEM")
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA key: %w", err)
	}
	key, ok := parsedKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("CA key is %T, want an ECDSA key", parsedKey)
	}
	return &Authority{cert: cert, key: key, certPEM: certPEM, keyPEM: keyPEM}, nil
}

func validity(notBefore, notAfter time.Time) (time.Time, time.Time) {
	if notBefore.IsZero() {
		notBefore = time.Now().Add(-time.Hour)
	}
	if notAfter.IsZero() {
		notAfter = notBefore.Add(DefaultValidity + time.Hour)
	}
	return notBefore, notAfter
}

func serialNumber() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generate serial number: %w", err)
	}
	return serial, nil
}

func encodeKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func writeFile(path string, content []byte, mode os.FileMode) (string, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
