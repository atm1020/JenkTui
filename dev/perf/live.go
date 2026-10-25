package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// pprofURL is where main.go serves net/http/pprof when DEVELOPMENT is set.
const pprofURL = "http://localhost:6060/debug/pprof"

func pprofUp(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return fetch(ctx, pprofURL+"/", io.Discard) == nil
}

func fetch(ctx context.Context, url string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func fetchFile(ctx context.Context, url, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := fetch(ctx, url, f); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (a *app) live(ctx context.Context, args []string) error {
	fs := newFlagSet("live")
	duration := fs.Duration("duration", 10*time.Second, "")
	force := fs.Bool("force", false, "")
	pos, err := parseArgs(fs, args, 1, "go run ./dev/perf live home-idle")
	if err != nil {
		return err
	}
	if *duration < time.Second || *duration%time.Second != 0 {
		return usageErr("--duration must be whole seconds, at least 1s", "go run ./dev/perf live "+pos[0]+" --duration 15s")
	}
	if !pprofUp(ctx) {
		return usageErr("no pprof server at localhost:6060",
			"go build -o dist/bin/jt ./dev/jt && JT_DEVELOPMENT=1 dist/bin/jt up perf   # isolated app, needs local Jenkins",
			"or prefer the offline workload: go run ./dev/perf capture my-change")
	}
	out, err := a.claimLabel(pos[0], *force)
	if err != nil {
		return err
	}
	commit, _ := git(ctx, a.repo, "rev-parse", "HEAD")
	if err := appendEnvironment(out, "source", "live-app", "commit", commit,
		"host", runtime.GOOS+"/"+runtime.GOARCH, "load1_at_start", load1String(ctx)); err != nil {
		return err
	}
	dir := filepath.Join(out, "live")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	secs := strconv.Itoa(int(duration.Seconds()))
	a.logf("sampling CPU for %ss", secs)
	cpuDone := make(chan error, 1)
	go func() { cpuDone <- fetchFile(ctx, pprofURL+"/profile?seconds="+secs, filepath.Join(dir, "cpu.pprof")) }()
	var errs []string
	for _, name := range []string{"heap", "goroutine"} {
		if err := fetchFile(ctx, pprofURL+"/"+name, filepath.Join(dir, name+".pprof")); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if err := <-cpuDone; err != nil {
		errs = append(errs, err.Error())
	}
	if err := fetchFile(ctx, pprofURL+"/allocs", filepath.Join(dir, "allocs.pprof")); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("downloading profiles: %s", strings.Join(errs, "; "))
	}
	if err := appendEnvironment(out, "load1_at_end", load1String(ctx)); err != nil {
		return err
	}
	return a.printSummary(pos[0], false)
}

func (a *app) doctor(ctx context.Context, args []string) error {
	if _, err := parseArgs(newFlagSet("doctor"), args, 0, "go run ./dev/perf doctor"); err != nil {
		return err
	}
	w := a.stdout
	missing := false
	for _, tool := range []string{"go", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			writeKV(w, tool, "MISSING")
			missing = true
		} else {
			writeKV(w, tool, "ok")
		}
	}
	if _, err := exec.LookPath("benchstat"); err != nil {
		writeKV(w, "benchstat", "missing (task benchstat:install); compare still reports medians")
	} else {
		writeKV(w, "benchstat", "ok")
	}
	if v, err := exec.CommandContext(ctx, "go", "env", "GOVERSION").Output(); err == nil {
		writeKV(w, "go_version", strings.TrimSpace(string(v)))
	}
	writeKV(w, "os", runtime.GOOS+"/"+runtime.GOARCH)
	writeKV(w, "nproc", strconv.Itoa(runtime.NumCPU()))
	writeKV(w, "load1", load1String(ctx))
	writeKV(w, "dirty_files", dirtyFiles(ctx, a.repo))
	if head, err := git(ctx, a.repo, "rev-parse", "--short", "HEAD"); err == nil {
		writeKV(w, "head", head)
	}
	writeKV(w, "captures_dir", a.root)
	if pprofUp(ctx) {
		writeKV(w, "pprof_6060", "up")
	} else {
		writeKV(w, "pprof_6060", "down")
	}
	a.warnIfBusy(ctx)
	if missing {
		return fmt.Errorf("required tools missing")
	}
	return nil
}

func (a *app) list(_ context.Context, args []string) error {
	if _, err := parseArgs(newFlagSet("list"), args, 0, "go run ./dev/perf list"); err != nil {
		return err
	}
	entries, err := os.ReadDir(a.root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		env := readEnvironment(filepath.Join(a.root, e.Name()))
		commit := env["commit"]
		if len(commit) > 8 {
			commit = commit[:8]
		}
		fmt.Fprintf(a.stdout, "%-24s %-8s %s\n", e.Name(), commit, env["source"])
	}
	return nil
}
