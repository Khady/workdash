package workspace

import "testing"

func TestIndexMergesRecordsByID(t *testing.T) {
	git := []GitRecord{
		{ID: ID{Host: "local", Repo: "/repo", Branch: "topic"}, RepoLabel: "repo"},
		{ID: ID{Host: "local", Repo: "/repo", Branch: "topic"}, RepoLabel: "repo",
			Worktree: &Worktree{Path: "/wt/topic"}},
	}
	snap := Index(git, nil)
	if len(snap.Workspaces) != 1 {
		t.Fatalf("got %d workspaces, want 1", len(snap.Workspaces))
	}
	if !snap.Workspaces[0].Has(FacetWorktree) {
		t.Fatal("merged workspace should carry the worktree facet")
	}
}

func TestIndexAttachesSessionByWorktreePath(t *testing.T) {
	git := []GitRecord{{ID: ID{Host: "local", Repo: "/repo", Branch: "topic"},
		Worktree: &Worktree{Path: "/wt/topic"}}}
	sessions := []SessionRecord{{Host: "local", Session: Session{Name: "topic", Path: "/wt/topic"}}}
	snap := Index(git, sessions)
	if len(snap.Orphans) != 0 {
		t.Fatalf("got %d orphans, want 0", len(snap.Orphans))
	}
	if snap.Workspaces[0].Session == nil || snap.Workspaces[0].Session.Name != "topic" {
		t.Fatalf("session not attached: %+v", snap.Workspaces[0].Session)
	}
}

func TestIndexKeepsUnmatchedSessionAsOrphan(t *testing.T) {
	sessions := []SessionRecord{{Host: "local", Session: Session{Name: "stray", Path: "/tmp"}}}
	snap := Index(nil, sessions)
	if len(snap.Workspaces) != 0 {
		t.Fatalf("got %d workspaces, want 0", len(snap.Workspaces))
	}
	if len(snap.Orphans) != 1 || snap.Orphans[0].Session.Name != "stray" {
		t.Fatalf("orphans = %+v, want one named stray", snap.Orphans)
	}
}

func TestIndexDoesNotAttachSessionFromAnotherHost(t *testing.T) {
	git := []GitRecord{{ID: ID{Host: "local", Repo: "/repo", Branch: "topic"},
		Worktree: &Worktree{Path: "/wt/topic"}}}
	sessions := []SessionRecord{{Host: "asia", Session: Session{Name: "topic", Path: "/wt/topic"}}}
	snap := Index(git, sessions)
	if snap.Workspaces[0].Session != nil {
		t.Fatal("session from another host should not attach")
	}
	if len(snap.Orphans) != 1 {
		t.Fatalf("got %d orphans, want 1", len(snap.Orphans))
	}
}

func TestIndexSortsWorkspaces(t *testing.T) {
	git := []GitRecord{
		{ID: ID{Host: "local", Repo: "/b", Branch: "z"}, RepoLabel: "b"},
		{ID: ID{Host: "local", Repo: "/a", Branch: "b"}, RepoLabel: "a"},
		{ID: ID{Host: "local", Repo: "/a", Branch: "a"}, RepoLabel: "a"},
	}
	snap := Index(git, nil)
	got := []string{}
	for _, ws := range snap.Workspaces {
		got = append(got, ws.RepoLabel+"/"+ws.ID.Branch)
	}
	want := []string{"a/a", "a/b", "b/z"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}
