// Package serversetup validates Jenkins connections and persists setup changes.
// Stored credentials stay behind this boundary, never in the editing form.
package serversetup

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jenkinsclient"
)

// Draft contains public connection settings and only a newly entered token.
// An empty ReplacementToken preserves the stored token when editing.
type Draft struct {
	ID, Name, URL, Username, CAPath string
	ReplacementToken                string
}

// Verified owns the resolved credential. It must not be logged or exposed to a form.
type Verified struct{ server appcontext.ServerConfig }

// Verify performs only bounded, read-only requests; it never saves or starts builds.
// Returned errors deliberately exclude server responses, URLs and credentials.
func Verify(ctx context.Context, dir string, draft Draft) (*Verified, error) {
	servers, err := config.LoadServerConfigs(dir)
	if err != nil {
		return nil, errors.New("cannot read server configuration; check server.json")
	}
	server := appcontext.ServerConfig{ID: draft.ID, Name: strings.TrimSpace(draft.Name), JenkinsURL: strings.TrimRight(strings.TrimSpace(draft.URL), "/"), Username: strings.TrimSpace(draft.Username), APIToken: draft.ReplacementToken, CACertPath: strings.TrimSpace(draft.CAPath)}
	if server.APIToken == "" {
		for _, stored := range servers {
			if stored.ID == server.ID {
				server.APIToken = stored.APIToken
				break
			}
		}
	}
	if server.ID == "" || server.Name == "" {
		return nil, errors.New("enter a server name")
	}
	if err := config.ValidateServerConfig(&server); err != nil {
		return nil, err
	}
	client := jenkinsclient.New(jenkinsclient.Options{})
	defer jenkinsclient.CloseIdle(client)
	// Do not follow redirects: they may send credentials to an unintended server.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if server.CACertPath != "" {
		pem, err := os.ReadFile(server.CACertPath)
		if err != nil {
			return nil, errors.New("cannot read CA certificate; check the advanced setting")
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("CA certificate is not valid PEM")
		}
		client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// whoAmI prevents anonymous-readable Jenkins instances from accepting bad credentials.
	if err := check(ctx, client, server, "/whoAmI/api/json", true); err != nil {
		return nil, err
	}
	if err := check(ctx, client, server, "/api/json", false); err != nil {
		return nil, err
	}
	return &Verified{server: server}, nil
}

func check(ctx context.Context, client *http.Client, server appcontext.ServerConfig, path string, identity bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.JenkinsURL+path, nil)
	if err != nil {
		return errors.New("invalid Jenkins URL")
	}
	req.SetBasicAuth(server.Username, server.APIToken)
	resp, err := client.Do(req)
	if err != nil {
		var certErr *tls.CertificateVerificationError
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			return errors.New("connection test canceled")
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return errors.New("connection test timed out after five seconds")
		case errors.As(err, &certErr):
			return errors.New("TLS certificate verification failed; check the CA certificate")
		default:
			return errors.New("cannot connect to Jenkins; check the URL and network")
		}
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return errors.New("authentication failed; check username and API token")
	case http.StatusForbidden:
		return errors.New("access denied; check credentials and Jenkins read permissions")
	case http.StatusOK:
	default:
		return fmt.Errorf("server returned HTTP %d; check the Jenkins URL and access permissions", resp.StatusCode)
	}
	var result struct {
		Authenticated bool   `json:"authenticated"`
		Mode          string `json:"mode"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return errors.New("the URL did not return a valid Jenkins response")
	}
	if identity && !result.Authenticated {
		return errors.New("authentication failed; Jenkins reported anonymous access")
	}
	if !identity && result.Mode == "" {
		return errors.New("the URL did not return a Jenkins server response")
	}
	return nil
}

// Save updates by stable ID, preserving other connections and their credentials.
// Call only after accepting a Verify result on the UI loop. Saving is not cancelable.
func (v *Verified) Save(dir string) error {
	servers, err := config.LoadServerConfigs(dir)
	if err != nil {
		return errors.New("cannot read server configuration; nothing was saved")
	}
	found := false
	for i := range servers {
		if servers[i].ID == v.server.ID {
			servers[i] = v.server
			found = true
			break
		}
	}
	if !found {
		servers = append(servers, v.server)
	}
	if err := config.SaveServerConfigs(dir, servers); err != nil {
		return errors.New("cannot save server configuration; check directory permissions and retry")
	}
	cfg, err := config.LoadConfig(dir)
	if err != nil {
		return errors.New("server saved, but application settings could not be read; retry to remember the server")
	}
	cfg.LastServerID = v.server.ID
	if err := config.SaveConfig(dir, cfg); err != nil {
		return errors.New("server saved, but last-used server could not be saved; check permissions and retry")
	}
	return nil
}
