package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// load1 returns the one-minute load average where the OS exposes it.
func load1(ctx context.Context) (float64, bool) {
	var raw string
	switch runtime.GOOS {
	case "linux":
		data, err := os.ReadFile("/proc/loadavg")
		if err != nil {
			return 0, false
		}
		raw = string(data)
	case "darwin", "freebsd":
		out, err := exec.CommandContext(ctx, "sysctl", "-n", "vm.loadavg").Output()
		if err != nil {
			return 0, false
		}
		raw = strings.Trim(strings.TrimSpace(string(out)), "{} ")
	default:
		return 0, false
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	return v, err == nil
}

func load1String(ctx context.Context) string {
	if v, ok := load1(ctx); ok {
		return strconv.FormatFloat(v, 'f', 2, 64)
	}
	return "unknown"
}

func cpuModel(ctx context.Context) string {
	switch runtime.GOOS {
	case "linux":
		f, err := os.Open("/proc/cpuinfo")
		if err != nil {
			return "unknown"
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if name, value, ok := strings.Cut(sc.Text(), ":"); ok && strings.TrimSpace(name) == "model name" {
				return strings.TrimSpace(value)
			}
		}
	case "darwin":
		if out, err := exec.CommandContext(ctx, "sysctl", "-n", "machdep.cpu.brand_string").Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return "unknown"
}

// warnIfBusy reports when other load is likely to swamp run-to-run noise.
func (a *app) warnIfBusy(ctx context.Context) {
	l, ok := load1(ctx)
	if !ok || l <= float64(runtime.NumCPU())*0.25 {
		return
	}
	a.logf("warning: load1=%.2f on %d cores; numbers will be noisy, interleaving keeps the comparison fair", l, runtime.NumCPU())
	if out, err := exec.CommandContext(ctx, "ps", "-eo", "pcpu,comm", "--sort=-pcpu").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, line := range lines[1:min(len(lines), 6)] {
			fmt.Fprintf(a.stderr, "  %s\n", strings.TrimSpace(line))
		}
	}
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func dirtyFiles(ctx context.Context, dir string) string {
	out, err := git(ctx, dir, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return "unknown"
	}
	if out == "" {
		return "0"
	}
	return strconv.Itoa(len(strings.Split(out, "\n")))
}

// environment.txt holds "key: value" lines. Captures made by `task
// profile:job` use a raw format whose first line is the commit hash.
const environmentFile = "environment.txt"

var commitLine = regexp.MustCompile(`^[0-9a-f]{40}$`)

func writeKV(w io.Writer, key, value string) {
	fmt.Fprintf(w, "%s: %s\n", key, value)
}

func appendEnvironment(dir string, kv ...string) error {
	f, err := os.OpenFile(filepath.Join(dir, environmentFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	for i := 0; i+1 < len(kv); i += 2 {
		writeKV(f, kv[i], kv[i+1])
	}
	return f.Close()
}

func readEnvironment(dir string) map[string]string {
	env := map[string]string{}
	data, err := os.ReadFile(filepath.Join(dir, environmentFile))
	if err != nil {
		return env
	}
	for i, line := range strings.Split(string(data), "\n") {
		if i == 0 && commitLine.MatchString(line) {
			env["commit"] = line
			continue
		}
		if key, value, ok := strings.Cut(line, ": "); ok && !strings.Contains(key, " ") {
			if _, seen := env[key]; !seen {
				env[key] = value
			}
		}
	}
	return env
}
