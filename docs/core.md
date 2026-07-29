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

### Remote actions

`capture` on a remote workspace is one ssh. `pane` is two steps: the window is
created on the far side first, so the work survives losing the connection, and
then a *local* tmux window attaches to it. That is deliberate — remote agents end
up in the same local window list, and the same F-key switching, as local ones.
The local window is named `host/branch` so two machines with the same branch do
not collide.

How to reach a host interactively is `attach_argv`, defaulting to `["ssh", "-t"]`
with the target appended. mosh is worth configuring for a host you keep
long-running agents on, because it survives a suspended laptop:

```toml
attach_argv = ["mosh", "-p", "25300:25399"]
```

Anything sent over ssh is assembled as remote shell text with each argument
quoted individually — never as one quoted blob, which would make the far side try
to run a command whose name contains spaces.

## Pull requests

PRs are the only network source, so they are collected separately from git and
tmux and attached afterwards with `workspace.AttachPRs`. Two consequences:

- A failed or slow GitHub call degrades to *no pr facet*, never to no snapshot.
  The git and tmux facts are local and fast; nothing about them should wait on
  api.github.com.
- A pull request is a fact about a branch, not about a machine, so it attaches to
  every clone of that repo — the same PR shows on the local checkout and on the
  remote one.

The join key is `(repo full name, head branch)`. Full name comes from
`git remote get-url origin`, collected as another section of the host plan, so it
costs nothing extra.

One `gh api graphql` call runs per distinct `gh_account`:

```toml
[[repos]]
root = "/home/me/code/monorepo"
gh_account = "louis-roche_ahrefs"    # optional; omit to use gh's active account
```

The account's token is fetched with `gh auth token --user <account>` and passed
to `gh` in the environment, never in argv — `/proc/PID/cmdline` is world-readable
on Linux while `/proc/PID/environ` is not.

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

A selector is `[host:][repo:]branch`. Git forbids `:` in ref names and config
validation forbids it in labels, so the split is unambiguous even for branches
with slashes. A two-part selector means `repo:branch`, never `host:branch`, so
host qualification is spelled out in full. Ambiguity errors list candidates
fully qualified, so every candidate is a selector you can type back.

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

## Not yet built

PR collection, remote *actions* (launching a pane on `asia`), the daemon and its
socket protocol, push-based freshness, the TUI, and the browser popup. The `PR`
facet exists so PR collection slots in without a model change.
