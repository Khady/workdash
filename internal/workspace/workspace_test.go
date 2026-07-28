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
