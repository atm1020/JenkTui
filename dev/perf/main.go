// Command perf measures JenkTui rendering performance without a human in the
// loop: interleaved before/after runs of the offline job-page workload,
// benchstat, focused pprof totals, noise verdicts, and a sanity check that the
// measured work happened. Run `go run ./dev/perf --help`.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

const usageText = `perf — measure JenkTui rendering performance without a human in the loop.

The offline workloads (--page job: job-animation-v1, a 160x50 job page with 20
builds and one running stage; --page build: build-animation-v1, a 160x50 build
page with a running stage and step) use the real Bubble Tea renderer into a
counting sink and need no Jenkins, no TTY, and no personal config. Every
command is non-interactive.

Usage: go run ./dev/perf COMMAND [args] [flags]

Commands:
  doctor                         check toolchain, machine load, dirty tree, pprof :6060
  capture LABEL [flags]          measure one source (working tree or --ref)
  ab NAME --base REF [flags]     interleaved A/B of two sources, then compare
  summary LABEL [--json]         metrics for one capture
  compare BEFORE AFTER [--json]  benchstat, metric deltas, noise verdicts
  live LABEL [flags]             profile a running DEVELOPMENT=1 app (pprof :6060)
  list                           list captures

Run "go run ./dev/perf COMMAND --help" for flags and examples.
Captures go to dist/profiles/LABEL (PERF_ROOT overrides).
Exit codes: 0 ok, 1 failure, 2 usage error, 3 measurement sanity check failed.
`

// errSanity marks a capture whose work did not happen (no views or frames).
var errSanity = errors.New("measurement sanity check failed")

// errHelpShown ends a command after it printed its --help.
var errHelpShown = errors.New("help shown")

type usageError struct {
	msg      string
	examples []string
}

func (e *usageError) Error() string { return e.msg }

func usageErr(msg string, examples ...string) error {
	return &usageError{msg: msg, examples: examples}
}

type app struct {
	repo   string
	root   string
	stdout io.Writer
	stderr io.Writer
}

func (a *app) logf(format string, args ...any) {
	fmt.Fprintf(a.stderr, "perf: "+format+"\n", args...)
}

type command struct {
	help string
	run  func(*app, context.Context, []string) error
}

var commands = map[string]command{
	"doctor":  {helpDoctor, (*app).doctor},
	"capture": {helpCapture, (*app).capture},
	"ab":      {helpAB, (*app).ab},
	"summary": {helpSummary, (*app).summary},
	"compare": {helpCompare, (*app).compare},
	"live":    {helpLive, (*app).live},
	"list":    {helpList, (*app).list},
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, usageText)
		return 0
	}
	cmd, ok := commands[args[0]]
	if !ok {
		return report(stderr, usageErr(fmt.Sprintf("unknown command %q", args[0]),
			"go run ./dev/perf --help", "go run ./dev/perf capture my-change"))
	}
	repo, err := repoRoot(ctx)
	if err != nil {
		return report(stderr, err)
	}
	a := &app{repo: repo, root: captureRoot(repo), stdout: stdout, stderr: stderr}
	err = cmd.run(a, ctx, args[1:])
	if errors.Is(err, errHelpShown) {
		fmt.Fprint(stdout, cmd.help)
		return 0
	}
	return report(stderr, err)
}

func report(stderr io.Writer, err error) int {
	var ue *usageError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ue):
		fmt.Fprintf(stderr, "perf: error: %s\n", ue.msg)
		for _, ex := range ue.examples {
			fmt.Fprintf(stderr, "  %s\n", ex)
		}
		return 2
	case errors.Is(err, errSanity):
		fmt.Fprintf(stderr, "perf: %v\n", err)
		return 3
	default:
		fmt.Fprintf(stderr, "perf: error: %v\n", err)
		return 1
	}
}

func repoRoot(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", errors.New("run from inside the jenktui git repository")
	}
	return strings.TrimSpace(string(out)), nil
}

func captureRoot(repo string) string {
	if root := os.Getenv("PERF_ROOT"); root != "" {
		return root
	}
	return filepath.Join(repo, "dist", "profiles")
}

// parseArgs parses flags that may appear before, between, or after
// positionals, and checks the positional count.
func parseArgs(fs *flag.FlagSet, args []string, wantPos int, examples ...string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, errHelpShown
			}
			return nil, usageErr(err.Error(), append([]string{"go run ./dev/perf " + fs.Name() + " --help"}, examples...)...)
		}
		args = fs.Args()
		if len(args) == 0 {
			break
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
	if len(pos) != wantPos {
		return nil, usageErr(fmt.Sprintf("%s takes %d argument(s), got %d", fs.Name(), wantPos, len(pos)), examples...)
	}
	return pos, nil
}

func newFlagSet(name string) *flag.FlagSet {
	return flag.NewFlagSet(name, flag.ContinueOnError)
}
