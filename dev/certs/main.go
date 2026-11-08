// Command certs issues TLS material for the local Jenkins stack using a private CA.
//
// The generated keys are unprotected development material. They live under
// dev/docker/tls, which is gitignored, and must never be committed or reused
// anywhere real.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atm1020/jenktui/dev/localca"
)

const usage = `certs — issue TLS material for the local Jenkins stack.

  go run ./dev/certs [flags]

Writes four files into -dir:
  ca.pem          the CA certificate; point ca_cert_path at this
  ca-key.pem      the CA key, kept so -reuse-ca can issue more leaves
  server.pem      the certificate the TLS proxy serves
  server-key.pem  its key

Flags:
  -dir     output directory (default dev/docker/tls)
  -hosts   comma-separated names and IPs the server certificate covers
           (default localhost,127.0.0.1,::1,jenkins.local,jenkins-tls)
  -mode    valid      a correct certificate (default)
           expired    already past its NotAfter
           wrong-host covers jenkins.internal only, so the hostname never matches
           Use the last two to see how JenkTui reports each failure.
  -reuse-ca  keep the existing CA and issue only a new server certificate, so a
             saved ca_cert_path stays valid
  -force     overwrite existing files (implied unless files already exist)

Examples:
  go run ./dev/certs                      # fresh CA + server certificate
  go run ./dev/certs -mode expired        # reproduce the expiry error message
  go run ./dev/certs -reuse-ca -mode wrong-host
`

type mode string

const (
	modeValid     mode = "valid"
	modeExpired   mode = "expired"
	modeWrongHost mode = "wrong-host"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "certs: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("certs", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() { fmt.Fprint(stdout, usage) }
	dir := fs.String("dir", filepath.Join("dev", "docker", "tls"), "output directory")
	hosts := fs.String("hosts", "localhost,127.0.0.1,::1,jenkins.local,jenkins-tls", "hosts the server certificate covers")
	modeFlag := fs.String("mode", string(modeValid), "valid, expired or wrong-host")
	reuseCA := fs.Bool("reuse-ca", false, "keep the existing CA and reissue only the server certificate")
	force := fs.Bool("force", false, "overwrite existing files")
	if err := fs.Parse(args); err != nil {
		return err
	}

	selected := mode(*modeFlag)
	switch selected {
	case modeValid, modeExpired, modeWrongHost:
	default:
		return fmt.Errorf("unknown -mode %q; want valid, expired or wrong-host", *modeFlag)
	}

	caCertPath := filepath.Join(*dir, "ca.pem")
	caKeyPath := filepath.Join(*dir, "ca-key.pem")
	serverCertPath := filepath.Join(*dir, "server.pem")
	serverKeyPath := filepath.Join(*dir, "server-key.pem")

	ca, reused, err := authority(caCertPath, caKeyPath, *reuseCA, *force)
	if err != nil {
		return err
	}
	if !reused {
		if _, err := ca.WriteCertPEM(caCertPath); err != nil {
			return err
		}
		if _, err := ca.WriteKeyPEM(caKeyPath); err != nil {
			return err
		}
	}

	leaf := leafFor(selected, *hosts)
	if _, _, err := ca.WriteLeaf(leaf, serverCertPath, serverKeyPath); err != nil {
		return err
	}

	absCA, err := filepath.Abs(caCertPath)
	if err != nil {
		absCA = caCertPath
	}
	if reused {
		fmt.Fprintf(stdout, "reused CA     %s\n", caCertPath)
	} else {
		fmt.Fprintf(stdout, "wrote CA      %s\n", caCertPath)
	}
	fmt.Fprintf(stdout, "wrote server  %s (mode=%s, hosts=%s)\n", serverCertPath, selected, strings.Join(leaf.Hosts, ","))
	if selected != modeValid {
		fmt.Fprintf(stdout, "\nthis certificate is deliberately broken (mode=%s); connections are expected to fail\n", selected)
	}
	fmt.Fprintf(stdout, `
Next:
  task jenkins:tls          start Jenkins behind the HTTPS proxy on :8443
  task seed:tls             seed it over HTTPS

Point JenkTui at it with ca_cert_path:
  "jenkins_url":  "https://localhost:8443",
  "ca_cert_path": "%s"
`, absCA)
	return nil
}

// authority loads the existing CA when asked to reuse it, so a ca_cert_path
// already saved in a server configuration keeps working.
func authority(certPath, keyPath string, reuse, force bool) (*localca.Authority, bool, error) {
	if reuse {
		ca, err := localca.Load(certPath, keyPath)
		if err != nil {
			return nil, false, fmt.Errorf("-reuse-ca needs an existing CA in place: %w", err)
		}
		return ca, true, nil
	}
	if !force {
		if _, err := os.Stat(certPath); err == nil {
			return nil, false, fmt.Errorf("%s already exists; pass -force to replace the CA (every ca_cert_path pointing at it must be re-copied) or -reuse-ca to keep it", certPath)
		}
	}
	ca, err := localca.New(localca.Options{CommonName: "JenkTui Development CA"})
	if err != nil {
		return nil, false, err
	}
	return ca, false, nil
}

func leafFor(selected mode, hosts string) localca.LeafOptions {
	opts := localca.LeafOptions{Hosts: splitHosts(hosts)}
	switch selected {
	case modeExpired:
		opts.NotBefore = time.Now().Add(-48 * time.Hour)
		opts.NotAfter = time.Now().Add(-24 * time.Hour)
	case modeWrongHost:
		opts.Hosts = []string{"jenkins.internal"}
	case modeValid:
	}
	return opts
}

func splitHosts(raw string) []string {
	var hosts []string
	for _, host := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(host); trimmed != "" {
			hosts = append(hosts, trimmed)
		}
	}
	return hosts
}
