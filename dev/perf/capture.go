package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const worktreeRef = "worktree"

// page is one offline animation workload: the package that holds it, its
// replay benchmark, and its live-profile test.
type page struct {
	name        string
	pkg         string
	benchmark   string
	profileTest string
	// legacyBench matches older benchmark names that are not comparable with
	// benchmark; nil when the page never had one.
	legacyBench *regexp.Regexp
}

var pages = map[string]page{
	"setup": {name: "setup", pkg: "./src/ui/components/dialogs/serversetup", benchmark: "BenchmarkServerSetupFrame", profileTest: "TestServerSetupProfile"},
	"job": {
		name: "job", pkg: "./src/ui/components/job",
		benchmark: "BenchmarkJobPageAnimationFrame", profileTest: "TestJobAnimationProfile",
		legacyBench: regexp.MustCompile(`^BenchmarkJobPageAnimation`),
	},
	"build": {
		name: "build", pkg: "./src/ui/components/build",
		benchmark: "BenchmarkBuildPageAnimationFrame", profileTest: "TestBuildAnimationProfile",
	},
}

func (p page) testBinary() string { return p.name + ".test" }

// Match `task bench:job` / `task profile:job`.
var measureEnv = []string{"TZ=UTC", "GOMAXPROCS=2", "CGO_ENABLED=0", "GOEXPERIMENT=greenteagc"}

var labelPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type measureOpts struct {
	page        page
	pageName    string
	rounds      int
	benchCount  int
	benchtime   string
	duration    time.Duration
	benchOnly   bool
	profileOnly bool
	quick       bool
	force       bool
}

func addMeasureFlags(fs *flag.FlagSet, o *measureOpts) {
	fs.StringVar(&o.pageName, "page", "job", "")
	fs.IntVar(&o.rounds, "rounds", 5, "")
	fs.IntVar(&o.benchCount, "bench-count", 2, "")
	fs.StringVar(&o.benchtime, "benchtime", "1s", "")
	fs.DurationVar(&o.duration, "duration", 10*time.Second, "")
	fs.BoolVar(&o.benchOnly, "bench-only", false, "")
	fs.BoolVar(&o.profileOnly, "profile-only", false, "")
	fs.BoolVar(&o.quick, "quick", false, "")
	fs.BoolVar(&o.force, "force", false, "")
}

func (o *measureOpts) validate() error {
	p, ok := pages[o.pageName]
	if !ok {
		return usageErr(fmt.Sprintf("unknown --page %q (job, build, setup)", o.pageName), "go run ./dev/perf capture my-change --page build")
	}
	o.page = p
	if o.quick {
		o.rounds, o.benchCount, o.duration = 1, 3, 3*time.Second
	}
	switch {
	case o.rounds < 1:
		return usageErr("--rounds must be >= 1", "go run ./dev/perf ab my-change --base HEAD --rounds 5")
	case o.benchCount < 1:
		return usageErr("--bench-count must be >= 1")
	case o.duration < time.Second:
		return usageErr("--duration must be >= 1s (the profile harness rejects shorter runs)", "--duration 10s")
	case o.benchOnly && o.profileOnly:
		return usageErr("--bench-only and --profile-only exclude each other")
	}
	return nil
}

// estimate is a rough wall-clock guess so callers can decide to background.
func (o *measureOpts) estimate(sides int) time.Duration {
	bt, err := time.ParseDuration(o.benchtime)
	if err != nil {
		bt = time.Second
	}
	var perRound time.Duration
	if !o.profileOnly {
		perRound += time.Duration(o.benchCount)*2*bt + time.Second
	}
	if !o.benchOnly {
		perRound += o.duration + 2*time.Second
	}
	return time.Duration(sides) * (time.Duration(o.rounds)*perRound + 15*time.Second)
}

// side is one source being measured into one capture directory.
type side struct {
	page       page
	label      string
	ref        string
	src        string
	out        string
	bench      string
	hasProfile bool
}

func (a *app) claimLabel(label string, force bool) (string, error) {
	if !labelPattern.MatchString(label) {
		return "", usageErr(fmt.Sprintf("label %q must match [A-Za-z0-9._-]+", label), "go run ./dev/perf capture my-change")
	}
	out := filepath.Join(a.root, label)
	if _, err := os.Stat(out); err == nil {
		if !force {
			return "", usageErr(fmt.Sprintf("capture %q already exists at %s", label, out),
				"go run ./dev/perf summary "+label+"   # read it", "add --force to replace it")
		}
		if err := os.RemoveAll(out); err != nil {
			return "", err
		}
	}
	return out, os.MkdirAll(out, 0o755)
}

// worktrees builds git refs in detached temporary worktrees and removes them
// when measurement ends, including on interrupt.
type worktrees struct {
	repo string
	dirs []string
}

func (w *worktrees) resolve(ctx context.Context, ref string) (string, error) {
	if ref == worktreeRef {
		return w.repo, nil
	}
	sha, err := git(ctx, w.repo, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", usageErr(fmt.Sprintf("unknown git ref %q", ref),
			"go run ./dev/perf ab my-change --base HEAD~1", "go run ./dev/perf ab my-change --base main --head worktree")
	}
	dir := filepath.Join(os.TempDir(), "jenktui-perf-wt", fmt.Sprintf("%.12s-%d-%d", sha, os.Getpid(), len(w.dirs)))
	if _, err := git(ctx, w.repo, "worktree", "add", "--detach", "--force", dir, sha); err != nil {
		return "", err
	}
	w.dirs = append(w.dirs, dir)
	return dir, nil
}

func (w *worktrees) cleanup() {
	// The measurement context may already be cancelled.
	for _, dir := range w.dirs {
		_, _ = git(context.Background(), w.repo, "worktree", "remove", "--force", dir)
	}
	w.dirs = nil
}

func (a *app) prepareSide(ctx context.Context, wt *worktrees, s *side) error {
	src, err := wt.resolve(ctx, s.ref)
	if err != nil {
		return err
	}
	s.src = src
	commit, err := git(ctx, src, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	goVersion, _ := exec.CommandContext(ctx, "go", "env", "GOVERSION").Output()
	if err := appendEnvironment(s.out,
		"source", s.ref, "commit", commit, "dirty_files", dirtyFiles(ctx, src),
		"go", strings.TrimSpace(string(goVersion)), "goexperiment", "greenteagc", "gomaxprocs", "2",
		"host", runtime.GOOS+"/"+runtime.GOARCH, "cpu", cpuModel(ctx),
		"nproc", strconv.Itoa(runtime.NumCPU()), "load1_at_start", load1String(ctx)); err != nil {
		return err
	}

	a.logf("building %s (%s)", s.label, s.ref)
	p := s.page
	bin := filepath.Join(s.out, p.testBinary())
	build := exec.CommandContext(ctx, "go", "test", "-c", "-o", bin, p.pkg)
	build.Dir, build.Env = src, append(os.Environ(), measureEnv...)
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("go test -c failed for %s (%s): %w\n%s", s.label, s.ref, err, out)
	}

	listed, err := exec.CommandContext(ctx, bin, "-test.list", ".").Output()
	if err != nil {
		return fmt.Errorf("listing tests in %s: %w", bin, err)
	}
	for _, name := range strings.Fields(string(listed)) {
		switch {
		case name == p.profileTest:
			s.hasProfile = true
		case name == p.benchmark:
			s.bench = name
		case s.bench == "" && p.legacyBench != nil && p.legacyBench.MatchString(name):
			s.bench = name
		}
	}
	if s.bench == "" {
		return fmt.Errorf("%s has no %s replay benchmark; pick a newer ref", s.ref, p.benchmark)
	}
	if s.bench != p.benchmark {
		a.logf("warning: %s only has %s (3 renders/op); do not benchstat it against %s", s.ref, s.bench, p.benchmark)
	}
	return appendEnvironment(s.out, "page", p.name, "benchmark", s.bench, "has_profile", strconv.FormatBool(s.hasProfile))
}

// runTest runs the side's test binary from its package directory so
// relative testdata paths resolve as under `go test`.
func runTest(ctx context.Context, s *side, logPath string, extraEnv []string, args ...string) error {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := exec.CommandContext(ctx, filepath.Join(s.out, s.page.testBinary()), args...)
	cmd.Dir = filepath.Join(s.src, filepath.FromSlash(strings.TrimPrefix(s.page.pkg, "./")))
	cmd.Env = append(append(os.Environ(), measureEnv...), extraEnv...)
	cmd.Stdout, cmd.Stderr = f, f
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w (see %s)", filepath.Base(cmd.Path), err, logPath)
	}
	return nil
}

func (a *app) benchRound(ctx context.Context, s *side, o *measureOpts) error {
	return runTest(ctx, s, filepath.Join(s.out, "bench.txt"), nil,
		"-test.run", "^$", "-test.bench", "^"+s.bench+"$", "-test.benchmem",
		"-test.benchtime", o.benchtime, "-test.count", strconv.Itoa(o.benchCount))
}

func (a *app) profileRound(ctx context.Context, s *side, o *measureOpts, round int) error {
	if !s.hasProfile {
		return nil
	}
	dir := filepath.Join(s.out, fmt.Sprintf("profile-%d", round))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return runTest(ctx, s, filepath.Join(dir, "test.log"),
		[]string{"JENKTUI_ANIMATION_PROFILE_DIR=" + dir, "JENKTUI_ANIMATION_PROFILE_DURATION=" + o.duration.String()},
		"-test.run", "^"+s.page.profileTest+"$", "-test.v", "-test.timeout", "2m")
}

// measure builds every side, then alternates them round by round
// (A B A B ...) so warmup and machine drift affect all sides alike.
func (a *app) measure(ctx context.Context, o *measureOpts, sides []*side) error {
	wt := &worktrees{repo: a.repo}
	defer wt.cleanup()
	a.warnIfBusy(ctx)
	a.logf("estimated run time: ~%s (run in the background if your tool times out)", o.estimate(len(sides)).Round(time.Second))
	for _, s := range sides {
		if err := a.prepareSide(ctx, wt, s); err != nil {
			return err
		}
	}
	for r := 1; r <= o.rounds; r++ {
		for _, s := range sides {
			a.logf("round %d/%d: %s", r, o.rounds, s.label)
			if !o.profileOnly {
				if err := a.benchRound(ctx, s, o); err != nil {
					return err
				}
			}
			if !o.benchOnly {
				if err := a.profileRound(ctx, s, o, r); err != nil {
					return err
				}
			}
		}
	}
	for _, s := range sides {
		if err := appendEnvironment(s.out, "load1_at_end", load1String(ctx)); err != nil {
			return err
		}
		a.logf("done: %s", s.out)
	}
	return nil
}

func (a *app) capture(ctx context.Context, args []string) error {
	fs := newFlagSet("capture")
	var o measureOpts
	addMeasureFlags(fs, &o)
	ref := fs.String("ref", worktreeRef, "")
	asJSON := fs.Bool("json", false, "")
	pos, err := parseArgs(fs, args, 1, "go run ./dev/perf capture my-change", "go run ./dev/perf capture baseline --ref main")
	if err != nil {
		return err
	}
	if err := o.validate(); err != nil {
		return err
	}
	out, err := a.claimLabel(pos[0], o.force)
	if err != nil {
		return err
	}
	sides := []*side{{page: o.page, label: pos[0], ref: *ref, out: out}}
	if err := a.measure(ctx, &o, sides); err != nil {
		return errors.Join(err, a.cleanupFailed(sides))
	}
	return a.printSummary(pos[0], *asJSON)
}

func (a *app) ab(ctx context.Context, args []string) error {
	fs := newFlagSet("ab")
	var o measureOpts
	addMeasureFlags(fs, &o)
	base := fs.String("base", "", "")
	head := fs.String("head", worktreeRef, "")
	asJSON := fs.Bool("json", false, "")
	pos, err := parseArgs(fs, args, 1, "go run ./dev/perf ab my-change --base HEAD")
	if err != nil {
		return err
	}
	name := pos[0]
	if *base == "" {
		return usageErr("ab needs --base REF",
			"go run ./dev/perf ab "+name+" --base HEAD", "go run ./dev/perf ab "+name+" --base main --head worktree")
	}
	if err := o.validate(); err != nil {
		return err
	}
	sides := []*side{{page: o.page, label: name + "-base", ref: *base}, {page: o.page, label: name + "-head", ref: *head}}
	for _, s := range sides {
		if s.out, err = a.claimLabel(s.label, o.force); err != nil {
			return err
		}
	}
	if err := a.measure(ctx, &o, sides); err != nil {
		return errors.Join(err, a.cleanupFailed(sides))
	}
	return a.printComparison(ctx, sides[0].label, sides[1].label, *asJSON)
}

// cleanupFailed removes half-written captures so a retry needs no --force.
func (a *app) cleanupFailed(sides []*side) error {
	var errs []error
	for _, s := range sides {
		errs = append(errs, os.RemoveAll(s.out))
	}
	return errors.Join(errs...)
}
