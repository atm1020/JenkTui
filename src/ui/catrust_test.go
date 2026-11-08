package ui

import (
	stdcontext "context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/atm1020/gojenkins"
	"github.com/atm1020/jenktui/dev/localca"
	"github.com/atm1020/jenktui/src/context"
)

// privateCAJenkins serves a Jenkins stand-in over TLS behind a CA that no
// system trust store knows, and writes that CA where ca_cert_path can point.
func privateCAJenkins(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	ca, err := localca.New(localca.Options{})
	if err != nil {
		t.Fatalf("localca.New: %v", err)
	}
	server, err := ca.TLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"mode":"NORMAL"}`))
	}), localca.LeafOptions{})
	if err != nil {
		t.Fatalf("TLSServer: %v", err)
	}
	t.Cleanup(server.Close)

	caPath, err := ca.WriteCertPEM(filepath.Join(t.TempDir(), "company-ca.pem"))
	if err != nil {
		t.Fatalf("WriteCertPEM: %v", err)
	}
	return server, caPath
}

func testCAPEM(t *testing.T) []byte {
	t.Helper()
	ca, err := localca.New(localca.Options{})
	if err != nil {
		t.Fatalf("localca.New: %v", err)
	}
	return ca.CertPEM()
}

// End-to-end: a configured CA must let real Jenkins traffic through, not merely
// be stored somewhere. GET and POST are both covered because writes run a crumb
// pre-flight that fails separately from the request itself.
func TestConfiguredCAReachesJenkinsOverTLS(t *testing.T) {
	server, caPath := privateCAJenkins(t)
	jenkins, _, err := newJenkinsClient(&context.ServerConfig{
		JenkinsURL: server.URL, Username: "user", APIToken: "token", CACertPath: caPath,
	}, t.TempDir())
	if err != nil {
		t.Fatalf("newJenkinsClient() error = %v", err)
	}
	requester := jenkins.Requester.(*gojenkins.Requester)

	var payload map[string]any
	response, err := requester.GetJSON(stdcontext.Background(), "/api/json", &payload, nil)
	if response != nil {
		defer response.Body.Close()
	}
	if err != nil {
		t.Errorf("GET with a configured CA failed: %v", err)
	}
	response, err = requester.Post(stdcontext.Background(), "/job/demo/build", nil, nil, nil)
	if response != nil {
		defer response.Body.Close()
	}
	if err != nil {
		t.Errorf("POST with a configured CA failed: %v", err)
	}
}

// Without the CA the same server must be refused, so the test above proves
// trust was added rather than verification being skipped.
func TestUnconfiguredCAIsRefusedWithoutPanicking(t *testing.T) {
	server, _ := privateCAJenkins(t)
	jenkins, _, err := newJenkinsClient(&context.ServerConfig{
		JenkinsURL: server.URL, Username: "user", APIToken: "token",
	}, t.TempDir())
	if err != nil {
		t.Fatalf("newJenkinsClient() error = %v", err)
	}
	requester := jenkins.Requester.(*gojenkins.Requester)

	var payload map[string]any
	response, err := requester.GetJSON(stdcontext.Background(), "/api/json", &payload, nil)
	if response != nil {
		defer response.Body.Close()
	}
	if err == nil {
		t.Error("GET succeeded against an untrusted certificate")
	}

	// Regression: the crumb pre-flight used to discard the TLS error and panic.
	response, err = requester.Post(stdcontext.Background(), "/job/demo/build", nil, nil, nil)
	if response != nil {
		defer response.Body.Close()
	}
	if err == nil {
		t.Error("POST succeeded against an untrusted certificate")
	}
}
