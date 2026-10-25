package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// instance is one RUN: an isolated HOME, a binary, a private tmux server, and
// an evidence folder that outlives it.
type instance struct {
	cfg    config
	run    string
	state  string
	home   string
	bin    string
	evid   string
	data   string
	sock   string
	stdout io.Writer
	stderr io.Writer
}

func newInstance(cfg config, run string, stdout, stderr io.Writer) *instance {
	state := filepath.Join(cfg.stateRoot, run)
	home := filepath.Join(state, "home")
	return &instance{
		cfg:    cfg,
		run:    run,
		state:  state,
		home:   home,
		bin:    filepath.Join(state, "jenktui"),
		evid:   filepath.Join(cfg.evidenceRoot, run),
		data:   filepath.Join(home, ".local", "share", "JenkTui"),
		sock:   "jtv-" + run,
		stdout: stdout,
		stderr: stderr,
	}
}

func (in *instance) logPath() string { return filepath.Join(in.data, "logs", "jenktui.log") }
func (in *instance) dbPath() string  { return filepath.Join(in.data, "runs.db") }

func (in *instance) tmux(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "tmux", append([]string{"-L", in.sock}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

func (in *instance) alive(ctx context.Context) bool {
	_, err := in.tmux(ctx, "has-session", "-t", "jt")
	return err == nil
}

func (in *instance) needAlive(ctx context.Context) error {
	if !in.alive(ctx) {
		return usageErr(fmt.Sprintf("no instance for RUN=%s", in.run), "jt up "+in.run)
	}
	return nil
}

func (in *instance) paneValue(ctx context.Context, format string) string {
	out, err := in.tmux(ctx, "display", "-p", "-t", "jt", format)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func (in *instance) paneDead(ctx context.Context) bool {
	return in.paneValue(ctx, "#{pane_dead}") != "0"
}

func (in *instance) plainScreen(ctx context.Context) (string, error) {
	return in.tmux(ctx, "capture-pane", "-p", "-t", "jt")
}

func (in *instance) dumpScreen(ctx context.Context) {
	if s, err := in.plainScreen(ctx); err == nil {
		fmt.Fprintf(in.stderr, "--- screen ---\n%s", s)
	}
}

// transcript timestamps every action into the evidence folder.
func (in *instance) transcriptf(format string, args ...any) {
	if err := os.MkdirAll(in.evid, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(in.evid, "transcript.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
}

// jenkinsGet performs an authenticated GET against the local Jenkins.
func (in *instance) jenkinsGet(ctx context.Context, path string) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, in.cfg.jenkinsURL+path, nil)
	if err != nil {
		return 0, nil, err
	}
	req.SetBasicAuth(in.cfg.user, in.cfg.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, body, err
}

func (in *instance) jenkinsStatus(ctx context.Context, path string) int {
	code, _, _ := in.jenkinsGet(ctx, path)
	return code
}

type serverEntry struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	JenkinsURL string `json:"jenkins_url"`
	Username   string `json:"username"`
	APIToken   string `json:"api_token"`
}

func (in *instance) serverJSON() ([]byte, error) {
	servers := []serverEntry{{"local", "Local", in.cfg.jenkinsURL, in.cfg.user, in.cfg.token}}
	if in.cfg.secondServer {
		servers = append(servers, serverEntry{"local-b", "Local B", in.cfg.jenkinsURL, in.cfg.user, in.cfg.token})
	}
	return json.Marshal(map[string]any{"servers": servers})
}

const tmuxConfig = "set -g remain-on-exit on\nset -g history-limit 10000\nset -g default-terminal tmux-256color\n"

// readyPattern: the home splash shows the Jenkins version only after an
// authenticated round-trip; with several servers and no last_server_id the
// app opens the startup server picker instead.
var readyPattern = regexp.MustCompile(`Jenkins +[0-9]+\.[0-9]+|Select Jenkins server`)

func (in *instance) up(ctx context.Context, _ []string) error {
	if in.alive(ctx) {
		return usageErr(fmt.Sprintf("RUN=%s already running", in.run), "jt down "+in.run, "or pick another RUN id")
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return errors.New("tmux not installed")
	}
	if code := in.jenkinsStatus(ctx, "/api/json"); code != http.StatusOK {
		return fmt.Errorf("jenkins at %s answered HTTP %d (start it: task jenkins:lts; seed: task seed)", in.cfg.jenkinsURL, code)
	}

	cfgDir := filepath.Join(in.home, ".config", "JenkTui")
	for _, dir := range []string{cfgDir, in.evid} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	for _, dir := range []string{in.state, in.home} {
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}

	build := exec.CommandContext(ctx, "go", "build", "-o", in.bin, ".")
	build.Dir, build.Env = in.cfg.repo, append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("go build failed: %w\n%s", err, out)
	}

	servers, err := in.serverJSON()
	if err != nil {
		return err
	}
	tmuxConf := filepath.Join(in.state, "tmux.conf")
	files := map[string][]byte{
		filepath.Join(cfgDir, "server.json"): servers,
		filepath.Join(cfgDir, "config.json"): []byte(in.cfg.configJSON + "\n"),
		tmuxConf:                             []byte(tmuxConfig),
	}
	for path, content := range files {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			return err
		}
	}

	// tmux runs the argv directly (no shell), so paths need no quoting.
	appCmd := []string{"env", "HOME=" + in.home, "TERM=xterm-256color", "COLORTERM=truecolor"}
	if in.cfg.development {
		appCmd = append(appCmd, "DEVELOPMENT=1")
	}
	appCmd = append(appCmd, in.bin)
	args := []string{
		"-f", tmuxConf, "new-session", "-d", "-s", "jt",
		"-x", fmt.Sprint(in.cfg.cols), "-y", fmt.Sprint(in.cfg.rows),
	}
	if _, err := in.tmux(ctx, append(args, appCmd...)...); err != nil {
		return err
	}
	// Pin the pane at JT_COLS x JT_ROWS so a human attaching to watch cannot
	// resize the app the agent is reading. (tmux 3.4 crashes when this is set
	// in the config file, so set it on the live server.)
	if _, err := in.tmux(ctx, "set", "-g", "window-size", "manual"); err != nil {
		return err
	}
	if describe, err := exec.CommandContext(ctx, "git", "-C", in.cfg.repo, "describe", "--always", "--dirty").Output(); err == nil {
		_ = os.WriteFile(filepath.Join(in.evid, "build.txt"), describe, 0o644)
	}
	in.transcriptf("up: %s (HOME=%s, %dx%d, jenkins=%s)", in.bin, in.home, in.cfg.cols, in.cfg.rows, in.cfg.jenkinsURL)

	if err := in.waitFor(ctx, readyPattern, 30*time.Second, false); err != nil {
		return fmt.Errorf("app did not reach the home view (%w); see: jt log %s; then: jt down %s", err, in.run, in.run)
	}
	state := "home"
	if s, _ := in.plainScreen(ctx); strings.Contains(s, "Select Jenkins server") {
		state = "server-picker"
	}
	in.transcriptf("ready: %s", state)
	fmt.Fprintf(in.stdout, "ready RUN=%s state=%s  evidence=%s\n", in.run, state, in.evid)
	return nil
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func (in *instance) doctor(ctx context.Context, _ []string) error {
	failed := false
	check := func(ok bool, desc string) {
		if ok {
			fmt.Fprintf(in.stdout, "ok   %s\n", desc)
		} else {
			fmt.Fprintf(in.stdout, "FAIL %s\n", desc)
			failed = true
		}
	}
	info := func(format string, args ...any) { fmt.Fprintf(in.stdout, "info "+format+"\n", args...) }

	alive := in.alive(ctx)
	check(alive, "tmux server "+in.sock+" has session")
	if alive {
		dead := in.paneValue(ctx, "#{pane_dead}")
		check(dead == "0", fmt.Sprintf("app process still running (pane_dead=%s)", dead))
		if runtime.GOOS == "linux" {
			exe, _ := os.Readlink("/proc/" + in.paneValue(ctx, "#{pane_pid}") + "/exe")
			check(exe == in.bin, fmt.Sprintf("pane runs our binary (%s)", exe))
		} else {
			info("pane binary check skipped (needs /proc)")
		}
	}
	st, err := os.Stat(filepath.Join(in.home, ".config", "JenkTui"))
	check(err == nil && st.IsDir(), fmt.Sprintf("isolated HOME exists (%s)", in.home))
	logData, err := os.ReadFile(in.logPath())
	check(err == nil && len(logData) > 0, "app log written under isolated HOME")
	check(!regexp.MustCompile(`panic|fatal error`).Match(logData), "no panic in jenktui.log")
	if len(logData) > 0 {
		// The router logs every bounded-history eviction at ERRO.
		info("%d ERRO lines in jenktui.log (router evictions are normal; read others with: jt log %s 200)",
			bytes.Count(logData, []byte("ERRO")), in.run)
	}
	code := in.jenkinsStatus(ctx, "/api/json")
	check(code == http.StatusOK, fmt.Sprintf("Jenkins %s auth OK (HTTP %d)", in.cfg.jenkinsURL, code))
	seeded := in.jenkinsStatus(ctx, "/job/DevTeam/job/Frontend/job/mobile-app/api/json")
	check(seeded == http.StatusOK, fmt.Sprintf("seed data present (mobile-app HTTP %d)", seeded))
	if b, err := os.ReadFile(filepath.Join(in.evid, "build.txt")); err == nil {
		info("build %s", strings.TrimSpace(string(b)))
	}
	sc := bufio.NewScanner(bytes.NewReader(logData))
	for sc.Scan() {
		if strings.Contains(sc.Text(), "Starting JenkTui") {
			info("%s", ansiEscape.ReplaceAllString(sc.Text(), ""))
			break
		}
	}
	if failed {
		return errors.New("doctor found failures")
	}
	return nil
}

func (in *instance) down(ctx context.Context, _ []string) error {
	if err := os.MkdirAll(in.evid, 0o755); err != nil {
		return err
	}
	if in.alive(ctx) {
		if s, err := in.plainScreen(ctx); err == nil {
			_ = os.WriteFile(filepath.Join(in.evid, "final-screen.txt"), []byte(s), 0o644)
		}
		_, _ = in.tmux(ctx, "send-keys", "-t", "jt", "C-c")
		for i := 0; i < 20 && !in.paneDead(ctx); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		// Only this RUN's private server, never by process name.
		_, _ = in.tmux(ctx, "kill-server")
	}
	// Preserve proof before deleting state. Never copy server.json: it holds
	// credentials.
	if logs := filepath.Join(in.data, "logs"); dirExists(logs) {
		dst := filepath.Join(in.evid, "logs")
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
		if err := os.CopyFS(dst, os.DirFS(logs)); err != nil {
			return fmt.Errorf("copying logs: %w", err)
		}
	}
	if _, err := os.Stat(in.dbPath()); err == nil {
		if err := backupDB(ctx, in.dbPath(), filepath.Join(in.evid, "runs.db")); err != nil {
			fmt.Fprintf(in.stderr, "jt: warning: runs.db backup failed: %v\n", err)
		}
	}
	in.transcriptf("down")
	if filepath.Dir(in.state) != filepath.Clean(in.cfg.stateRoot) {
		return fmt.Errorf("refusing to delete %s", in.state)
	}
	if err := os.RemoveAll(in.state); err != nil {
		return err
	}
	fmt.Fprintf(in.stdout, "stopped RUN=%s; evidence kept in %s\n", in.run, in.evid)
	return nil
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func openReadOnly(path string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+path+"?mode=ro")
}

// backupDB writes a transactionally consistent copy, even if the app was
// killed mid-write.
func backupDB(ctx context.Context, src, dst string) error {
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	db, err := openReadOnly(src)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, "VACUUM INTO ?", dst)
	return err
}
