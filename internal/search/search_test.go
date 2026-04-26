package search

import (
	"testing"

	"github.com/Khady/workdash/internal/model"
)

func TestBuildDisplayEntriesGroupsEmptyAllQuery(t *testing.T) {
	items := []model.WorkItem{
		{Kind: model.KindPR, Title: "p", RepoLabel: "r", HostLabel: "local", PRURL: "https://github.com/o/r/pull/1"},
		{Kind: model.KindBranch, Title: "b", RepoLabel: "r", HostLabel: "local"},
		{Kind: model.KindWorktree, Title: "w", RepoLabel: "r", HostLabel: "local"},
		{Kind: model.KindTmux, Title: "t", Session: "t", HostLabel: "local"},
	}
	got := BuildDisplayEntries(items, "", model.ModeAll, model.SortDefault)
	if len(got) != 8 || !got[0].IsHeader() || got[0].Label != "Tmux" {
		t.Fatalf("unexpected entries: %#v", got)
	}
}

func TestFilterItemsMatchesVisibleText(t *testing.T) {
	items := []model.WorkItem{{Kind: model.KindWorktree, Title: "repo-main", RepoLabel: "repo", Branch: "main", HostLabel: "local"}}
	got := FilterItems(items, "repo", model.ModeAll, model.SortDefault)
	if len(got) != 1 {
		t.Fatalf("expected match, got %#v", got)
	}
}

func TestBuildDisplayEntriesShowsAtLeastTwentyWorktrees(t *testing.T) {
	items := []model.WorkItem{
		{Kind: model.KindTmux, Title: "t", Session: "t", HostLabel: "local"},
	}
	for i := 0; i < 5; i++ {
		items = append(items, model.WorkItem{
			Kind:      model.KindWorktree,
			Title:     "main-wt",
			RepoLabel: "repo",
			HostLabel: "local",
			IsPrimary: true,
		})
	}
	for i := 0; i < 8; i++ {
		items = append(items, model.WorkItem{
			Kind:      model.KindWorktree,
			Title:     "extra-wt",
			RepoLabel: "repo",
			HostLabel: "local",
		})
	}

	got := BuildDisplayEntries(items, "", model.ModeAll, model.SortDefault)
	worktreeEntries := 0
	for _, entry := range got {
		if entry.Item != nil && entry.Item.Kind == model.KindWorktree {
			worktreeEntries++
		}
	}
	if worktreeEntries != 13 {
		t.Fatalf("got %d worktree entries, want 13", worktreeEntries)
	}
}

func TestCappedMainWorktreeCountUsesTwentyAsFloor(t *testing.T) {
	items := make([]model.WorkItem, 0, 5)
	for i := 0; i < 5; i++ {
		items = append(items, model.WorkItem{Kind: model.KindWorktree, IsPrimary: true})
	}
	if got := cappedMainWorktreeCount(items); got != 20 {
		t.Fatalf("got %d, want 20", got)
	}
}

func TestCappedMainWorktreeCountShowsAllPrimariesAboveTwenty(t *testing.T) {
	items := make([]model.WorkItem, 0, 25)
	for i := 0; i < 25; i++ {
		items = append(items, model.WorkItem{Kind: model.KindWorktree, IsPrimary: true})
	}
	if got := cappedMainWorktreeCount(items); got != 25 {
		t.Fatalf("got %d, want 25", got)
	}
}

func TestFilterItemsMatchesPREnrichmentOnWorktree(t *testing.T) {
	items := []model.WorkItem{{
		Kind:      model.KindWorktree,
		Title:     "wt",
		RepoLabel: "repo",
		Branch:    "feature/x",
		HostLabel: "local",
		PRNumber:  123,
		PRStatus:  model.PROpen,
		PRURL:     "https://github.com/example/monorepo/pull/123",
	}}
	for _, query := range []string{"123", "open", "example/monorepo/pull/123"} {
		got := FilterItems(items, query, model.ModeAll, model.SortDefault)
		if len(got) != 1 {
			t.Fatalf("query %q: expected match, got %#v", query, got)
		}
	}
}

func TestFilterItemsMatchesTmuxWindowAndPathEnrichment(t *testing.T) {
	windows := 4
	attached := 1
	items := []model.WorkItem{{
		Kind:            model.KindTmux,
		Title:           "main",
		Session:         "main",
		HostLabel:       "local",
		Path:            "/repo/wt",
		TmuxWindows:     &windows,
		TmuxAttached:    &attached,
		TmuxWindowNames: []string{"0:shell", "1:laksa"},
	}}
	for _, query := range []string{"laksa", "/repo/wt", "4 windows", "1 attached"} {
		got := FilterItems(items, query, model.ModeAll, model.SortDefault)
		if len(got) != 1 {
			t.Fatalf("query %q: expected match, got %#v", query, got)
		}
	}
}
