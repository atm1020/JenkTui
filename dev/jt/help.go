package main

const helpUp = `jt up RUN

Build the app into $JT_STATE_ROOT/RUN/jenktui, write an isolated server.json
(id local) and config.json (poll 3s, notifications off), start it in a private
tmux server (jtv-RUN), and wait until the home splash shows the Jenkins version
or the startup server picker. Prints "ready RUN=... state=home|server-picker".
Refuses a RUN that is already running and any non-localhost Jenkins.
After a failed up, run down.

Examples:
  jt up demo
  JT_SECOND_SERVER=1 jt up servers
  JT_CONFIG_JSON='{"theme":"Nord","notification":{"desktop_enabled":false}}' jt up nord
  JT_DEVELOPMENT=1 jt up perf
`

const helpDoctor = `jt doctor RUN

Read-only health check: tmux session, app alive, pane runs this RUN's binary
(Linux), isolated HOME and log, no panic in jenktui.log, Jenkins auth, seed
data. Prints ok/FAIL/info lines; exits 1 on any FAIL.

Examples:
  jt doctor demo
`

const helpKeys = `jt keys RUN KEY...

Send tmux key names (Enter Escape Tab Up Down C-p C-b C-s C-t C-r C-c) or
single characters. Always wait for the next view before the next key.

Examples:
  jt keys demo C-p
  jt keys demo Down Down Enter
  jt keys demo F
`

const helpType = `jt type RUN TEXT

Send TEXT literally (no key-name parsing), e.g. into a search box. Text that
starts with "-" is fine.

Examples:
  jt type demo mobile-app
  jt type demo '--dry-run'
`

const helpWait = `jt wait RUN REGEX [SECONDS]

Poll the whole screen every 250ms until REGEX (Go RE2 syntax, POSIX classes
like [[:space:]] work) matches. Default timeout 20s. On timeout or if the app
exits, dumps the screen to stderr and exits 1.

Examples:
  jt wait demo 'Global Search'
  jt wait demo '│ #[0-9]+ │' 30
`

const helpCrumb = `jt crumb RUN REGEX [SECONDS]

Like wait, but matches only the last non-blank row: the footer breadcrumb and
toast. Use it to prove which page is active, since dialogs draw over the old
page's text. Join breadcrumb words with .* (nerd-font glyphs sit between).

Examples:
  jt crumb demo 'Frontend.*mobile-app'
  jt crumb demo 'Added to favorites' 10
`

const helpScreen = `jt screen RUN

Print the current screen as plain text.

Examples:
  jt screen demo
`

const helpSnap = `jt snap RUN NAME

Save the screen to the evidence folder as NAME.txt (plain) and NAME.ansi
(with colors; view with less -R). Prints the .txt path.

Examples:
  jt snap demo fav-toggle-before
`

const helpNote = `jt note RUN TEXT...

Append a timestamped line to the evidence transcript.

Examples:
  jt note demo 'baseline lastBuild=557'
`

const helpDB = `jt db RUN SQL

Run a read-only query against the instance's runs.db and print a column
table. The query and its output are copied into the transcript.

Examples:
  jt db demo 'select job_path, build_number, status from runs'
  jt db demo "select * from favorites where server_id='local'"
`

const helpJenkins = `jt jenkins RUN PATH

GET PATH from the local Jenkins with the configured credentials and print the
body followed by a newline. Brackets in tree= queries need no escaping.
Exits 1 on a non-2xx response.

Examples:
  jt jenkins demo '/job/DevTeam/job/Frontend/job/mobile-app/api/json?tree=lastBuild[number],inQueue'
  jt jenkins demo /job/DevTeam/job/Frontend/job/mobile-app/557/consoleText
`

const helpLog = `jt log RUN [N]

Print the last N lines of the instance's jenktui.log (default 40; has ANSI
colors). ERRO lines from router history eviction are normal.

Examples:
  jt log demo
  jt log demo 200
`

const helpDown = `jt down RUN

Save the last screen, send ctrl+c, kill only the private tmux server, copy
logs and a consistent runs.db backup into the evidence folder (never
server.json), and delete the RUN's state. Safe to repeat and safe after a
failed up.

Examples:
  jt down demo
`

const helpList = `jt list

List RUNs that still have state under $JT_STATE_ROOT (leftovers to down).

Examples:
  jt list
`
