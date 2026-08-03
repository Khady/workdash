# Workdash

Terminal dashboard for GitHub pull requests, git worktrees, local branches and
tmux sessions, across this machine and configured SSH hosts.

```bash
make install-core     # wd (CLI) and wdt (TUI) into ~/.local/bin
make install-firefox  # the Firefox native-messaging host
```

Then point `~/.config/workdash/wd.toml` at your repos and hosts.

## Docs

- **[docs/using-wd.md](docs/using-wd.md)** — how to drive it: the tmux popup, the
  three commands, selectors, writing actions, and the sharp edges.
- **[docs/core.md](docs/core.md)** — why it is built this way: the facet model, the
  verbs, one SSH round trip per host, what pull requests cost.
- **[docs/TODO.md](docs/TODO.md)** — what is not built yet, with reasons.

## Shape

```
collect ──records──> workspace.Index ──Snapshot──> action.Resolve ──> launch
 (git, tmux, gh)      (pure merge)                 (facets+argv)      (4 verbs)
```

A **workspace** is a branch on a repo on a host, plus whichever facets exist for
it: a worktree, tmux sessions, a pull request. Workdash stores nothing
authoritative — worktrees live in git, sessions in tmux, pull requests on GitHub —
so its cache is disposable and a missing facet is a normal state rather than an
error.

Frontends: `wd` (CLI, also what scripts and the fzf picker use), `wdt` (TUI), and
`wd-host` (Firefox). All three share one core, one snapshot and one dispatch path.

## Requirements

`git` and `tmux` locally; the same plus `gh` on any configured remote host, which
is collected in a single SSH round trip with nothing installed there. `fzf` if you
use the `wd-pick` picker.
