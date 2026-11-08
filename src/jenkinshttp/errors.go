package jenkinshttp

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
)

// Explain returns a credential-safe explanation and whether the cause was
// recognized. Callers must sanitize unrecognized errors before displaying them.
func Explain(err error) (string, bool) {
	var unknownCA x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var verification *tls.CertificateVerificationError
	switch {
	case errors.Is(err, http.ErrSchemeMismatch):
		return "the server answered with plain HTTP; change jenkins_url to use http://", true
	case errors.As(err, &unknownCA):
		return "the server's certificate was issued by an unknown authority; set ca_cert_path to your organization's CA certificate", true
	case errors.As(err, &hostname):
		return "the server's certificate does not cover this hostname; check that jenkins_url matches the certificate", true
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired:
		return "the server's certificate has expired; contact your Jenkins administrator", true
	case errors.As(err, &verification):
		return "the server's certificate could not be verified; check ca_cert_path", true
	}
	return "", false
}

// Describe replaces recognized errors with an explanation, returning others
// unchanged. Use Explain when raw transport errors must not be disclosed.
func Describe(err error) error {
	if message, ok := Explain(err); ok {
		return errors.New(message)
	}
	return err
}
