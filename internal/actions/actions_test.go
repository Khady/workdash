package actions

import (
	"strings"
	"testing"

	"github.com/Khady/workdash/internal/config"
	"github.com/Khady/workdash/internal/model"
)

func TestRemoteInteractiveActionWrapsSSH(t *testing.T) {
	got := ConfiguredShellAction{ConfiguredCommandAction: ConfiguredCommandAction{
		Command: "git switch 'feature branch'", Cwd: "/srv/code/example-repo", Remote: true, RemoteInteractive: true, SSHTarget: "me@devbox",
	}}.ToShell()
	want := "ssh -A -t 'me@devbox' 'bash -lc '\\''cd -- '\\''\"'\"'\"'\"'\"'\"'/srv/code/example-repo'\"'\"'\"'\"'\"'\"'\\'' && git switch '\\''\"'\"'\"'\"'\"'\"'feature branch'\"'\"'\"'\"'\"'\"'\\'''\\'''"
	if !strings.HasPrefix(got, "ssh -A -t ") || !contains(got, "me@devbox") || !contains(got, "git switch") {
		t.Fatalf("unexpected command: %s; want shape including %s", got, want)
	}
}

func TestRemoteNonInteractiveActionForwardsAgent(t *testing.T) {
	got := ConfiguredInlineAction{ConfiguredCommandAction: ConfiguredCommandAction{
		Command: "git branch -D 'feature branch'", Cwd: "/srv/code/example-repo", Remote: true, SSHTarget: "me@devbox",
	}}.ToShell()
	if !strings.HasPrefix(got, "ssh -A -o BatchMode=yes ") {
		t.Fatalf("unexpected command: %s", got)
	}
}

func TestSerializeEmittedActionPrefixesInteractive(t *testing.T) {
	got := SerializeEmittedAction(ConfiguredShellAction{ConfiguredCommandAction: ConfiguredCommandAction{
		Command: "git switch 'main'", Cwd: "/repo", Relaunch: "always",
	}})
	if got != RelaunchAlwaysMarker+"\ncd -- '/repo' && git switch 'main'" {
		t.Fatalf("got %q", got)
	}
}

func TestTerminalLaunchCommandUsesAttachForLocalTmux(t *testing.T) {
	got := TerminalLaunchCommand(TmuxAction{Session: "s", InsideTmux: true})
	if got != "tmux attach-session -t 's'" {
		t.Fatalf("got %q", got)
	}
}

func TestTerminalLaunchCommandKeepsShellOpenAfterLocalCheckout(t *testing.T) {
	got := TerminalLaunchCommand(CheckoutAction{RepoRoot: "/repo", Branch: "feature/fix"})
	want := "cd -- '/repo' && git checkout 'feature/fix' && exec \"${SHELL:-bash}\""
	if got != want {
		t.Fatalf("got %q; want %q", got, want)
	}
}

func TestBranchCheckoutIncludesTerminalOptionWhenLauncherIsAvailable(t *testing.T) {
	options := ActionsForItem(model.WorkItem{
		Kind:     model.KindBranch,
		Title:    "feature/fix",
		Branch:   "feature/fix",
		RepoRoot: "/repo",
	}, nil, true, false)

	var found bool
	for _, option := range options {
		_, ok := option.Action.(TerminalLaunchAction)
		if option.Label == "checkout branch in terminal" && ok {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing terminal checkout option in %#v", options)
	}
}

func TestTmuxActionsIncludeEditorOptionsWhenPathIsKnown(t *testing.T) {
	options := ActionsForItem(model.WorkItem{
		Kind:      model.KindTmux,
		Title:     "session",
		Session:   "session",
		Path:      "/repo",
		SSHTarget: "me@devbox",
		Action:    TmuxAction{Session: "session", SSHTarget: "me@devbox"},
	}, testCommands(), false, true)

	editors := map[rune]ConfiguredInlineAction{}
	for _, option := range options {
		if action, ok := option.Action.(ConfiguredInlineAction); ok && strings.HasPrefix(action.ID, "open-") {
			editors[option.Shortcut] = action
		}
	}
	for _, shortcut := range []rune{'v', 'c', 'z'} {
		action, ok := editors[shortcut]
		if !ok {
			t.Fatalf("missing editor action for shortcut %q in %#v", shortcut, options)
		}
		if action.Detail != "/repo" || action.SSHTarget != "me@devbox" {
			t.Fatalf("unexpected editor action for %q: %#v", shortcut, action)
		}
	}
}

func TestTmuxActionsIncludeConfiguredShellWhenPathIsKnown(t *testing.T) {
	options := ActionsForItem(model.WorkItem{
		Kind:    model.KindTmux,
		Title:   "session",
		Session: "session",
		Path:    "/repo",
		Action:  TmuxAction{Session: "session"},
	}, testCommands(), true, true)

	var found bool
	for _, option := range options {
		action, ok := option.Action.(TerminalLaunchAction)
		if option.Shortcut != 's' || option.Label != "open shell in terminal" || !ok {
			continue
		}
		configured, ok := action.Action.(ConfiguredShellAction)
		if ok && configured.ID == "open-shell" && configured.Cwd == "/repo" && configured.Command == "bash" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing tmux configured shell action in %#v", options)
	}
}

func TestPRActionsUseConfiguredPathActions(t *testing.T) {
	options := ActionsForItem(model.WorkItem{
		Kind:      model.KindPR,
		Title:     "Fix thing",
		Path:      "/repo/wt",
		Branch:    "feature/fix-thing",
		RepoRoot:  "/repo",
		PRURL:     "https://github.com/example/repo/pull/123",
		SSHTarget: "me@devbox",
	}, testCommands(), false, true)

	seen := map[string]bool{}
	for _, option := range options {
		seen[option.Label] = true
	}
	if seen["open linked worktree"] {
		t.Fatalf("unexpected built-in linked worktree action in %#v", options)
	}
	for _, label := range []string{"open PR in browser", "open shell", "git status", "delete branch"} {
		if !seen[label] {
			t.Fatalf("missing %q in %#v", label, options)
		}
	}
}

func TestWorktreeActionsUseConfiguredShellInsteadOfBuiltInOpen(t *testing.T) {
	options := ActionsForItem(model.WorkItem{
		Kind:      model.KindWorktree,
		Title:     "repo-feature",
		Path:      "/repo/wt",
		Branch:    "feature/fix-thing",
		RepoRoot:  "/repo",
		SSHTarget: "me@devbox",
	}, testCommands(), false, true)

	seen := map[string]bool{}
	for _, option := range options {
		seen[option.Label] = true
	}
	if seen["open worktree"] {
		t.Fatalf("unexpected built-in open worktree action in %#v", options)
	}
	if seen["delete worktree"] {
		t.Fatalf("unexpected built-in delete worktree action in %#v", options)
	}
	if !seen["open shell"] {
		t.Fatalf("missing configured open shell action in %#v", options)
	}
}

func TestCheckedOutBranchActionsUseConfiguredPathActions(t *testing.T) {
	options := ActionsForItem(model.WorkItem{
		Kind:      model.KindBranch,
		Title:     "feature/fix-thing",
		Path:      "/repo/wt",
		Branch:    "feature/fix-thing",
		RepoRoot:  "/repo",
		SSHTarget: "me@devbox",
	}, testCommands(), false, true)

	seen := map[string]bool{}
	for _, option := range options {
		seen[option.Label] = true
	}
	if seen["open checked out worktree"] {
		t.Fatalf("unexpected built-in checked out worktree action in %#v", options)
	}
	if !seen["open shell"] || !seen["git status"] || !seen["delete branch"] {
		t.Fatalf("missing configured branch actions in %#v", options)
	}
}

func testCommands() []config.CommandConfig {
	return []config.CommandConfig{
		{ID: "open-pr", Shortcut: 'p', Label: "open PR in browser", Detail: "{pr_url}", Command: "xdg-open {pr_url:q}", Run: "background", Scope: "both", Contexts: []string{"pr", "worktree", "branch", "tmux"}, Requires: []string{"pr_url"}},
		{ID: "open-shell", Shortcut: 's', Label: "open shell", Detail: "{path}", Command: "bash", Cwd: "{path}", Run: "shell", Scope: "both", Contexts: []string{"pr", "worktree", "branch", "tmux"}, Requires: []string{"path"}, Remote: true, RemoteInteractive: true, Relaunch: "always"},
		{ID: "git-status", Shortcut: 'g', Label: "git status", Detail: "{repo_root}", Command: "git status --short --branch", Cwd: "{repo_root}", Run: "shell", Scope: "both", Contexts: []string{"pr", "worktree", "branch"}, Requires: []string{"repo_root"}, Remote: true},
		{ID: "delete-branch", Shortcut: 'x', Label: "delete branch", Detail: "{branch}", Command: "git branch -D {branch:q}", Cwd: "{repo_root}", Run: "inline", Scope: "both", Contexts: []string{"pr", "worktree", "branch"}, Requires: []string{"repo_root", "branch"}, Remote: true},
		{ID: "delete-session", Shortcut: 'x', Label: "delete session path", Detail: "{path}", Command: "rm -rf -- {path:q}", Run: "inline", Scope: "both", Contexts: []string{"tmux"}, Requires: []string{"path"}, Remote: true},
		{ID: "open-vscode-remote", Shortcut: 'v', Label: "open in vscode", Detail: "{path}", Command: "code --remote {remote_vscode:q} {path:q}", Run: "background", Scope: "remote", Contexts: []string{"pr", "worktree", "tmux"}, Requires: []string{"path", "ssh_target"}},
		{ID: "open-cursor-remote", Shortcut: 'c', Label: "open in cursor", Detail: "{path}", Command: "cursor --remote {remote_vscode:q} {path:q}", Run: "background", Scope: "remote", Contexts: []string{"pr", "worktree", "tmux"}, Requires: []string{"path", "ssh_target"}},
		{ID: "open-zed-remote", Shortcut: 'z', Label: "open in zed", Detail: "{path}", Command: "zed {remote_ssh_url:q}", Run: "background", Scope: "remote", Contexts: []string{"pr", "worktree", "tmux"}, Requires: []string{"path", "ssh_target"}},
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || contains(s[1:], sub) || s[:len(sub)] == sub))
}
