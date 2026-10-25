// Command jt drives a real, isolated JenkTui instance in a private tmux
// server so agents can verify features the way a user sees them. Every
// command takes a RUN id; one RUN is one isolated $HOME (config, runs.db,
// logs, cache) plus one private tmux socket. Run `jt --help`.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

const usageText = `jt — drive a real, isolated JenkTui instance in a private tmux server.

Build once per session from the repo root, then call the binary:
  go build -o dist/bin/jt ./dev/jt && J=dist/bin/jt

Every command takes a RUN id (any [A-Za-z0-9_-] slug you choose). One RUN =
one isolated $HOME (config, runs.db, logs, cache) + one private tmux socket.

  jt up      RUN            build the app, write isolated config, launch, wait for ready
  jt doctor  RUN            read-only health check of that instance
  jt keys    RUN KEY...     tmux send-keys (named keys: Enter Escape C-p C-b Tab Up ...)
  jt type    RUN TEXT       send TEXT literally (no key-name interpretation)
  jt wait    RUN REGEX [S]  poll the screen until REGEX matches (default 20s)
  jt crumb   RUN REGEX [S]  like wait, but only the footer row (breadcrumb + toast);
                            use it to prove WHICH page is active (dialogs overlay old text)
  jt screen  RUN            print the current screen (plain text)
  jt snap    RUN NAME       save screen to evidence as NAME.txt (+ NAME.ansi)
  jt note    RUN TEXT       append a line to the evidence transcript
  jt db      RUN SQL        read-only query against the instance's runs.db
  jt jenkins RUN PATH       GET PATH from the local Jenkins (e.g. /job/QA/api/json)
  jt log     RUN [N]        tail the instance's jenktui.log (default 40 lines)
  jt down    RUN            stop instance, copy logs+db into evidence, delete state
  jt list                   list RUNs that still have state (candidates for down)

Run "jt COMMAND --help" for details and examples.

Env overrides:
  JT_JENKINS_URL      (default http://127.0.0.1:8080, must be localhost)
  JT_JENKINS_USER     (default admin)    JT_JENKINS_TOKEN (default admin)
  JT_STATE_ROOT       (default ${TMPDIR:-/tmp}/jenktui-verify)  — deleted by down
  JT_EVIDENCE_ROOT    (default <repo>/dist/verify)              — survives down
  JT_COLS / JT_ROWS   terminal size (default 160x45)
  JT_CONFIG_JSON      replacement config.json (JSON object; missing fields use defaults)
  JT_DEVELOPMENT=1    start the app with DEVELOPMENT=1 (pprof on localhost:6060; one
                      instance at a time) for ` + "`go run ./dev/perf live`" + `
  JT_SECOND_SERVER=1  add a second server.json entry (id local-b, name "Local B",
                      same Jenkins) so the server selector has something to switch to

Exit codes: 0 ok, 1 failure (timeout, app died, check failed), 2 usage error.
`

type usageError struct {
	msg      string
	examples []string
}

func (e *usageError) Error() string { return e.msg }

func usageErr(msg string, examples ...string) error {
	return &usageError{msg: msg, examples: examples}
}

// errHelpShown ends a command after it printed its --help.
var errHelpShown = errors.New("help shown")

var runPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type command struct {
	help string
	// min/max positional arguments after RUN; max < 0 means unbounded.
	min, max int
	run      func(*instance, context.Context, []string) error
}

var commands = map[string]command{
	"up":      {helpUp, 0, 0, (*instance).up},
	"doctor":  {helpDoctor, 0, 0, (*instance).doctor},
	"keys":    {helpKeys, 1, -1, (*instance).keys},
	"type":    {helpType, 1, 1, (*instance).typeText},
	"wait":    {helpWait, 1, 2, (*instance).wait},
	"crumb":   {helpCrumb, 1, 2, (*instance).crumb},
	"screen":  {helpScreen, 0, 0, (*instance).screen},
	"snap":    {helpSnap, 1, 1, (*instance).snap},
	"note":    {helpNote, 1, -1, (*instance).note},
	"db":      {helpDB, 1, 1, (*instance).db},
	"jenkins": {helpJenkins, 1, 1, (*instance).jenkins},
	"log":     {helpLog, 0, 1, (*instance).log},
	"down":    {helpDown, 0, 0, (*instance).down},
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	err := dispatch(ctx, args, stdout, stderr)
	var ue *usageError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ue):
		fmt.Fprintf(stderr, "jt: error: %s\n", ue.msg)
		for _, ex := range ue.examples {
			fmt.Fprintf(stderr, "  %s\n", ex)
		}
		return 2
	default:
		fmt.Fprintf(stderr, "jt: %v\n", err)
		return 1
	}
}

func dispatch(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || isHelp(args[0]) || args[0] == "help" {
		fmt.Fprint(stdout, usageText)
		return nil
	}
	name, rest := args[0], args[1:]
	if name == "list" {
		if len(rest) > 0 && isHelp(rest[0]) {
			fmt.Fprint(stdout, helpList)
			return nil
		}
		cfg, err := loadConfig(ctx)
		if err != nil {
			return err
		}
		return listRuns(cfg, stdout)
	}
	cmd, ok := commands[name]
	if !ok {
		return usageErr(fmt.Sprintf("unknown command %q", name), "jt --help", "jt up demo")
	}
	// --help is honored anywhere except as literal text for type/note/wait.
	if len(rest) > 0 && isHelp(rest[0]) || len(rest) > 1 && isHelp(rest[len(rest)-1]) && name != "type" && name != "note" {
		fmt.Fprint(stdout, cmd.help)
		return nil
	}
	if len(rest) == 0 {
		return usageErr(name+" needs a RUN id", exampleOf(cmd.help))
	}
	runID, params := rest[0], rest[1:]
	if !runPattern.MatchString(runID) {
		return usageErr(fmt.Sprintf("RUN id %q must match [A-Za-z0-9_-]+", runID), exampleOf(cmd.help))
	}
	if len(params) < cmd.min || cmd.max >= 0 && len(params) > cmd.max {
		return usageErr(fmt.Sprintf("%s got %d argument(s) after RUN", name, len(params)), exampleOf(cmd.help))
	}
	cfg, err := loadConfig(ctx)
	if err != nil {
		return err
	}
	in := newInstance(cfg, runID, stdout, stderr)
	return cmd.run(in, ctx, params)
}

func isHelp(arg string) bool { return arg == "-h" || arg == "--help" }

// exampleOf returns the first example invocation from a help text.
func exampleOf(help string) string {
	_, after, ok := strings.Cut(help, "Examples:\n")
	if !ok {
		return "jt --help"
	}
	line, _, _ := strings.Cut(after, "\n")
	return strings.TrimSpace(line)
}

type config struct {
	repo         string
	jenkinsURL   string
	user         string
	token        string
	stateRoot    string
	evidenceRoot string
	cols, rows   int
	configJSON   string
	secondServer bool
	development  bool
}

// Notifications off so verification never pops desktop alerts.
const defaultConfigJSON = `{"poll_interval":3,"notification":{"desktop_enabled":false,"sound_enabled":false}}`

func loadConfig(ctx context.Context) (config, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return config{}, errors.New("run jt from inside the jenktui git repository")
	}
	repo := strings.TrimSpace(string(out))
	cfg := config{
		repo:         repo,
		jenkinsURL:   envOr("JT_JENKINS_URL", "http://127.0.0.1:8080"),
		user:         envOr("JT_JENKINS_USER", "admin"),
		token:        envOr("JT_JENKINS_TOKEN", "admin"),
		stateRoot:    envOr("JT_STATE_ROOT", filepath.Join(os.TempDir(), "jenktui-verify")),
		evidenceRoot: envOr("JT_EVIDENCE_ROOT", filepath.Join(repo, "dist", "verify")),
		configJSON:   envOr("JT_CONFIG_JSON", defaultConfigJSON),
		secondServer: os.Getenv("JT_SECOND_SERVER") == "1",
		development:  os.Getenv("JT_DEVELOPMENT") != "",
	}
	if cfg.cols, err = envInt("JT_COLS", 160); err != nil {
		return config{}, err
	}
	if cfg.rows, err = envInt("JT_ROWS", 45); err != nil {
		return config{}, err
	}
	if !isLocalURL(cfg.jenkinsURL) {
		return config{}, fmt.Errorf("JT_JENKINS_URL=%s is not local; refusing to drive a non-disposable Jenkins", cfg.jenkinsURL)
	}
	if !json.Valid([]byte(cfg.configJSON)) {
		return config{}, usageErr("JT_CONFIG_JSON is not valid JSON",
			`JT_CONFIG_JSON='{"theme":"Nord","notification":{"desktop_enabled":false}}' jt up demo`)
	}
	return cfg, nil
}

func isLocalURL(u string) bool {
	return strings.HasPrefix(u, "http://127.0.0.1:") || strings.HasPrefix(u, "http://localhost:")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 20 {
		return 0, usageErr(fmt.Sprintf("%s=%q must be an integer >= 20", key, v), key+"=200 jt up demo")
	}
	return n, nil
}

func listRuns(cfg config, w io.Writer) error {
	entries, err := os.ReadDir(cfg.stateRoot)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() && runPattern.MatchString(e.Name()) {
			fmt.Fprintf(w, "%s\tstate=%s\n", e.Name(), filepath.Join(cfg.stateRoot, e.Name()))
		}
	}
	return nil
}
