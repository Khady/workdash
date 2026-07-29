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

## Remote hosts

```toml
[[hosts]]
label = "asia"
ssh_target = "me@asia"
# tmux defaults to on; set tmux = false to opt out

[[hosts.repos]]
root = "/home/me/Code/ahrefs/monorepo"    # interpreted on the remote host, so ~ is not expanded
```

A host is collected in **one SSH round trip**. `PlanFor` lists the commands a
host needs, and `SSH` renders the whole plan into a single shell script piped
through `ssh host bash -c`. Measured against a host with 11 repos: one ssh exec
per git command took 2.72s with a warm `ControlMaster`; the batched form takes
0.083s. Nothing is installed on the far side — the remote requirements are bash,
git and tmux, exactly what workdash already assumed.

Two things that are easy to get wrong here, both now pinned by tests:

- **The script must be quoted for the remote shell.** ssh joins its command
  arguments with spaces and lets the remote login shell re-parse them, so an
  unquoted script makes `bash -c` take only the next word as its command. The
  symptom was subtle: every host silently lost its *first* section's output.
- **No `set -e`.** Each section is isolated so one unreadable repo cannot
  truncate the host.

Hosts are collected concurrently and degrade independently: an unreachable host
contributes one warning and the rest of the list still renders.

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

`needs` can also assert *absence* with a `!` prefix. Facet absence is a
first-class state, so "create the worktree" belongs only where there isn't one:

```toml
[[actions]]
id = "gwt"
needs = ["!worktree"]    # offered only when the branch has no worktree yet
```

`shell = true` runs the command through `bash -ic` so shell functions from your
rc resolve. That is the single place in the codebase where a command becomes a
quoted string, and it exists precisely so workdash can call `gwt`.

Only the documented placeholders — `{path}`, `{branch}`, `{repo}`, `{host}`,
`{session}`, `{pr_url}`, `{pr_number}` — are substituted. Any other brace span
is passed through untouched, so `awk '{print $1}'` and `find -name '*.{go,md}'`
work as actions. A *known* placeholder with no value for a given workspace makes
the action inapplicable rather than producing a command with a hole in it.

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

## Known gaps

Two things a code review surfaced that are deliberately left as-is for now:

- **`Pane` inside tmux creates a window the collector cannot attribute back.**
  `tmux new-window` runs in the *calling* client's session, but `collect.Tmux`
  lists sessions only — so a window opened for workspace B while you sit in
  session A never appears as a facet of B. Closing this needs a window-level
  collector (`tmux list-panes -a` with `#{pane_current_path}`), which is a model
  change: facets would gain windows alongside sessions. It belongs with the
  agent-status work, not here.
- **`{session}` means the first session** when a workspace has several. Tmux
  orders them alphabetically, so that is arguably an arbitrary choice — but
  dropping the action entirely when there are two sessions would leave you
  unable to attach at all, which is worse. Choosing between several sessions is
  a job for a frontend that can ask.

## Not in this slice

Remote hosts, PR collection, the daemon and its socket protocol, push-based
freshness, the TUI, and the browser popup. The `PR` facet and the `Host` field
exist so those slot in without a model change.
