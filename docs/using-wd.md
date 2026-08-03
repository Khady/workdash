# Using `wd`

How the rewritten core is meant to be driven day to day. For *why* it is shaped
this way — the facet model, the two verbs, what got deleted and what that bought
— see [core.md](core.md).

## What is installed

| Path | What it is |
|---|---|
| `~/.local/bin/wd` | the binary |
| `~/.local/bin/wdt` | the TUI — grouped, coloured, recency-ordered |
| `~/.local/bin/wd-pick` | fzf picker: workspace → action → run |
| `~/.config/workdash/wd.toml` | config for `wd`, generated from `config.toml` |
| `~/.tmux.conf` | `bind w` → `wdt`, `bind W` → `wd-pick`, `bind C-a send-prefix` |

`config.toml` is untouched, so the old `workdash` TUI keeps working unchanged.
The two configs are independent; nothing is shared.

Rebuild after pulling changes — **both**, since they share the collector and
installing one leaves you running two different versions of it:

```bash
make install-core
```

## The normal way in

**`prefix + w`** opens the TUI in a tmux popup: type to filter, `Enter` for the
action menu, `Ctrl+R` to reload, `?` for help, `q` to quit.

**`Tab`** / **`Shift+Tab`** cycle the view, as the old dashboard did: **All →
Tmux → Worktrees → PRs → Branches**. Those are facet filters — "Branches" means
the ones checked out nowhere — and the active view is shown top-left next to the
filter, so a short list is never mysterious.

**`prefix + W`** opens the fzf picker instead, if you prefer it.

The popup is `wd-pick`, which is three `wd` calls piped through `fzf` — worth
knowing because it means nothing about the picker is privileged. Anything it does,
you can do from a shell or a script.

## The three commands

```bash
wd ls                                    # every workspace and its facets
wd ls --json                             # the same, for scripts
wd actions asia:monorepo:louis/x         # what can I do on this workspace
wd run asia:monorepo:louis/x shell       # do it
wd refresh                               # refetch pull requests now
wd ls --needs session                    # the same filters as the TUI's Tab
wd ls --needs worktree --needs pr        # combine them
wd ls --needs '!worktree'                # branches checked out nowhere
```

`wd ls` output is one workspace per line:

```
asia   monorepo  louis/ocaml-scripting  worktree,session  [gwt-monorepo-louis-ocaml-scripting: 2 windows]
asia   dune      louis/dune-watch       pr
local  gmi-status main                  worktree
asia   (unlinked) claude rc monorepo     session
```

The columns are host, repo, branch, facets, and session detail. **Facets are the
point**: they say what currently exists for that branch, and the available
actions follow from them. A branch with no worktree gets "create worktree"; one
with a session gets "attach"; one with a PR gets "open PR". `(unlinked)` is a tmux
session whose path matches no worktree workdash knows about — reported rather than
hidden.

## Selectors

`[host:][repo:]branch`, shortest unambiguous form:

```bash
wd actions louis/dune-watch              # if that branch is unique
wd actions dune:louis/dune-watch         # if two repos have it
wd actions asia:dune:louis/dune-watch    # if two hosts do
```

A two-part selector is always `repo:branch`, never `host:branch`. Git forbids `:`
in ref names and config validation forbids it in labels, so the split is
unambiguous even for branches with slashes. When a selector is ambiguous the error
lists candidates fully qualified, so each one is a selector you can paste back:

```
wd: "monorepo:develop" is ambiguous: asia:monorepo:develop, local:monorepo:develop
```

## Where things run

An action's `where` is `pane`, `capture`, `attach` or `background`.

- **`pane`** goes into a tmux window: shells, editors, agents. Inside tmux you get
  a new window; outside it, a detached session and the command to attach. If the
  workspace already has a session, the window goes *there* rather than beside it.
- **`capture`** runs and prints: `git status`, `xdg-open`.
- **`attach`** connects to the session that already exists and creates nothing.
  It takes no `argv` and implies `needs = ["session"]`. Locally it switches the
  client; for a remote workspace it opens a local window connected to the remote
  session. This is the verb for "take me to the work that is already running".

- **`background`** starts the command detached and does not wait: GUI programs.
  A browser or `emacs` under `capture` would make the picker wait until you closed
  it. Remote workspaces reject this verb and tell you to add `run_on = "local"`,
  since the programs that need it are desktop programs on the machine in front of
  you.

- **`background`** starts the command detached and does not wait: GUI programs.
  A browser or GUI `emacs` under `capture` makes the caller wait until you close
  it. A remote workspace rejects this verb and tells you to add `run_on = "local"`,
  since the programs needing it are desktop programs on the machine you are at.

`run_on = "local"` runs the command on this machine even when the workspace is
remote. That is different from `hosts`, which says which *workspaces* an action
applies to: opening a pull request in a browser is about a remote workspace but
belongs on your laptop.

For a **remote** workspace, `pane` creates the window on asia first — so the work
survives losing the connection — then attaches from a local tmux window named
`asia/branch`. That is deliberate: remote agents land in the same window list, and
the same `F1`–`F10` switching, as local ones.

`wd` never spawns a terminal emulator and never hands a shell back to its caller.

## Adding an action

```toml
[[actions]]
id = "test"
label = "run tests"
needs = ["worktree"]        # facets required; "!worktree" means it must be absent
hosts = ["asia"]            # optional; omit for every host
where = "pane"              # pane | capture | attach | background
run_on = "local"            # optional; run here even for a remote workspace
argv = ["dune", "test"]     # a list, so nothing needs quoting
cwd = "{path}"              # optional; omitted means the worktree if there is one
shell = true                # optional; run via your login shell so rc functions resolve
```

Placeholders: `{path}` `{branch}` `{repo}` `{host}` `{session}` `{pr_url}`
`{pr_number}`. Anything else in braces is left alone, so `awk '{print $1}'` works
as an argument. A placeholder that has no value for a workspace makes the action
unavailable there rather than producing a broken command.

Two settings that exist for real reasons:

- **`hosts`** keeps a GUI editor off a remote workspace, where it would open a
  path that does not exist locally, and keeps a command installed on only one
  machine from being offered on the other. `gwt`, `gwt-delete` and `agents` are
  scoped to `asia` because they are not installed here.
- **`attach_argv`** on an action overrides how a remote pane is reached. The host
  default is `["ssh", "-A", "-t"]` so agent forwarding and the `LocalForward`
  entries in `~/.ssh/config` keep working. mosh is opted into per action — it
  survives a suspended laptop, which is worth it for an editor, but it cannot
  forward the agent or ports, so shells must not use it. Note mosh needs `--`
  before the target: `["mosh", "-p", "25300:25399", "--"]`.
- **`shell = true`** runs through `bash -ic`, so shell functions from your rc
  resolve. This is how `wd` calls `gwt` instead of reimplementing it — a worktree
  created here and one created by typing `gwt` yourself are the same thing.

## Speed, and when it is slow

Git and tmux facts are local and batched: one ssh round trip per remote host,
~0.08s for asia's eleven repos.

Pull requests are the only network source, and they are never allowed to make you
wait. The cache under `$XDG_CACHE_HOME/workdash` has two limits:

```toml
pr_refresh_after = "15m"   # soft: past this, render the cache and refresh behind you
pr_cache_ttl = "24h"       # hard: past this, block and refetch
```

So a cold run costs ~3s once, and every run after that is ~0.15s — including runs
past the soft limit, which render instantly and kick off a detached refresh. Only
a cache older than the hard limit blocks. `wd refresh` forces a refetch when you
have just opened a pull request.

Rate limits are not a concern: the GraphQL budget is 5000 points/hour and a query
costs about one, so even refreshing every 15 minutes across two accounts uses well
under 1%.

That cache is disposable — deleting the directory is always safe, a corrupt entry
counts as a miss, and a failed fetch is never stored.

If a host is unreachable you get one warning naming it and the rest of the list
still renders. `wd ls` exits non-zero only when *every* source failed, so a script
can tell "nothing to show" from "everything is broken".

## Sharp edges

- **`gwt-delete` runs immediately, with no confirmation.** The old TUI prompted;
  the CLI has no prompt layer yet. It is in the config because you had it, but it
  is the one action to be careful with in a popup.
- **No remote GUI editors.** `code --remote` and `zed ssh://` from `config.toml`
  did not carry over: they need an ssh target inside the action language. Terminal
  editors are unaffected, since a remote `pane` runs where the code is.
- **`xdg-open` guesses from its environment.** Inside a tmux popup that is the
  tmux *server's* environment, not your session's, which is why the browser action
  names `firefox` explicitly instead.
- **A window opened inside your current session is invisible to `wd ls`.** The
  collector lists sessions, not windows, so a window created for workspace B while
  you sit in session A is not reported as a facet of B.

## Troubleshooting

```bash
wd ls                     # warnings go to stderr, one line per failing source
rm -rf ~/.cache/workdash  # force a fresh pull request fetch
```

A workspace missing from `wd ls` usually means its repo is not in `wd.toml` —
`wd` collects only what is configured, and unlike the old TUI it does not inject
the repo you happen to be standing in.
