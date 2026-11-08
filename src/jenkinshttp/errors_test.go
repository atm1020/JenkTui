package jenkinshttp

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplainNamesTheSettingThatFixesEachCause(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "untrusted CA",
			err:  &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}},
			want: "ca_cert_path",
		},
		{
			name: "hostname mismatch",
			err:  &tls.CertificateVerificationError{Err: x509.HostnameError{Host: "jenkins.example"}},
			want: "jenkins_url",
		},
		{
			name: "expired certificate",
			err:  &tls.CertificateVerificationError{Err: x509.CertificateInvalidError{Reason: x509.Expired}},
			want: "expired",
		},
		{
			name: "plain HTTP behind an https URL",
			err:  fmt.Errorf("get: %w", http.ErrSchemeMismatch),
			want: "http://",
		},
		{
			name: "unnamed verification failure",
			err:  &tls.CertificateVerificationError{Err: errors.New("some other defect")},
			want: "ca_cert_path",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message, ok := Explain(test.err)
			if !ok {
				t.Fatalf("Explain(%v) was not recognized", test.err)
			}
			if !strings.Contains(message, test.want) {
				t.Errorf("message = %q, want it to mention %q", message, test.want)
			}
		})
	}
}

func TestExplainDistinguishesHostnameFromUntrustedCA(t *testing.T) {
	untrusted, _ := Explain(&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}})
	hostname, _ := Explain(&tls.CertificateVerificationError{Err: x509.HostnameError{Host: "jenkins.example"}})
	if untrusted == hostname {
		t.Fatalf("both causes produced %q; the remedies differ", untrusted)
	}
}

func TestExplainSeesThroughWrapping(t *testing.T) {
	wrapped := fmt.Errorf(`Get "https://jenkins.example/api/json": %w`,
		&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}})
	if _, ok := Explain(wrapped); !ok {
		t.Fatal("a wrapped certificate error was not recognized")
	}
}

func TestExplainLeavesUnrelatedErrorsToTheCaller(t *testing.T) {
	if _, ok := Explain(errors.New("connection refused")); ok {
		t.Error("an unrelated error must not be reported as recognized")
	}
	if _, ok := Explain(nil); ok {
		t.Error("nil must not be reported as recognized")
	}
}

func TestExplainMessagesDoNotEchoTheError(t *testing.T) {
	wrapped := fmt.Errorf(`Get "https://user:token@jenkins.example/api/json": %w`,
		&tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}})
	message, ok := Explain(wrapped)
	if !ok {
		t.Fatal("expected a recognized cause")
	}
	for _, secret := range []string{"jenkins.example", "token", "https://"} {
		if strings.Contains(message, secret) {
			t.Errorf("message %q leaked %q from the underlying error", message, secret)
		}
	}
}

func TestDescribeReplacesOnlyRecognizedCauses(t *testing.T) {
	recognized := &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}
	if got := Describe(recognized); got.Error() == recognized.Error() {
		t.Error("a recognized cause was not replaced with its explanation")
	}

	other := errors.New("connection refused")
	if got := Describe(other); !errors.Is(got, other) {
		t.Errorf("Describe(%v) = %v, want the original error", other, got)
	}
	if Describe(nil) != nil {
		t.Error("Describe(nil) must stay nil")
	}
}

func TestNewReportsDistinguishableCACauses(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.pem")
	_, err := New(Options{CACertPath: missing})
	if !errors.Is(err, ErrCAUnreadable) {
		t.Errorf("missing file error = %v, want ErrCAUnreadable", err)
	}
	if errors.Is(err, ErrCANotPEM) {
		t.Error("a missing file must not be reported as invalid PEM")
	}

	notPEM := filepath.Join(t.TempDir(), "not-pem.pem")
	if err := os.WriteFile(notPEM, []byte("CA certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = New(Options{CACertPath: notPEM})
	if !errors.Is(err, ErrCANotPEM) {
		t.Errorf("invalid PEM error = %v, want ErrCANotPEM", err)
	}
	if errors.Is(err, ErrCAUnreadable) {
		t.Error("a readable file must not be reported as unreadable")
	}
}
