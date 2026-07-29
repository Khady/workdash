# The workdash core

This is the from-scratch core described in `docs/superpowers/plans/2026-07-28-workdash-core-rewrite.md`.
It lives beside the existing tree; the old `workdash` binary is untouched and
still builds. The new entrypoint is `cmd/wd`.

## The one rule

**Workdash owns no authoritative state.** Worktrees live in git, sessions live
in tmux, pull requests live on GitHub. A `Workspace` is a projection over those,
so the cache is disposable and always reconstructible.

Two consequences worth keeping:

- **A missing facet is a normal state, not an error.** A branch whose tmux
  session died on reboot is a workspace with a worktree and no session. The
  action list adapts: it offers "create worktree" where there is none and
  "attach" where a session exists.
- **No privileged path.** Workdash's actions shell out to the same commands you
  type by hand. The `gwt` action *runs `gwt`* — it does not reimplement it, so a
  worktree you make yourself and one made through workdash are the same thing.
  Sessions are joined to worktrees by comparing paths, never by workdash
  bookkeeping, so a session you started with a name workdash would never pick
  attaches exactly the same way.

## Shape

```
collect ──records──> workspace.Index ──Snapshot──> action.Resolve ──> launch
 (git, tmux)          (pure merge)                  (facets+argv)     (2 verbs)
```

| Package | Responsibility |
|---|---|
| `internal/workspace` | `Workspace` = `(host, repo, branch)` + optional worktree/sessions/PR facets; `Index` merges records |
| `internal/collect` | `Runner` interface plus git and tmux collectors; `LocalRunner` shells out, fakes drive the tests |
| `internal/wdconf` | Config: repos and action definitions |
| `internal/action` | Which actions apply to a workspace, and their concrete argv |
| `internal/launch` | `Pane` (tmux window) and `Capture` (run and return output) |
| `cmd/wd` | CLI frontend: `ls`, `actions`, `run` |

## Config

`~/.config/workdash/wd.toml`, or `$WD_CONFIG`. See `docs/wd.example.toml`.

An action is `argv` + `needs` + `where`. Because `argv` is a list, nothing needs
quoting, and the old `{path:q}` filter has no reason to exist.

```toml
[[actions]]
id = "agent"
label = "launch claude"
needs = ["worktree"]     # applicability, derived — replaces contexts + requires
where = "pane"           # pane | capture — replaces run + scope + relaunch
argv = ["claude"]
```

`shell = true` runs the command through `bash -ic` so shell functions from your
rc resolve. That is the single place in the codebase where a command becomes a
quoted string, and it exists precisely so workdash can call `gwt`.

## Usage

```bash
wd ls                                  # every workspace and its facets
wd ls --json                           # same, for scripts
wd actions rewrite/core-facets         # what can I do here
wd run rewrite/core-facets agent       # launch claude in a tmux window
```

A selector is a branch name, or `repo:branch` when a branch exists in several
repos. Git forbids `:` in ref names, so the split is unambiguous even for
branches with slashes.

`wd run` with a `pane` action opens a tmux window when you are inside tmux, and
creates a detached session when you are not. Workdash never spawns a terminal
emulator and never hands a shell back to its caller — which is why the terminal
you use stopped being an architectural concern.

## Not in this slice

Remote hosts, PR collection, the daemon and its socket protocol, push-based
freshness, the TUI, and the browser popup. The `PR` facet and the `Host` field
exist so those slot in without a model change.
