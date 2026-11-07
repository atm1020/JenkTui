---
name: reference-repos
description: Checkout and consult Charm Crush and gh-dash as in-repo reference sources. Use when implementing configuration, directory management, XDG paths, cross-platform support, or other Bubble Tea TUI patterns, or when the user mentions crush, gh-dash, or reference repositories.
---

# Reference repositories

Consult these Bubble Tea apps instead of inventing config/directory patterns.
Their checkouts live as hidden folders at the **JenkTui repo root**, never under
a developer home path.

| Priority | Source | Checkout | Start here |
|---|---|---|---|
| 1 | Charm Crush (`https://github.com/charmbracelet/crush.git`) | `.crush` | `.crush/internal/config/load.go` |
| 2 | gh-dash (`https://github.com/dlvhdr/gh-dash.git`) | `.ghdash` | `.ghdash/internal/config/parser.go` |

Crush is the deeper XDG/config/data-dir reference. gh-dash is the simpler YAML
config reference. Prefer Crush when they disagree.

## Ensure checkouts

1. Resolve the JenkTui git root (`git rev-parse --show-toplevel`). All paths
   below are relative to that root.
2. For each repo, if the checkout is missing or is not a git directory, clone
   it. Do not clone into a non-empty non-git folder.

```bash
root="$(git rev-parse --show-toplevel)"

if [ ! -d "$root/.crush/.git" ]; then
  git clone --depth 1 https://github.com/charmbracelet/crush.git "$root/.crush"
fi

if [ ! -d "$root/.ghdash/.git" ]; then
  git clone --depth 1 https://github.com/dlvhdr/gh-dash.git "$root/.ghdash"
fi
```

3. Done when both `$root/.crush/.git` and `$root/.ghdash/.git` exist.
4. Do not `git pull` unless the user asks for a refresh or the file you need
   is absent. These are read-only references.
5. Never commit `.crush` or `.ghdash`. They are gitignored.

## Consult

Read the listed start file first, then follow local imports. Quote paths as
`.crush/...` and `.ghdash/...`, not machine-specific absolute homes.

Use them for:

- Config loading and default merging
- Config vs data vs cache directory separation
- Environment-variable overrides and platform path differences
- YAML/JSON config file shape

Do not copy Crush or gh-dash APIs into JenkTui. Steal the pattern, keep
JenkTui types and package layout.
