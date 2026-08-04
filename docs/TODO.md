# Workdash core — outstanding work

Everything deferred so far, with why it was deferred and what it costs. Ordered
by what I would do next, not by size.

## Next

### Confirmation for destructive actions
`gwt-delete` runs immediately — one click in the Firefox popup, one keypress in
the TUI, no prompt. The old TUI prompted; the new action language has no
confirmation concept at all. This is the sharpest edge in the system.

Two spellings, and I would take the first:
- `confirm = true` on an action, honoured by every frontend.
- `browser = false`, keeping destructive actions out of the popup only.

### Window collector, then agent status
`collect.Tmux` lists *sessions*, so workdash knows a worktree has a session with
three windows and nothing about what is in them. `tmux list-panes -a` adds, per
window: `pane_current_path` (the join key, same derived-join principle as
sessions), `pane_current_command`, `window_activity`, `window_bell_flag`.

Cost: one more command inside the existing batched ssh, per host — not per
worktree. Returns instantly.

What it unlocks:
1. **Closes a known gap.** A window `Pane` creates inside your current session is
   invisible to `wd ls` today, because only sessions are listed.
2. **Agent status** — "which of these is waiting for me", which is the question a
   dashboard over a dozen agent sessions actually has. `pane_current_command` and
   `window_activity` are solid signals; `window_bell_flag` is the clean "wants
   you" signal *if* Claude Code rings the bell, which is unverified.
3. **Identifies mosh/ssh view windows.** A local window carrying `@wd_host` and
   `@wd_session` can be shown as "view of asia:gwt-… over mosh" instead of you
   reading window names.

Explicitly **not** included: reading pane contents to detect an approval prompt.
It is the only way to distinguish "waiting for your answer" from "idle", but it is
heuristic and was deliberately left out.

Model change it needs: the session facet gains windows, which ripples into the
TUI columns and `wd ls --json`.

### A local backend, chosen by config
Switching the local terminal from tmux to kitty currently means editing
`internal/launch`. It should mean one config key. The old dashboard had
`terminal_launcher = "terminal {command}"`, which is the right instinct and too
thin a contract — see below.

The local half of a remote `pane` is not one command. For one action on asia:

```
tmux list-clients -F '#{client_activity} #{client_session} #{client_tty}'   # outside tmux only
tmux new-window -t=<session>: -P -F '#{window_id}' -n 'asia/<branch>' -c '' -- <attach argv>
tmux set-option -w -t @57 @wd_host asia                                    # and @wd_session, @wd_transport
tmux switch-client -c /dev/pts/8 -t=<session>                              # only if you are elsewhere
```

Four concerns hide in there, each from a real bug: where the user is looking, which
session the window belongs to, the cwd (set locally, deliberately empty for a remote
view), and the metadata stamp the window collector is meant to read.

kitty can express all four — checked against `kitten @ launch --help`, not assumed:

| tmux | kitty |
|---|---|
| `new-window -n NAME` | `launch --type=tab --tab-title NAME` |
| `-c CWD` | `--cwd CWD` |
| `-t=SESSION:` | `--match` on an existing tab |
| `set-option -w @wd_host asia` | `--var wd_host=asia` |
| `-P -F '#{window_id}'` | `launch` prints the window id |
| `list-clients` | `kitten @ ls`, as JSON |
| `switch-client` | `kitten @ focus-tab` |

**The interface has to cover launch, list and attach together**, and the reason is
not the dashboard. `resolved.Session` is what makes `Pane` put a window in the place
a workspace already has instead of making a second one — the duplicate-session bug,
already fixed once on the remote side. A backend that cannot list cannot answer
"is there somewhere for this to go", so "open shell" then "open vim" on one branch
would give two tab groups instead of two tabs. Worse, workdash would have to start
remembering what it launched, which is exactly the state ownership the facet model
avoids: a session `gwt` made by hand has to stay indistinguishable from one workdash
made.

What it is *not* about, measured rather than assumed: 0 of 9 local rows carry a
session facet today, the only local tmux session is `wdt` at `/home/louis` (correctly
an orphan), and both rows in the snapshot with a session facet are on asia. So the
visible loss in `wd ls` from a blind local backend is nil. Remote work is
tmux-over-ssh whatever the local backend is, so the case where sessions currently
pull weight is untouched by this.

Shape, if it gets built: a `LocalBackend` interface with `local = "tmux"` (default)
or `"kitty"`, both in code. A `local = ["command", "wd-launch-x"]` escape hatch is
worth having for experiments, invoked as `launcher NAME -- COMMAND...` with `WD_CWD`,
`WD_GROUP`, `WD_HOST`, `WD_SESSION`, `WD_TRANSPORT` in the environment — env rather
than more positional arguments so adding a field later does not break scripts that
exist. Its stdout becomes the report workdash relays, which beats today's message
guessing at what tmux did. But a script backend is launch-only: a collector returns
structured data, and those windows would not appear in `wd ls`.

Worth doing even without switching terminals, because writing the tmux backend
against the interface is what proves the interface is real. Worth *not* doing
speculatively past that point: `kitten ssh` turned out to buy nothing in this
topology — asia already has the `xterm-kitty` terminfo, and remote tmux filters
kitty's integration features anyway — so kitty is not currently on the way in.

## Smaller gaps

- **`prunable` worktrees.** The old `PathMissing` flagged a worktree whose
  directory is gone while git still lists it. `git worktree list --porcelain`
  reports `prunable`; a two-line parser addition. The tmux half of the old field
  is already covered by orphan sessions.
- **`TmuxWindowNames`.** The old detail pane listed them. Comes free with the
  window collector.
- **`{session}` picks the first session** when a workspace has several. Choosing
  belongs in a frontend that can ask.
- **Views still get a "<base>-1" name on a fresh session.** `Pane` creating a
  session does not ask for the new window's id, so there is no window to focus and
  the view is made by the attach itself — the one path that lets tmux auto-name it.
  Adding `-P -F '#{window_id}'` to that create would route it through
  `prepareView` like every other view, and name it `<base>~@id`.
- **The local tmux server inherits whoever created it.** Remote commands now go
  through a login shell so the server workdash creates on a host has a usable
  PATH, and a tmux server keeps its birth PATH for life. Locally the same hole
  exists in principle — the Firefox host is started by Firefox, so a server
  created from the popup gets Firefox's environment. Measured on this machine
  Firefox's PATH is the full one, so there is nothing to fix yet; a desktop
  launched without a shell-derived environment would need the same treatment.
- **`emacs`/`code` on a *local* workspace vs `emacsclient`.** GUI emacs under
  `background` is fine now, but `emacsclient -c -a ''` reuses a running instance
  and is probably what you want.

## Deferred deliberately

- **Daemon and push-based freshness.** The disk cache plus batched collection made
  the daemon unnecessary for both frontends and the browser. Revisit only if
  evented freshness is wanted — inotify locally, tmux control mode remotely.
- **Dirty-worktree facet.** `git status --porcelain` per *worktree* is 42 commands
  on asia, unlike the one-per-host tmux calls. Measure before building.
- **A second remote host.** Nothing blocks it; `scope` replaced named hosts partly
  so this needs no action edits.
