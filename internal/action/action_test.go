package action

import (
	"strings"
	"testing"

	"github.com/Khady/workdash/internal/wdconf"
	"github.com/Khady/workdash/internal/workspace"
)

func ws() workspace.Workspace {
	return workspace.Workspace{
		ID:        workspace.ID{Host: "local", Repo: "/repo", Branch: "topic"},
		RepoLabel: "repo",
		Worktree:  &workspace.Worktree{Path: "/wt/topic"},
	}
}

func TestResolveSkipsActionsMissingFacets(t *testing.T) {
	defs := []wdconf.Action{
		{ID: "attach", Needs: []string{workspace.FacetSession}, Where: "pane", Argv: []string{"tmux", "attach"}, Cwd: "{path}"},
		{ID: "agent", Needs: []string{workspace.FacetWorktree}, Where: "pane", Argv: []string{"claude"}, Cwd: "{path}"},
	}
	got := Resolve(ws(), defs)
	if len(got) != 1 || got[0].ID != "agent" {
		t.Fatalf("resolved = %+v, want only agent", got)
	}
}

func TestResolveRendersPlaceholders(t *testing.T) {
	defs := []wdconf.Action{{ID: "gwt", Where: "pane", Shell: true,
		Argv: []string{"gwt", "{branch}"}, Cwd: "{repo}"}}
	got := Resolve(ws(), defs)
	if len(got) != 1 {
		t.Fatalf("resolved = %+v, want 1", got)
	}
	if strings.Join(got[0].Argv, " ") != "gwt topic" {
		t.Fatalf("argv = %v, want [gwt topic]", got[0].Argv)
	}
	if got[0].Cwd != "/repo" {
		t.Fatalf("cwd = %q, want /repo", got[0].Cwd)
	}
	if !got[0].Shell {
		t.Fatal("Shell flag should carry through")
	}
}

func TestResolveSkipsUnresolvablePlaceholders(t *testing.T) {
	defs := []wdconf.Action{{ID: "open-pr", Where: "capture", Argv: []string{"xdg-open", "{pr_url}"}, Cwd: "{path}"}}
	if got := Resolve(ws(), defs); len(got) != 0 {
		t.Fatalf("resolved = %+v, want none", got)
	}
}

func TestResolveSubstitutesInsideLargerArgument(t *testing.T) {
	defs := []wdconf.Action{{ID: "note", Where: "capture",
		Argv: []string{"echo", "branch={branch} at {path}"}, Cwd: "{path}"}}
	got := Resolve(ws(), defs)
	if len(got) != 1 {
		t.Fatalf("resolved = %+v, want 1", got)
	}
	if got[0].Argv[1] != "branch=topic at /wt/topic" {
		t.Fatalf("argv[1] = %q", got[0].Argv[1])
	}
}

func TestResolveNamesTargetAfterBranchNotAction(t *testing.T) {
	defs := []wdconf.Action{{ID: "agent", Where: "pane", Argv: []string{"claude"}, Cwd: "{path}"}}
	got := Resolve(ws(), defs)
	if len(got) != 1 || got[0].Name != "topic" {
		t.Fatalf("name = %q, want topic", got[0].Name)
	}
}

func TestResolveSanitizesNameForTmux(t *testing.T) {
	target := ws()
	target.ID.Branch = "feature/v1.2:rc"
	defs := []wdconf.Action{{ID: "agent", Where: "pane", Argv: []string{"claude"}, Cwd: "{path}"}}
	got := Resolve(target, defs)
	if got[0].Name != "feature/v1-2-rc" {
		t.Fatalf("name = %q, want feature/v1-2-rc", got[0].Name)
	}
}

func TestResolveCarriesHostFromWorkspace(t *testing.T) {
	target := ws()
	target.ID.Host = "asia"
	defs := []wdconf.Action{{ID: "shell", Where: "pane", Argv: []string{"bash"}, Cwd: "{path}"}}
	got := Resolve(target, defs)
	if len(got) != 1 || got[0].Host != "asia" {
		t.Fatalf("resolved = %+v, want host asia", got)
	}
}

func TestResolveKeepsNonPlaceholderBraceSpansLiteral(t *testing.T) {
	defs := []wdconf.Action{{ID: "awk", Where: "capture",
		Argv: []string{"awk", "{print $1}", "{path}/go.mod"}, Cwd: "{path}"}}
	got := Resolve(ws(), defs)
	if len(got) != 1 {
		t.Fatalf("resolved = %+v, want the awk action to survive", got)
	}
	if got[0].Argv[1] != "{print $1}" {
		t.Fatalf("argv[1] = %q, want the awk program untouched", got[0].Argv[1])
	}
	if got[0].Argv[2] != "/wt/topic/go.mod" {
		t.Fatalf("argv[2] = %q, want the placeholder still substituted", got[0].Argv[2])
	}
}

func TestResolveStillDropsKnownPlaceholderWithoutValue(t *testing.T) {
	defs := []wdconf.Action{{ID: "open-pr", Where: "capture", Argv: []string{"xdg-open", "{pr_url}"}, Cwd: "{path}"}}
	if got := Resolve(ws(), defs); len(got) != 0 {
		t.Fatalf("resolved = %+v, want none: {pr_url} is known but has no value here", got)
	}
}

func TestResolveHonoursNegatedFacetNeeds(t *testing.T) {
	defs := []wdconf.Action{{ID: "gwt", Needs: []string{"!" + workspace.FacetWorktree},
		Where: "pane", Argv: []string{"gwt", "{branch}"}, Cwd: "{repo}"}}
	if got := Resolve(ws(), defs); len(got) != 0 {
		t.Fatalf("resolved = %+v, want none: workspace already has a worktree", got)
	}
	bare := workspace.Workspace{ID: workspace.ID{Host: "local", Repo: "/repo", Branch: "topic"}}
	got := Resolve(bare, defs)
	if len(got) != 1 || got[0].ID != "gwt" {
		t.Fatalf("resolved = %+v, want gwt on a workspace with no worktree", got)
	}
}

// An action with no configured cwd must not inherit a requirement for a worktree.
// Opening a pull request in a browser needs no directory, and a PR-only workspace
// has no path to offer — dropping the action there made `open-pr` unreachable on
// exactly the workspaces it exists for.
func TestResolveDoesNotRequireAPathForActionsThatOmitCwd(t *testing.T) {
	prOnly := workspace.Workspace{
		ID:        workspace.ID{Host: "asia", Repo: "/srv/dune", Branch: "louis/dune-watch"},
		RepoLabel: "dune",
		PR:        &workspace.PR{Number: 10868, URL: "https://github.com/ocaml/dune/pull/10868"},
	}
	defs := []wdconf.Action{{ID: "open-pr", Needs: []string{workspace.FacetPR},
		Where: "capture", Argv: []string{"xdg-open", "{pr_url}"}}}
	got := Resolve(prOnly, defs)
	if len(got) != 1 {
		t.Fatalf("resolved = %+v, want open-pr to be available", got)
	}
	if got[0].Cwd != "" {
		t.Fatalf("cwd = %q, want empty when the workspace has no worktree", got[0].Cwd)
	}
	if got[0].Argv[1] != "https://github.com/ocaml/dune/pull/10868" {
		t.Fatalf("argv = %q", got[0].Argv)
	}
}

func TestResolveDefaultsCwdToTheWorktreeWhenThereIsOne(t *testing.T) {
	defs := []wdconf.Action{{ID: "shell", Where: "pane", Argv: []string{"bash"}}}
	got := Resolve(ws(), defs)
	if len(got) != 1 || got[0].Cwd != "/wt/topic" {
		t.Fatalf("resolved = %+v, want cwd /wt/topic", got)
	}
}

// An explicitly configured cwd is a requirement, so it still drops the action.
func TestResolveDropsActionsWhoseExplicitCwdCannotResolve(t *testing.T) {
	bare := workspace.Workspace{ID: workspace.ID{Host: "local", Repo: "/repo", Branch: "topic"}}
	defs := []wdconf.Action{{ID: "shell", Where: "pane", Argv: []string{"bash"}, Cwd: "{path}"}}
	if got := Resolve(bare, defs); len(got) != 0 {
		t.Fatalf("resolved = %+v, want none: {path} was asked for and is absent", got)
	}
}

// Some actions only make sense on one machine: a GUI editor opens a local path,
// so offering it on a remote workspace would launch it against a path that does
// not exist here. An empty host list means every host.
func TestResolveHonoursHostScoping(t *testing.T) {
	defs := []wdconf.Action{
		{ID: "code", Hosts: []string{"local"}, Needs: []string{workspace.FacetWorktree},
			Where: "capture", Argv: []string{"code", "{path}"}},
		{ID: "shell", Needs: []string{workspace.FacetWorktree}, Where: "pane", Argv: []string{"bash"}},
	}
	local := ws()
	if got := Resolve(local, defs); len(got) != 2 {
		t.Fatalf("resolved on local = %+v, want both", got)
	}
	remote := ws()
	remote.ID.Host = "asia"
	got := Resolve(remote, defs)
	if len(got) != 1 || got[0].ID != "shell" {
		t.Fatalf("resolved on asia = %+v, want only shell", got)
	}
}

func TestResolveAllowsAnActionScopedToARemoteHost(t *testing.T) {
	defs := []wdconf.Action{{ID: "vim", Hosts: []string{"asia"},
		Needs: []string{workspace.FacetWorktree}, Where: "pane", Argv: []string{"vim", "{path}"}}}
	remote := ws()
	remote.ID.Host = "asia"
	if got := Resolve(remote, defs); len(got) != 1 {
		t.Fatalf("resolved = %+v, want the asia-scoped action", got)
	}
	if got := Resolve(ws(), defs); len(got) != 0 {
		t.Fatalf("resolved on local = %+v, want none", got)
	}
}

// Execution needs to know the workspace already has a session, or it invents a
// name, creates a second session on the same worktree, and attaches to the empty
// one. Resolution knows the facts, so it carries them.
func TestResolveCarriesAnExistingSessionName(t *testing.T) {
	target := ws()
	target.Sessions = []workspace.Session{{Name: "gwt-monorepo-louis-x", Path: "/wt/topic"}}
	defs := []wdconf.Action{{ID: "shell", Where: "pane", Argv: []string{"bash"}}}
	got := Resolve(target, defs)
	if len(got) != 1 || got[0].Session != "gwt-monorepo-louis-x" {
		t.Fatalf("session = %q, want the existing session name", got[0].Session)
	}
}

func TestResolveLeavesSessionEmptyWhenThereIsNone(t *testing.T) {
	defs := []wdconf.Action{{ID: "shell", Where: "pane", Argv: []string{"bash"}}}
	got := Resolve(ws(), defs)
	if got[0].Session != "" {
		t.Fatalf("session = %q, want empty", got[0].Session)
	}
}

// hosts names machines, which is too specific: what a GUI editor actually needs
// is "the workspace is on this machine", and what vim-on-the-devbox needs is
// "the workspace is somewhere else". Naming asia in both makes adding a second
// remote host an edit of every action.
func TestResolveHonoursLocalScope(t *testing.T) {
	defs := []wdconf.Action{{ID: "code", Scope: wdconf.ScopeLocal,
		Needs: []string{workspace.FacetWorktree}, Where: "background", Argv: []string{"code", "{path}"}}}
	local := ws()
	if got := Resolve(local, defs); len(got) != 1 {
		t.Fatalf("resolved on local = %+v, want the local-scoped action", got)
	}
	remote := ws()
	remote.ID.Host = "asia"
	if got := Resolve(remote, defs); len(got) != 0 {
		t.Fatalf("resolved on asia = %+v, want none", got)
	}
}

func TestResolveHonoursRemoteScopeForAnyHost(t *testing.T) {
	defs := []wdconf.Action{{ID: "vim-remote", Scope: wdconf.ScopeRemote,
		Needs: []string{workspace.FacetWorktree}, Where: "pane", Argv: []string{"vim", "{path}"}}}
	for _, host := range []string{"asia", "eu", "us"} {
		remote := ws()
		remote.ID.Host = host
		if got := Resolve(remote, defs); len(got) != 1 {
			t.Fatalf("resolved on %q = %+v, want the remote-scoped action on every remote host", host, got)
		}
	}
	if got := Resolve(ws(), defs); len(got) != 0 {
		t.Fatalf("resolved on local = %+v, want none", got)
	}
}

// hosts stays for the genuinely machine-specific case, and composes with scope.
func TestResolveAppliesScopeAndHostsTogether(t *testing.T) {
	defs := []wdconf.Action{{ID: "only-asia", Scope: wdconf.ScopeRemote, Hosts: []string{"asia"},
		Needs: []string{workspace.FacetWorktree}, Where: "pane", Argv: []string{"true"}}}
	asia := ws()
	asia.ID.Host = "asia"
	if got := Resolve(asia, defs); len(got) != 1 {
		t.Fatalf("resolved on asia = %+v, want 1", got)
	}
	eu := ws()
	eu.ID.Host = "eu"
	if got := Resolve(eu, defs); len(got) != 0 {
		t.Fatalf("resolved on eu = %+v, want none: hosts still narrows further", got)
	}
}

// A run_on = "local" action runs on this machine, so the workspace's path — which
// is a path on the remote host — is not a directory it can start in. Setting it
// made the child's chdir fail, and with Setsid Go reports that as
// "fork/exec /home/louis/.local/bin/zed: no such file or directory", naming the
// binary and looking for all the world like zed was missing.
func TestResolveDoesNotGiveALocalActionARemoteCwd(t *testing.T) {
	remote := ws()
	remote.ID.Host = "asia"
	remote.SSHTarget = "me@asia"
	remote.Worktree = &workspace.Worktree{Path: "/home/me/Code/monorepo"}
	defs := []wdconf.Action{{ID: "zed-remote", Scope: wdconf.ScopeRemote, RunOn: wdconf.RunOnLocal,
		Needs: []string{workspace.FacetWorktree}, Where: "background",
		Argv: []string{"zed", "ssh://{ssh_target}{path}"}}}
	got := Resolve(remote, defs)
	if len(got) != 1 {
		t.Fatalf("resolved = %+v, want the action", got)
	}
	if got[0].Cwd != "" {
		t.Fatalf("cwd = %q, want empty: that path exists only on asia", got[0].Cwd)
	}
	if got[0].Argv[1] != "ssh://me@asia/home/me/Code/monorepo" {
		t.Fatalf("argv = %q, want the remote path still in the URL", got[0].Argv)
	}
}

// A local action on a local workspace still gets the worktree, which does exist.
func TestResolveKeepsTheCwdForALocalWorkspace(t *testing.T) {
	defs := []wdconf.Action{{ID: "zed", RunOn: wdconf.RunOnLocal,
		Needs: []string{workspace.FacetWorktree}, Where: "background", Argv: []string{"zed", "{path}"}}}
	got := Resolve(ws(), defs)
	if len(got) != 1 || got[0].Cwd != "/wt/topic" {
		t.Fatalf("resolved = %+v, want cwd /wt/topic", got)
	}
}
