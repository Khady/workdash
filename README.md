# Workdash

Terminal dashboard for GitHub PRs, git worktrees, local branches, and tmux
sessions across your machine and configured SSH hosts.

## Install

```bash
go build ./cmd/workdash
```

Put the resulting `workdash` binary on your `PATH`, then load the shell
wrapper:

```bash
source shell/workdash.sh
workdash
```

The wrapper is the intended entrypoint because it can apply `cd`, tmux attach,
remote shell, and other emitted actions in your current shell after the TUI
exits.

## Config

Workdash reads `~/.config/workdash/config.toml`.

```toml
tmux = true
config_editor = "nvim"
terminal_launcher = "gnome-terminal --tab -- bash -lc {command}"
terminal_launcher_default = true

[[repos]]
root = "/home/me/code/example-repo"
gh_account = "your-gh-cli-account"

[[hosts]]
label = "devbox"
ssh_target = "me@devbox"
tmux = true

[[hosts.repos]]
root = "/srv/code/example-repo"
gh_account = "your-gh-cli-account"

[[commands]]
id = "open-shell"
shortcut = "s"
label = "open shell"
detail = "{path}"
command = "bash"
cwd = "{path}"
run = "terminal"
contexts = ["pr", "worktree", "branch", "tmux"]
requires = ["path"]
remote = true
remote_interactive = true
```

See [examples/config.toml](examples/config.toml)
for a complete example.

### Top-Level Keys

`tmux`

- Type: boolean
- Default: `true`
- Controls whether local tmux sessions are shown.

`config_editor`

- Type: string
- Optional.
- Used by `F2` to open the config file.
- Must be non-empty when set.
- If omitted, Workdash falls back to `$EDITOR`.

`terminal_launcher`

- Type: string
- Optional.
- Command template used to launch shell actions in a new terminal window or tab.
- Must contain a `{command}` placeholder.
- Example: `gnome-terminal --tab -- bash -lc {command}`

`terminal_launcher_default`

- Type: boolean
- Default: `true`
- When `true`, actions that can either run inline or in a terminal prefer the configured `terminal_launcher`.
- When `false`, those actions default to the current shell unless you pick the terminal variant explicitly in the action menu.

### Commands

Configure action-menu commands with repeated `[[commands]]` tables. Workdash
does not install command defaults; add entries for shell commands, editor
launchers, or project-specific scripts in your config.

`id`

- Required string.
- Stable identifier for the command.

`shortcut`

- Optional single-character string.
- Adds a direct action-menu shortcut.

`label`

- Required string.
- Display label in the action menu.

`detail`

- Optional string template.
- Shown below the label.

`command`

- Required string template.
- Shell command to run.

`cwd`

- Optional string template.
- When set, Workdash runs `cd -- <cwd> && <command>`.

`run`

- Type: string.
- Default: `shell`.
- Values: `shell`, `terminal`, `inline`, `background`.
- `shell` emits the command to the wrapper, with terminal alternatives when `terminal_launcher` is configured.
- `terminal` always uses `terminal_launcher`.
- `inline` runs in the Workdash process and waits for completion.
- `background` starts the command from Workdash and does not wait.

`scope`

- Type: string.
- Default: `both`.
- Values: `both`, `local`, `remote`.

`contexts`

- Optional string array.
- Values: `pr`, `worktree`, `branch`, `tmux`.
- Empty means all item kinds.

`requires`

- Optional string array.
- Suppresses the command unless each named placeholder has a value.

`remote`

- Type: boolean.
- Default: `false`.
- When `true`, commands on remote items are wrapped in SSH.

`remote_interactive`

- Type: boolean.
- Default: `false`.
- Uses an interactive SSH session for remote commands.

`relaunch`

- Type: string.
- Default: `never`.
- Values: `never`, `always`.
- Use `always` for emitted shell commands that should relaunch Workdash after completion.

Supported templates include `{path}`, `{path:q}`, `{repo_root}`, `{branch}`,
`{session}`, `{pr_url}`, `{ssh_target}`, `{remote_vscode}`, and
`{remote_ssh_url}`. The `:q` suffix shell-quotes the value.

### Local Repos

Configure local repos with repeated `[[repos]]` tables.

`root`

- Required, non-empty string.
- Local filesystem path to the repo root.
- `~` is expanded.
- Missing local repo roots are skipped with a warning.

`label`

- Optional string.
- Display name for the repo.
- Defaults to the basename of `root`.

`gh_account`

- Optional string.
- Passed to `gh auth token --user <gh_account>` before account-scoped GitHub CLI calls for that host.
- Also used as the PR author for per-repo `gh pr list --author <gh_account>` calls.
- This controls which authenticated GitHub CLI account `@me` refers to when Workdash runs `gh api graphql`.
- If omitted, Workdash uses the default authenticated `gh` account on that machine or remote host.

### Remote Hosts

Configure remote hosts with repeated `[[hosts]]` tables.

`label`

- Required, non-empty string.
- Must be unique across hosts.
- Cannot be `local`.

`ssh_target`

- Required, non-empty string.
- Must be a single SSH destination argument such as `me@devbox`.
- Cannot contain whitespace.

`tmux`

- Required boolean on every remote host.
- Controls whether tmux sessions are collected from that host.

Each remote host may also define repeated `[[hosts.repos]]` tables with the
same fields as local `[[repos]]`, except `root` is interpreted on the remote
host and is not `~`-expanded by Workdash.

At least one of these must be true for every remote host:

- `tmux = true`
- At least one `[[hosts.repos]]` entry is configured

### PR Loading

Workdash uses a single account-scoped GitHub CLI flow:

- `gh api graphql` fetches recent authored PRs across states, including each PR's repository and `headRefName`.
- That shared PR dataset powers both the dedicated `PR` entries and PR badges on matching branches and worktrees.

Workdash launches one PR fetch per distinct `gh_account` across the configured
hosts. If no `gh_account` is configured for a host, it makes one call using
that host’s default `gh` authentication context.

## Usage

Type to filter results. Press `Enter` to open the action menu for the selected
item, `Ctrl+R` to refresh, `Ctrl+B` to toggle details, and `F1` for the full
shortcut list. PR entries are loaded from the shared account-scoped GraphQL PR
dataset.

## Logs

Workdash writes an append-only debug log by default:

```bash
tail -f "${XDG_STATE_HOME:-$HOME/.local/state}/workdash/workdash.log"
```

The log includes command start/end entries, SSH targets, exit codes, stdout, and
stderr. It is best-effort: logging failures are ignored so the dashboard can
still load. Logs rotate at 50 MiB with one retained backup. `GH_TOKEN` values
and GitHub token-shaped strings are redacted before writing.

## License

Workdash is licensed under the GNU Affero General Public License version 3 or
later. See [LICENSE](LICENSE).
