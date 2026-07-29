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
