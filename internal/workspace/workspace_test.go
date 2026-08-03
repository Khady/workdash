package workspace

import "testing"

func TestWorkspaceReportsPresentFacets(t *testing.T) {
	ws := Workspace{ID: ID{Host: "local", Repo: "/repo", Branch: "topic"}}
	if ws.Has(FacetWorktree) {
		t.Fatal("bare workspace should not have a worktree facet")
	}
	ws.Worktree = &Worktree{Path: "/repo/topic"}
	if !ws.Has(FacetWorktree) {
		t.Fatal("workspace with worktree should report the worktree facet")
	}
	if got := ws.Facets(); len(got) != 1 || got[0] != FacetWorktree {
		t.Fatalf("Facets() = %v, want [worktree]", got)
	}
}

// Matches is the one definition of "does this workspace satisfy these facet
// requirements". The action language, the TUI's view filter and the CLI all need
// it, and three copies of the "!" convention would eventually disagree.
func TestMatchesRequiresEveryFacet(t *testing.T) {
	ws := Workspace{ID: ID{Branch: "topic"}, Worktree: &Worktree{Path: "/wt"}}
	if !Matches(ws, nil) {
		t.Fatal("no requirements should match everything")
	}
	if !Matches(ws, []string{FacetWorktree}) {
		t.Fatal("worktree present should match")
	}
	if Matches(ws, []string{FacetWorktree, FacetSession}) {
		t.Fatal("a missing session should not match")
	}
}

func TestMatchesHonoursNegation(t *testing.T) {
	bare := Workspace{ID: ID{Branch: "topic"}}
	if !Matches(bare, []string{"!" + FacetWorktree}) {
		t.Fatal("!worktree should match a branch with no worktree")
	}
	withTree := Workspace{ID: ID{Branch: "topic"}, Worktree: &Worktree{Path: "/wt"}}
	if Matches(withTree, []string{"!" + FacetWorktree}) {
		t.Fatal("!worktree should not match a branch that has one")
	}
}
