package main

import (
	"strings"
	"testing"

	"github.com/Khady/workdash/internal/wdconf"
	"github.com/Khady/workdash/internal/workspace"
)

const prURL = "https://github.com/khady/workdash/pull/12"

// fakeSnapshot stands in for a real collection: two clones of the same repo
// carrying the same pull request, one with a worktree and a session, plus a
// workspace on an unrelated pull request that must never leak into a group.
func fakeSnapshot() workspace.Snapshot {
	return workspace.Snapshot{Workspaces: []workspace.Workspace{
		{
			ID:        workspace.ID{Host: "local", Repo: "/home/me/monorepo", Branch: "louis/topic"},
			RepoLabel: "monorepo",
			Worktree:  &workspace.Worktree{Path: "/home/me/wt/topic"},
			Sessions:  []workspace.Session{{Name: "topic", Windows: 2}},
			PR:        &workspace.PR{Number: 12, URL: "https://github.com/Khady/workdash/pull/12/files"},
		},
		{
			ID:        workspace.ID{Host: "asia", Repo: "/home/me/code/monorepo", Branch: "louis/topic"},
			RepoLabel: "monorepo",
			SSHTarget: "me@asia",
			PR:        &workspace.PR{Number: 12, URL: prURL},
		},
		{
			ID:        workspace.ID{Host: "local", Repo: "/home/me/monorepo", Branch: "louis/other"},
			RepoLabel: "monorepo",
			Worktree:  &workspace.Worktree{Path: "/home/me/wt/other"},
			PR:        &workspace.PR{Number: 99, URL: "https://github.com/khady/workdash/pull/99"},
		},
	}}
}

func fakeActions() []wdconf.Action {
	return []wdconf.Action{
		{ID: "shell", Label: "shell", Needs: []string{"worktree"}, Where: wdconf.WherePane, Argv: []string{"bash"}},
		{ID: "gwt", Label: "create worktree", Needs: []string{"!worktree"}, Where: wdconf.WherePane, Argv: []string{"gwt", "{branch}"}},
		{ID: "open", Label: "open pull request", Needs: []string{"pr"}, Where: wdconf.WhereBackground, Argv: []string{"firefox", "{pr_url}"}},
	}
}

func TestGroupsForPRGroupsPerWorkspace(t *testing.T) {
	groups := groupsForPR(fakeSnapshot(), fakeActions(), prURL)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want one per workspace carrying the pull request:\n%+v", len(groups), groups)
	}
	if groups[0].Host != "local" || groups[0].Title != "louis/topic" || groups[0].Path != "/home/me/wt/topic" {
		t.Fatalf("first group = %+v, want the local worktree", groups[0])
	}
	if groups[0].Kind != "worktree" {
		t.Fatalf("kind = %q, want %q for a workspace with a worktree", groups[0].Kind, "worktree")
	}
	if groups[1].Host != "asia" || groups[1].Kind != "branch" || groups[1].Path != "" {
		t.Fatalf("second group = %+v, want the remote branch with no worktree", groups[1])
	}
	if got := actionIDLabels(groups[0]); got != "shell,open" {
		t.Fatalf("local actions = %q, want the worktree and pull request actions", got)
	}
	if got := actionIDLabels(groups[1]); got != "gwt,open" {
		t.Fatalf("remote actions = %q, want the create-worktree and pull request actions", got)
	}
	for _, group := range groups {
		if group.ItemKey == "" {
			t.Fatalf("group %+v has no item_key", group)
		}
		for _, act := range group.Actions {
			if !act.Enabled {
				t.Fatalf("action %+v is disabled; the core resolves only applicable actions", act)
			}
			if act.Label == "" || act.Detail == "" {
				t.Fatalf("action %+v wants a label and a detail for the popup", act)
			}
		}
	}
}

// A pull request nobody has checked out is a legitimate empty answer, and the
// unrelated pull request in the snapshot must not answer for it.
func TestGroupsForPRIgnoresOtherPullRequests(t *testing.T) {
	if groups := groupsForPR(fakeSnapshot(), fakeActions(), "https://github.com/khady/workdash/pull/4321"); len(groups) != 0 {
		t.Fatalf("groups = %+v, want none", groups)
	}
}

// A workspace whose actions all turn out inapplicable would otherwise render as
// an empty section with a heading and no buttons.
func TestGroupsForPRDropsWorkspacesWithNoActions(t *testing.T) {
	defs := []wdconf.Action{{ID: "shell", Label: "shell", Needs: []string{"worktree"}, Where: wdconf.WherePane, Argv: []string{"bash"}}}
	groups := groupsForPR(fakeSnapshot(), defs, prURL)
	if len(groups) != 1 || groups[0].Host != "local" {
		t.Fatalf("groups = %+v, want only the workspace that has an applicable action", groups)
	}
}

func TestCountActionsTotalsEveryGroup(t *testing.T) {
	if got := countActions(groupsForPR(fakeSnapshot(), fakeActions(), prURL)); got != 4 {
		t.Fatalf("countActions() = %d, want 4", got)
	}
}

// The token is the whole reason the host can be stateless, so what it decodes to
// has to be exactly the workspace and action it was minted for.
func TestGroupTokensDecodeBackToTheirWorkspaceAndAction(t *testing.T) {
	groups := groupsForPR(fakeSnapshot(), fakeActions(), prURL)
	ref, err := decodeToken(groups[1].Actions[0].Token)
	if err != nil {
		t.Fatalf("decodeToken() error = %v", err)
	}
	want := actionRef{
		ID:     workspace.ID{Host: "asia", Repo: "/home/me/code/monorepo", Branch: "louis/topic"},
		Action: "gwt",
		PRURL:  prURL,
	}
	if ref != want {
		t.Fatalf("decoded = %+v, want %+v", ref, want)
	}
}

func TestResolveRefFindsTheActionAgain(t *testing.T) {
	groups := groupsForPR(fakeSnapshot(), fakeActions(), prURL)
	ref, err := decodeToken(groups[0].Actions[0].Token)
	if err != nil {
		t.Fatalf("decodeToken() error = %v", err)
	}
	resolved, err := resolveRef(fakeSnapshot(), fakeActions(), ref)
	if err != nil {
		t.Fatalf("resolveRef() error = %v", err)
	}
	if resolved.ID != "shell" || resolved.Cwd != "/home/me/wt/topic" || resolved.Host != "local" {
		t.Fatalf("resolved = %+v, want the shell action on the local worktree", resolved)
	}
}

// The point of revalidating: between minting the token and clicking the button
// the worktree can be deleted, and the action that needed it must not run.
func TestResolveRefRefusesAnActionThatNoLongerApplies(t *testing.T) {
	ref := actionRef{
		ID:     workspace.ID{Host: "local", Repo: "/home/me/monorepo", Branch: "louis/topic"},
		Action: "shell",
		PRURL:  prURL,
	}
	snap := fakeSnapshot()
	snap.Workspaces[0].Worktree = nil
	_, err := resolveRef(snap, fakeActions(), ref)
	if err == nil {
		t.Fatal("resolveRef() error = nil, want a refusal now that the worktree is gone")
	}
	if !strings.Contains(err.Error(), "shell") || !strings.Contains(err.Error(), "louis/topic") {
		t.Fatalf("error = %q, want it to name the action and the workspace", err)
	}
}

func TestResolveRefRefusesAWorkspaceThatIsGone(t *testing.T) {
	ref := actionRef{
		ID:     workspace.ID{Host: "local", Repo: "/home/me/monorepo", Branch: "louis/deleted"},
		Action: "shell",
		PRURL:  prURL,
	}
	if _, err := resolveRef(fakeSnapshot(), fakeActions(), ref); err == nil {
		t.Fatal("resolveRef() error = nil, want a refusal for a workspace that no longer exists")
	}
}

// A token names the pull request it was minted from. If that branch's pull
// request has since been closed and dropped, or a different one now sits on the
// branch, the click belongs to a page that no longer describes this workspace.
func TestResolveRefRefusesWhenThePullRequestNoLongerMatches(t *testing.T) {
	ref := actionRef{
		ID:     workspace.ID{Host: "local", Repo: "/home/me/monorepo", Branch: "louis/topic"},
		Action: "open",
		PRURL:  prURL,
	}
	snap := fakeSnapshot()
	snap.Workspaces[0].PR = nil
	if _, err := resolveRef(snap, fakeActions(), ref); err == nil {
		t.Fatal("resolveRef() error = nil, want a refusal now that the pull request facet is gone")
	}
	snap = fakeSnapshot()
	snap.Workspaces[0].PR = &workspace.PR{Number: 13, URL: "https://github.com/khady/workdash/pull/13"}
	if _, err := resolveRef(snap, fakeActions(), ref); err == nil {
		t.Fatal("resolveRef() error = nil, want a refusal when a different pull request is on the branch")
	}
}

func TestSummaryNamesHostsReposBranchesAndFacets(t *testing.T) {
	summary := summaryForPR(fakeSnapshot(), prURL)
	for _, want := range []string{"12", "local", "asia", "monorepo", "louis/topic", "worktree", "session"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary = %q, want it to mention %q", summary, want)
		}
	}
	if strings.Contains(summary, "louis/other") {
		t.Fatalf("summary = %q, want nothing about the unrelated pull request", summary)
	}
	if lines := strings.Count(summary, "\n") + 1; lines > 2 {
		t.Fatalf("summary = %q, want one or two lines", summary)
	}
}

func TestSummarySaysSoWhenNothingMatches(t *testing.T) {
	summary := summaryForPR(fakeSnapshot(), "https://github.com/khady/workdash/pull/4321")
	if summary == "" || !strings.Contains(summary, "no workspace") {
		t.Fatalf("summary = %q, want a sentence saying workdash knows nothing about it", summary)
	}
}

func actionIDLabels(group group) string {
	ids := []string{}
	for _, act := range group.Actions {
		ref, err := decodeToken(act.Token)
		if err != nil {
			ids = append(ids, "?")
			continue
		}
		ids = append(ids, ref.Action)
	}
	return strings.Join(ids, ",")
}
