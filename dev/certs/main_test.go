package main

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atm1020/jenktui/dev/localca"
)

func generate(t *testing.T, args ...string) (dir string, stdout string) {
	t.Helper()
	dir = t.TempDir()
	var out bytes.Buffer
	if err := run(append([]string{"-dir", dir}, args...), &out); err != nil {
		t.Fatalf("run(%v): %v\n%s", args, err, out.String())
	}
	return dir, out.String()
}

func leafFrom(t *testing.T, dir string) *x509.Certificate {
	t.Helper()
	certPEM, err := os.ReadFile(filepath.Join(dir, "server.pem"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("server certificate is not valid PEM")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return leaf
}

func TestGeneratesAChainTheCAVerifies(t *testing.T) {
	dir, out := generate(t)

	for _, name := range []string{"ca.pem", "ca-key.pem", "server.pem", "server-key.pem"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	if !strings.Contains(out, "ca_cert_path") {
		t.Errorf("output does not show the ca_cert_path to use:\n%s", out)
	}

	ca, err := localca.Load(filepath.Join(dir, "ca.pem"), filepath.Join(dir, "ca-key.pem"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca.CertPEM()) {
		t.Fatal("generated CA is not valid PEM")
	}
	if _, err := leafFrom(t, dir).Verify(x509.VerifyOptions{Roots: pool, DNSName: "localhost"}); err != nil {
		t.Errorf("generated leaf does not verify against the generated CA: %v", err)
	}
}

func TestPrivateKeysAreOwnerOnly(t *testing.T) {
	dir, _ := generate(t)
	for _, name := range []string{"ca-key.pem", "server-key.pem"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("%s mode = %o, want 600", name, mode)
		}
	}
}

func TestHostsFlagControlsTheSANs(t *testing.T) {
	dir, _ := generate(t, "-hosts", "jenkins.corp,10.1.2.3")
	leaf := leafFrom(t, dir)

	if err := leaf.VerifyHostname("jenkins.corp"); err != nil {
		t.Errorf("leaf does not cover jenkins.corp: %v", err)
	}
	if err := leaf.VerifyHostname("10.1.2.3"); err != nil {
		t.Errorf("leaf does not cover 10.1.2.3: %v", err)
	}
	if err := leaf.VerifyHostname("localhost"); err == nil {
		t.Error("leaf covers localhost, which -hosts did not name")
	}
}

func TestBrokenModesProduceTheIntendedDefect(t *testing.T) {
	expiredDir, out := generate(t, "-mode", "expired")
	if !strings.Contains(out, "deliberately broken") {
		t.Errorf("expired mode did not warn that it is broken:\n%s", out)
	}
	if leaf := leafFrom(t, expiredDir); !leaf.NotAfter.Before(time.Now()) {
		t.Errorf("expired mode produced NotAfter = %s, want a past time", leaf.NotAfter)
	}

	wrongHostDir, _ := generate(t, "-mode", "wrong-host")
	leaf := leafFrom(t, wrongHostDir)
	if err := leaf.VerifyHostname("localhost"); err == nil {
		t.Error("wrong-host mode produced a certificate that still covers localhost")
	}
	if leaf.NotAfter.Before(time.Now()) {
		t.Error("wrong-host mode must fail on the hostname, not on expiry")
	}
}

func TestRejectsUnknownMode(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"-dir", t.TempDir(), "-mode", "nonsense"}, &out); err == nil {
		t.Error("an unknown -mode was accepted")
	}
}

func TestExistingCAIsNotReplacedWithoutForce(t *testing.T) {
	dir, _ := generate(t)
	before, err := os.ReadFile(filepath.Join(dir, "ca.pem"))
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"-dir", dir}, &out); err == nil {
		t.Fatal("a second run silently replaced the CA")
	}

	if err := run([]string{"-dir", dir, "-force"}, &out); err != nil {
		t.Fatalf("-force failed: %v", err)
	}
	forced, err := os.ReadFile(filepath.Join(dir, "ca.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before, forced) {
		t.Error("-force did not replace the CA")
	}
}

func TestReuseCAKeepsTheAuthority(t *testing.T) {
	dir, _ := generate(t)
	caBefore, err := os.ReadFile(filepath.Join(dir, "ca.pem"))
	if err != nil {
		t.Fatal(err)
	}
	serverBefore, err := os.ReadFile(filepath.Join(dir, "server.pem"))
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run([]string{"-dir", dir, "-reuse-ca"}, &out); err != nil {
		t.Fatalf("-reuse-ca failed: %v", err)
	}

	caAfter, _ := os.ReadFile(filepath.Join(dir, "ca.pem"))
	serverAfter, _ := os.ReadFile(filepath.Join(dir, "server.pem"))
	if !bytes.Equal(caBefore, caAfter) {
		t.Error("-reuse-ca replaced the CA; a saved ca_cert_path would break")
	}
	if bytes.Equal(serverBefore, serverAfter) {
		t.Error("-reuse-ca did not reissue the server certificate")
	}
}

func TestReuseCAWithoutAnExistingCAFails(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"-dir", t.TempDir(), "-reuse-ca"}, &out); err == nil {
		t.Error("-reuse-ca succeeded with no CA in place")
	}
}
