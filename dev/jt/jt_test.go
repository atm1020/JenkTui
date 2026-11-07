package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runJT(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(context.Background(), args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func isolate(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("JT_STATE_ROOT", filepath.Join(root, "state"))
	t.Setenv("JT_EVIDENCE_ROOT", filepath.Join(root, "evidence"))
	t.Setenv("JT_JENKINS_URL", "")
	t.Setenv("JT_CONFIG_JSON", "")
	t.Setenv("JT_COLS", "")
	return root
}

func TestUsageErrors(t *testing.T) {
	isolate(t)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown command", []string{"bogus"}, `unknown command "bogus"`},
		{"missing RUN", []string{"keys"}, "keys needs a RUN id"},
		{"bad RUN", []string{"up", "../x"}, "must match"},
		{"too few args", []string{"wait", "demo"}, "wait got 0 argument(s)"},
		{"too many args", []string{"snap", "demo", "a", "b"}, "snap got 2 argument(s)"},
	}
	for _, tt := range tests {
		code, _, stderr := runJT(t, tt.args...)
		if code != 2 || !strings.Contains(stderr, tt.want) {
			t.Errorf("%s: code=%d stderr=%q, want 2 and %q", tt.name, code, stderr, tt.want)
		}
		// Every usage error carries a runnable example.
		if !strings.Contains(stderr, "\n  jt ") {
			t.Errorf("%s: no example invocation in %q", tt.name, stderr)
		}
	}
}

func TestHelp(t *testing.T) {
	isolate(t)
	if code, out, _ := runJT(t); code != 0 || !strings.Contains(out, "jt up      RUN") {
		t.Errorf("no args: code=%d", code)
	}
	if code, out, _ := runJT(t, "wait", "--help"); code != 0 || !strings.HasPrefix(out, "jt wait RUN REGEX") {
		t.Errorf("wait --help: code=%d out=%q", code, out)
	}
	if code, out, _ := runJT(t, "db", "demo", "--help"); code != 0 || !strings.HasPrefix(out, "jt db RUN SQL") {
		t.Errorf("trailing --help: code=%d out=%q", code, out)
	}
}

func TestEveryHelpHasExamples(t *testing.T) {
	helps := map[string]string{"list": helpList}
	for name, cmd := range commands {
		helps[name] = cmd.help
	}
	for name, help := range helps {
		if !strings.Contains(help, "\nExamples:\n  ") || !strings.HasPrefix(exampleOf(help), "jt "+name) {
			t.Errorf("%s --help: first example %q does not invoke it", name, exampleOf(help))
		}
	}
}

func TestLoadConfigGuards(t *testing.T) {
	isolate(t)
	t.Setenv("JT_JENKINS_URL", "https://jenkins.example.com")
	if _, err := loadConfig(context.Background()); err == nil || !strings.Contains(err.Error(), "not local") {
		t.Errorf("remote Jenkins accepted: %v", err)
	}
	t.Setenv("JT_JENKINS_URL", "")
	t.Setenv("JT_CONFIG_JSON", "{nope")
	if code, _, stderr := runJT(t, "doctor", "demo"); code != 2 || !strings.Contains(stderr, "not valid JSON") {
		t.Errorf("bad JT_CONFIG_JSON: code=%d %q", code, stderr)
	}
	t.Setenv("JT_CONFIG_JSON", "")
	t.Setenv("JT_COLS", "wide")
	if _, err := loadConfig(context.Background()); err == nil {
		t.Error("JT_COLS=wide accepted")
	}
}

// https is accepted so the TLS dev stack (task jenkins:tls) can be driven; the
// guard is about the host being loopback, not about the scheme.
func TestIsLocalURL(t *testing.T) {
	for url, want := range map[string]bool{
		"http://127.0.0.1:8080":       true,
		"http://localhost:8081":       true,
		"https://127.0.0.1:8443":      true,
		"https://localhost:8443":      true,
		"http://127.0.0.1.evil:8080":  false,
		"https://127.0.0.1.evil:8443": false,
		"http://10.0.0.5:8080":        false,
		"https://jenkins.corp:443":    false,
	} {
		if got := isLocalURL(url); got != want {
			t.Errorf("isLocalURL(%q) = %v, want %v", url, got, want)
		}
	}
}

// A configured CA has to land in server.json as ca_cert_path, and stay absent
// when none is set, or the app falls back to system roots.
func TestServerJSONCarriesCACertPath(t *testing.T) {
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	in := newInstance(config{
		jenkinsURL: "https://127.0.0.1:8443", user: "u", token: "t",
		caCertPath: caPath, secondServer: true,
	}, "x", nil, nil)

	raw, err := in.serverJSON()
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Servers []struct {
			CACertPath string `json:"ca_cert_path"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Servers) != 2 {
		t.Fatalf("got %d servers, want 2", len(parsed.Servers))
	}
	for i, server := range parsed.Servers {
		if server.CACertPath != caPath {
			t.Errorf("server %d ca_cert_path = %q, want %q", i, server.CACertPath, caPath)
		}
	}

	withoutCA := newInstance(config{jenkinsURL: "http://127.0.0.1:8080", user: "u", token: "t"}, "x", nil, nil)
	raw, err = withoutCA.serverJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "ca_cert_path") {
		t.Errorf("server.json carries ca_cert_path with none configured: %s", raw)
	}
}

func TestLoadConfigRejectsUnreadableCACert(t *testing.T) {
	t.Setenv("JT_JENKINS_URL", "https://127.0.0.1:8443")
	t.Setenv("JT_CA_CERT", filepath.Join(t.TempDir(), "absent.pem"))
	if _, err := loadConfig(context.Background()); err == nil {
		t.Error("an unreadable JT_CA_CERT was accepted")
	}
}

func TestServerJSON(t *testing.T) {
	in := newInstance(config{jenkinsURL: "http://127.0.0.1:8080", user: "u", token: "t", secondServer: true}, "x", nil, nil)
	raw, err := in.serverJSON()
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Servers []serverEntry }
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Servers) != 2 || got.Servers[0].ID != "local" || got.Servers[1].ID != "local-b" || got.Servers[1].APIToken != "t" {
		t.Errorf("servers = %+v", got.Servers)
	}
}

func TestFooterRow(t *testing.T) {
	screen := "Health Score\n\n   admin  DevTeam  Frontend  mobile-app   Added to favorites  \n\n   \n"
	if got := footerRow(screen); !strings.Contains(got, "mobile-app") {
		t.Errorf("footerRow = %q", got)
	}
	if got := footerRow("\n \n"); got != "" {
		t.Errorf("blank screen footer = %q", got)
	}
}

func TestQueryTableAndBackup(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "runs.db")
	db, err := sql.Open("sqlite", src)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `create table favorites(server_id text, name text, note text);
		insert into favorites values ('local', 'mobile-app', null), ('local', 'two
lines', 'x')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	ro, err := openReadOnly(src)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	var out bytes.Buffer
	if err := queryTable(ctx, ro, "select * from favorites", &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "server_id") || !strings.HasPrefix(lines[1], "---") ||
		!strings.Contains(lines[3], "two lines") {
		t.Errorf("table:\n%s", out.String())
	}
	if _, err := ro.ExecContext(ctx, "delete from favorites"); err == nil {
		t.Error("read-only connection allowed a write")
	}

	dst := filepath.Join(dir, "evidence.db")
	for range 2 { // repeatable: down may run twice
		if err := backupDB(ctx, src, dst); err != nil {
			t.Fatal(err)
		}
	}
	copyDB, _ := openReadOnly(dst)
	defer copyDB.Close()
	var n int
	if err := copyDB.QueryRowContext(ctx, "select count(*) from favorites").Scan(&n); err != nil || n != 2 {
		t.Errorf("backup rows = %d, %v", n, err)
	}
}

func TestLogTailAndNote(t *testing.T) {
	isolate(t)
	cfg, err := loadConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	in := newInstance(cfg, "demo", &out, &out)
	if err := os.MkdirAll(filepath.Dir(in.logPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(in.logPath(), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := in.log(context.Background(), []string{"2"}); err != nil || out.String() != "b\nc\n" {
		t.Errorf("log 2 = %q, %v", out.String(), err)
	}
	if err := in.note(context.Background(), []string{"hello", "world"}); err != nil {
		t.Fatal(err)
	}
	transcript, _ := os.ReadFile(filepath.Join(in.evid, "transcript.log"))
	if !strings.HasSuffix(string(transcript), " note: hello world\n") {
		t.Errorf("transcript = %q", transcript)
	}

	var list bytes.Buffer
	if err := listRuns(cfg, &list); err != nil || !strings.HasPrefix(list.String(), "demo\t") {
		t.Errorf("list = %q, %v", list.String(), err)
	}
}

func TestDriveCommandsNeedARunningInstance(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	isolate(t)
	code, _, stderr := runJT(t, "keys", "nosuchrun-jt-test", "Enter")
	if code != 2 || !strings.Contains(stderr, "jt up nosuchrun-jt-test") {
		t.Errorf("code=%d stderr=%q", code, stderr)
	}
}

func TestDownWithoutInstanceIsSafe(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	root := isolate(t)
	for range 2 {
		code, out, stderr := runJT(t, "down", "never-started")
		if code != 0 || !strings.HasPrefix(out, "stopped RUN=never-started") {
			t.Fatalf("code=%d out=%q stderr=%q", code, out, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "evidence", "never-started", "transcript.log")); err != nil {
		t.Error("down should leave a transcript")
	}
}
