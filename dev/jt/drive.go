package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

func (in *instance) keys(ctx context.Context, args []string) error {
	if err := in.needAlive(ctx); err != nil {
		return err
	}
	if _, err := in.tmux(ctx, append([]string{"send-keys", "-t", "jt"}, args...)...); err != nil {
		return err
	}
	in.transcriptf("keys: %s", strings.Join(args, " "))
	return nil
}

func (in *instance) typeText(ctx context.Context, args []string) error {
	if err := in.needAlive(ctx); err != nil {
		return err
	}
	if _, err := in.tmux(ctx, "send-keys", "-t", "jt", "-l", "--", args[0]); err != nil {
		return err
	}
	in.transcriptf("type: %s", args[0])
	return nil
}

func (in *instance) wait(ctx context.Context, args []string) error {
	return in.waitCommand(ctx, "wait", args)
}

func (in *instance) crumb(ctx context.Context, args []string) error {
	return in.waitCommand(ctx, "crumb", args)
}

func (in *instance) waitCommand(ctx context.Context, name string, args []string) error {
	if err := in.needAlive(ctx); err != nil {
		return err
	}
	re, err := regexp.Compile(args[0])
	if err != nil {
		return usageErr(fmt.Sprintf("bad REGEX: %v", err), "jt "+name+" demo 'Health Score'")
	}
	timeout := 20 * time.Second
	if len(args) == 2 {
		secs, err := strconv.ParseFloat(args[1], 64)
		if err != nil || secs <= 0 {
			return usageErr(fmt.Sprintf("SECONDS %q must be a positive number", args[1]), "jt "+name+" demo 'Health Score' 30")
		}
		timeout = time.Duration(secs * float64(time.Second))
	}
	if err := in.waitFor(ctx, re, timeout, name == "crumb"); err != nil {
		in.transcriptf("%s TIMEOUT: /%s/ after %s", name, args[0], timeout)
		return err
	}
	in.transcriptf("%s ok: /%s/", name, args[0])
	fmt.Fprintf(in.stdout, "%s matched /%s/\n", name, args[0])
	return nil
}

// footerRow returns the last non-blank row: breadcrumb plus toast.
func footerRow(screen string) string {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}

func (in *instance) waitFor(ctx context.Context, re *regexp.Regexp, timeout time.Duration, footerOnly bool) error {
	deadline := time.Now().Add(timeout)
	for {
		screen, err := in.plainScreen(ctx)
		if err == nil {
			region := screen
			if footerOnly {
				region = footerRow(screen)
			}
			if re.MatchString(region) {
				return nil
			}
		}
		if in.paneDead(ctx) {
			in.dumpScreen(ctx)
			return fmt.Errorf("app exited while waiting for /%s/", re)
		}
		if time.Now().After(deadline) {
			in.dumpScreen(ctx)
			return fmt.Errorf("timed out after %s waiting for /%s/", timeout, re)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (in *instance) screen(ctx context.Context, _ []string) error {
	if err := in.needAlive(ctx); err != nil {
		return err
	}
	s, err := in.plainScreen(ctx)
	if err != nil {
		return err
	}
	fmt.Fprint(in.stdout, s)
	return nil
}

var snapName = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func (in *instance) snap(ctx context.Context, args []string) error {
	if err := in.needAlive(ctx); err != nil {
		return err
	}
	name := args[0]
	if !snapName.MatchString(name) {
		return usageErr(fmt.Sprintf("snap NAME %q must match [A-Za-z0-9._-]+", name), "jt snap demo fav-toggle-after")
	}
	if err := os.MkdirAll(in.evid, 0o755); err != nil {
		return err
	}
	plain, err := in.plainScreen(ctx)
	if err != nil {
		return err
	}
	colored, err := in.tmux(ctx, "capture-pane", "-p", "-e", "-t", "jt")
	if err != nil {
		return err
	}
	txt := filepath.Join(in.evid, name+".txt")
	if err := os.WriteFile(txt, []byte(plain), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(in.evid, name+".ansi"), []byte(colored), 0o644); err != nil {
		return err
	}
	in.transcriptf("snap: %s", name)
	fmt.Fprintln(in.stdout, txt)
	return nil
}

func (in *instance) note(_ context.Context, args []string) error {
	in.transcriptf("note: %s", strings.Join(args, " "))
	return nil
}

func (in *instance) db(ctx context.Context, args []string) error {
	if _, err := os.Stat(in.dbPath()); err != nil {
		return fmt.Errorf("no runs.db yet at %s", in.dbPath())
	}
	db, err := openReadOnly(in.dbPath())
	if err != nil {
		return err
	}
	defer db.Close()
	var out bytes.Buffer
	if err := queryTable(ctx, db, args[0], &out); err != nil {
		return err
	}
	fmt.Fprint(in.stdout, out.String())
	in.transcriptf("db: %s", args[0])
	var indented strings.Builder
	for _, line := range strings.SplitAfter(out.String(), "\n") {
		if line != "" {
			indented.WriteString("    " + line)
		}
	}
	appendFile(filepath.Join(in.evid, "transcript.log"), indented.String())
	return nil
}

// queryTable prints rows like `sqlite3 -header -column`.
func queryTable(ctx context.Context, db *sql.DB, query string, w io.Writer) error {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(cols, "\t"))
	dashes := make([]string, len(cols))
	for i, c := range cols {
		dashes[i] = strings.Repeat("-", max(len(c), 3))
	}
	fmt.Fprintln(tw, strings.Join(dashes, "\t"))
	values := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		cells := make([]string, len(cols))
		for i, v := range values {
			cells[i] = strings.ReplaceAll(v.String, "\n", " ")
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return tw.Flush()
}

func appendFile(path, content string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(content)
}

func (in *instance) jenkins(ctx context.Context, args []string) error {
	path := args[0]
	if !strings.HasPrefix(path, "/") {
		return usageErr(fmt.Sprintf("PATH %q must start with /", path), "jt jenkins demo /job/QA/api/json")
	}
	code, body, err := in.jenkinsGet(ctx, path)
	if err != nil {
		return err
	}
	if code < 200 || code > 299 {
		return fmt.Errorf("GET %s: HTTP %d", path, code)
	}
	// A trailing newline, as the recipes expect (`... consoleText | wc -l`).
	fmt.Fprintf(in.stdout, "%s\n", body)
	return nil
}

func (in *instance) log(_ context.Context, args []string) error {
	n := 40
	if len(args) == 1 {
		var err error
		if n, err = strconv.Atoi(args[0]); err != nil || n < 1 {
			return usageErr(fmt.Sprintf("N %q must be a positive integer", args[0]), "jt log demo 200")
		}
	}
	data, err := os.ReadFile(in.logPath())
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no log yet at %s", in.logPath())
	}
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(data), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	fmt.Fprint(in.stdout, strings.Join(lines[max(0, len(lines)-n):], ""))
	return nil
}
