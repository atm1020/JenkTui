package serversetup

import (
	"bytes"
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/log/v2"
	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"
)

func jenkinsHandler(t *testing.T, token string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Error("connection test must be read-only")
		}
		user, password, ok := r.BasicAuth()
		if !ok || user != "user" || password != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/jenkins/whoAmI/api/json":
			_, _ = w.Write([]byte(`{"authenticated":true}`))
		case "/jenkins/api/json":
			_, _ = w.Write([]byte(`{"mode":"NORMAL"}`))
		default:
			t.Errorf("unexpected request path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestVerifySaveAndEditPreservesStoredTokenAndIdentity(t *testing.T) {
	const token = "test-only-secret"
	var logs bytes.Buffer
	previous := log.Default()
	log.SetDefault(log.New(&logs))
	t.Cleanup(func() { log.SetDefault(previous) })
	t.Cleanup(func() {
		if strings.Contains(logs.String(), token) || strings.Contains(logs.String(), "other-secret") {
			t.Error("setup logged a credential")
		}
	})
	srv := httptest.NewServer(jenkinsHandler(t, token))
	defer srv.Close()
	dir := t.TempDir()
	draft := Draft{ID: "stable-id", Name: "Local", URL: srv.URL + "/jenkins/", Username: "user", ReplacementToken: token}
	verified, err := Verify(context.Background(), dir, draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "server.json")); !os.IsNotExist(err) {
		t.Fatal("test must not save")
	}
	if err := verified.Save(dir); err != nil {
		t.Fatal(err)
	}
	servers, err := config.LoadServerConfigs(dir)
	if err != nil {
		t.Fatal(err)
	}
	servers = append(servers, appcontext.ServerConfig{ID: "other", Name: "Other", JenkinsURL: srv.URL, Username: "user", APIToken: "other-secret"})
	if err := config.SaveServerConfigs(dir, servers); err != nil {
		t.Fatal(err)
	}
	draft.Name, draft.ReplacementToken = "Renamed", ""
	verified, err = Verify(context.Background(), dir, draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := verified.Save(dir); err != nil {
		t.Fatal(err)
	}
	servers, err = config.LoadServerConfigs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 || servers[0].ID != "stable-id" || servers[0].Name != "Renamed" || servers[0].APIToken != token || servers[1].APIToken != "other-secret" {
		t.Fatal("edit did not preserve identity and credentials")
	}
	info, err := os.Stat(filepath.Join(dir, "server.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatal("server configuration must be private")
	}
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastServerID != "stable-id" {
		t.Fatal("last server not remembered")
	}
}

func TestVerifyRejectsFailuresWithoutEchoingSecrets(t *testing.T) {
	const secret = "do-not-echo-this-token"
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"auth", secret, "authentication failed", 401},
		{"access", secret, "access denied", 403},
		{"server", secret, "HTTP 500", 500},
		{"html", secret, "valid Jenkins response", 200},
		{"anonymous", `{"authenticated":false}`, "anonymous", 200},
		{"redirect", secret, "HTTP 302", 302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := Verify(context.Background(), t.TempDir(), Draft{ID: "id", Name: "name", URL: srv.URL, Username: "user", ReplacementToken: secret})
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), secret) {
				t.Fatalf("expected sanitized %s failure", tc.name)
			}
		})
	}
}

func TestVerifyTLSAndCustomCA(t *testing.T) {
	srv := httptest.NewTLSServer(jenkinsHandler(t, "secret"))
	defer srv.Close()
	dir := t.TempDir()
	draft := Draft{ID: "id", Name: "TLS", URL: srv.URL + "/jenkins", Username: "user", ReplacementToken: "secret"}
	if _, err := Verify(context.Background(), dir, draft); err == nil || !strings.Contains(err.Error(), "TLS certificate") {
		t.Fatal("untrusted TLS must fail")
	}
	path := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	draft.CAPath = path
	if _, err := Verify(context.Background(), dir, draft); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyCancellationAndDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	for _, canceled := range []bool{true, false} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		if canceled {
			cancel()
		}
		_, err := Verify(ctx, t.TempDir(), Draft{ID: "id", Name: "name", URL: srv.URL, Username: "user", ReplacementToken: "secret"})
		cancel()
		if err == nil {
			t.Fatal("canceled or timed-out request succeeded")
		}
	}
}

func TestEditCanReplaceToken(t *testing.T) {
	dir := t.TempDir()
	server := appcontext.ServerConfig{ID: "stable", Name: "Local", JenkinsURL: "http://localhost", Username: "user", APIToken: "old-token"}
	if err := config.SaveServerConfigs(dir, []appcontext.ServerConfig{server}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(jenkinsHandler(t, "new-token"))
	defer srv.Close()
	verified, err := Verify(context.Background(), dir, Draft{ID: server.ID, Name: server.Name, URL: srv.URL + "/jenkins", Username: server.Username, ReplacementToken: "new-token"})
	if err != nil {
		t.Fatal(err)
	}
	if err := verified.Save(dir); err != nil {
		t.Fatal(err)
	}
	stored, err := config.LoadServerConfigs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].ID != server.ID || stored[0].APIToken != "new-token" {
		t.Fatal("replacement did not update the existing credential")
	}
}

func TestSaveFailureDoesNotTruncateExistingConfig(t *testing.T) {
	dir := t.TempDir()
	// A directory at the destination forces atomic rename to fail, even as root.
	path := filepath.Join(dir, "server.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveServerConfigs(dir, []appcontext.ServerConfig{{APIToken: "secret"}}); err == nil {
		t.Fatal("expected save failure")
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatal("failed save replaced existing destination")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatal("failed save left a credential temporary file")
	}
}

func TestVerifyRejectsCredentialURLs(t *testing.T) {
	_, err := Verify(context.Background(), t.TempDir(), Draft{ID: "id", Name: "name", URL: "http://user:secret@example.com", Username: "user", ReplacementToken: "secret"})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("credential URL accepted or echoed")
	}
}
