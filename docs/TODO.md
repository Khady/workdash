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

## Smaller gaps

- **`prunable` worktrees.** The old `PathMissing` flagged a worktree whose
  directory is gone while git still lists it. `git worktree list --porcelain`
  reports `prunable`; a two-line parser addition. The tmux half of the old field
  is already covered by orphan sessions.
- **`TmuxWindowNames`.** The old detail pane listed them. Comes free with the
  window collector.
- **`{session}` picks the first session** when a workspace has several. Choosing
  belongs in a frontend that can ask.
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
