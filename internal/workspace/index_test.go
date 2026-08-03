package workspace

import (
	"testing"
	"time"
)

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
	if len(snap.Workspaces[0].Sessions) != 1 || snap.Workspaces[0].Sessions[0].Name != "topic" {
		t.Fatalf("session not attached: %+v", snap.Workspaces[0].Sessions)
	}
}

func TestIndexAttachesEverySessionSharingAWorktree(t *testing.T) {
	git := []GitRecord{{ID: ID{Host: "local", Repo: "/repo", Branch: "topic"},
		Worktree: &Worktree{Path: "/wt/topic"}}}
	sessions := []SessionRecord{
		{Host: "local", Session: Session{Name: "agent", Path: "/wt/topic"}},
		{Host: "local", Session: Session{Name: "review", Path: "/wt/topic"}},
	}
	snap := Index(git, sessions)
	if len(snap.Orphans) != 0 {
		t.Fatalf("orphans = %+v, want none: both sessions belong to a known worktree", snap.Orphans)
	}
	got := snap.Workspaces[0].Sessions
	if len(got) != 2 || got[0].Name != "agent" || got[1].Name != "review" {
		t.Fatalf("sessions = %+v, want agent and review", got)
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
	if len(snap.Workspaces[0].Sessions) != 0 {
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

func TestIndexDoesNotAliasCallerWorktrees(t *testing.T) {
	worktree := &Worktree{Path: "/wt/topic"}
	git := []GitRecord{{ID: ID{Host: "local", Repo: "/repo", Branch: "topic"}, Worktree: worktree}}
	snap := Index(git, nil)
	worktree.Path = "/mutated"
	if got := snap.Workspaces[0].Worktree.Path; got != "/wt/topic" {
		t.Fatalf("worktree path = %q, want /wt/topic: Index must not alias its input", got)
	}
}

func TestAttachPRsMatchesByRepoFullNameAndBranch(t *testing.T) {
	snap := Index([]GitRecord{
		{ID: ID{Host: "local", Repo: "/a", Branch: "louis/dune-watch"}, RepoLabel: "dune", RepoFullName: "ocaml/dune"},
		{ID: ID{Host: "local", Repo: "/a", Branch: "other"}, RepoLabel: "dune", RepoFullName: "ocaml/dune"},
	}, nil)
	got := AttachPRs(snap, []PRRecord{
		{FullName: "ocaml/dune", Branch: "louis/dune-watch",
			PR: PR{Number: 10868, URL: "https://github.com/ocaml/dune/pull/10868", State: "MERGED"}},
	})
	byBranch := map[string]Workspace{}
	for _, ws := range got.Workspaces {
		byBranch[ws.ID.Branch] = ws
	}
	if pr := byBranch["louis/dune-watch"].PR; pr == nil || pr.Number != 10868 {
		t.Fatalf("pr = %+v, want #10868", pr)
	}
	if byBranch["other"].Has(FacetPR) {
		t.Fatal("a branch with no matching PR must not gain a pr facet")
	}
}

// The same PR belongs to the same branch wherever that repo is cloned, so it
// attaches on every host rather than only the first.
func TestAttachPRsMatchesAcrossHosts(t *testing.T) {
	snap := Index([]GitRecord{
		{ID: ID{Host: "local", Repo: "/a", Branch: "topic"}, RepoLabel: "dune", RepoFullName: "ocaml/dune"},
		{ID: ID{Host: "asia", Repo: "/srv/a", Branch: "topic"}, RepoLabel: "dune", RepoFullName: "ocaml/dune"},
	}, nil)
	got := AttachPRs(snap, []PRRecord{{FullName: "ocaml/dune", Branch: "topic", PR: PR{Number: 7}}})
	for _, ws := range got.Workspaces {
		if !ws.Has(FacetPR) {
			t.Fatalf("workspace on %q has no pr facet", ws.ID.Host)
		}
	}
}

func TestAttachPRsIgnoresOtherRepositoriesWithTheSameBranchName(t *testing.T) {
	snap := Index([]GitRecord{
		{ID: ID{Host: "local", Repo: "/a", Branch: "main"}, RepoLabel: "dune", RepoFullName: "ocaml/dune"},
	}, nil)
	got := AttachPRs(snap, []PRRecord{{FullName: "someone/else", Branch: "main", PR: PR{Number: 1}}})
	if got.Workspaces[0].Has(FacetPR) {
		t.Fatal("a PR from a different repository must not attach")
	}
}

func TestAttachPRsSkipsWorkspacesWithNoKnownFullName(t *testing.T) {
	snap := Index([]GitRecord{
		{ID: ID{Host: "local", Repo: "/a", Branch: "main"}, RepoLabel: "local-only"},
	}, nil)
	got := AttachPRs(snap, []PRRecord{{FullName: "", Branch: "main", PR: PR{Number: 1}}})
	if got.Workspaces[0].Has(FacetPR) {
		t.Fatal("a repo with no remote must not match a PR with an empty full name")
	}
}

// A pull request raised from a fork has repository = the base repo (ocaml/dune)
// while the local clone's origin is the fork (Khady/dune). Matching only the base
// would never find it, which is most of a fork-based workflow.
func TestAttachPRsMatchesForkWorkflowByHeadRepository(t *testing.T) {
	snap := Index([]GitRecord{
		{ID: ID{Host: "asia", Repo: "/srv/dune", Branch: "louis/dune-watch"},
			RepoLabel: "dune", RepoFullName: "Khady/dune"},
	}, nil)
	got := AttachPRs(snap, []PRRecord{{
		FullName:     "ocaml/dune",
		HeadFullName: "Khady/dune",
		Branch:       "louis/dune-watch",
		PR:           PR{Number: 10868, URL: "https://github.com/ocaml/dune/pull/10868"},
	}})
	if pr := got.Workspaces[0].PR; pr == nil || pr.Number != 10868 {
		t.Fatalf("pr = %+v, want #10868 matched via the head repository", pr)
	}
}

func TestAttachPRsStillMatchesSameRepoPullRequests(t *testing.T) {
	snap := Index([]GitRecord{
		{ID: ID{Host: "asia", Repo: "/srv/mono", Branch: "louis/x"},
			RepoLabel: "monorepo", RepoFullName: "ahrefs-core/monorepo"},
	}, nil)
	got := AttachPRs(snap, []PRRecord{{
		FullName: "ahrefs-core/monorepo", HeadFullName: "ahrefs-core/monorepo",
		Branch: "louis/x", PR: PR{Number: 5},
	}})
	if !got.Workspaces[0].Has(FacetPR) {
		t.Fatal("a same-repo pull request must still attach")
	}
}

// Recency is what makes hundreds of rows navigable: the branch you touched an
// hour ago belongs at the top, not wherever the alphabet puts it.
func TestIndexSortsByRecencyFirst(t *testing.T) {
	old := time.Unix(1000, 0)
	recent := time.Unix(9000, 0)
	snap := Index([]GitRecord{
		{ID: ID{Host: "local", Repo: "/a", Branch: "aaa-old"}, RepoLabel: "a", CommittedAt: &old},
		{ID: ID{Host: "local", Repo: "/a", Branch: "zzz-recent"}, RepoLabel: "a", CommittedAt: &recent},
	}, nil)
	if snap.Workspaces[0].ID.Branch != "zzz-recent" {
		t.Fatalf("order = %s then %s, want the recent branch first",
			snap.Workspaces[0].ID.Branch, snap.Workspaces[1].ID.Branch)
	}
}

// Branches with no date sort after dated ones, then alphabetically, so a repo
// that reports no timestamps still lists predictably.
func TestIndexPutsUndatedWorkspacesLast(t *testing.T) {
	dated := time.Unix(1000, 0)
	snap := Index([]GitRecord{
		{ID: ID{Host: "local", Repo: "/a", Branch: "b-undated"}, RepoLabel: "a"},
		{ID: ID{Host: "local", Repo: "/a", Branch: "a-undated"}, RepoLabel: "a"},
		{ID: ID{Host: "local", Repo: "/a", Branch: "z-dated"}, RepoLabel: "a", CommittedAt: &dated},
	}, nil)
	got := []string{}
	for _, ws := range snap.Workspaces {
		got = append(got, ws.ID.Branch)
	}
	want := []string{"z-dated", "a-undated", "b-undated"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestIndexKeepsTheNewestDateWhenRecordsMerge(t *testing.T) {
	old := time.Unix(1000, 0)
	recent := time.Unix(9000, 0)
	id := ID{Host: "local", Repo: "/a", Branch: "topic"}
	snap := Index([]GitRecord{
		{ID: id, RepoLabel: "a", CommittedAt: &old},
		{ID: id, RepoLabel: "a", CommittedAt: &recent},
	}, nil)
	if got := snap.Workspaces[0].CommittedAt; got == nil || !got.Equal(recent) {
		t.Fatalf("committed at = %v, want %v", got, recent)
	}
}

// A pull request on a branch nobody has fetched is the case the browser has to
// answer: you are looking at a colleague's PR and want a worktree for it. AttachPRs
// alone cannot help — there is nothing to attach to — so the pull request has to
// create the workspace.
func TestAddPRWorkspacesCreatesAWorkspaceForAnUnknownBranch(t *testing.T) {
	snap := Index([]GitRecord{
		{ID: ID{Host: "asia", Repo: "/srv/mono", Branch: "mine"}, RepoLabel: "monorepo",
			RepoFullName: "ahrefs-core/monorepo", SSHTarget: "me@asia"},
	}, nil)
	got := AddPRWorkspaces(snap, []PRRecord{
		{FullName: "ahrefs-core/monorepo", Branch: "koonwen/ci/dune-engine",
			PR: PR{Number: 37238, Title: "ci: dune engine for CI", State: "OPEN"}},
	})
	if len(got.Workspaces) != 2 {
		t.Fatalf("workspaces = %+v, want the pull request to add one", got.Workspaces)
	}
	var added Workspace
	for _, ws := range got.Workspaces {
		if ws.ID.Branch == "koonwen/ci/dune-engine" {
			added = ws
		}
	}
	// The host and repo are derived from a repo workdash already tracks, never
	// invented: that is what makes the workspace actionable, since every action
	// needs to know where to run.
	if added.ID.Host != "asia" || added.ID.Repo != "/srv/mono" {
		t.Fatalf("added = %+v, want it placed on the host that has this repository", added.ID)
	}
	if added.RepoLabel != "monorepo" || added.SSHTarget != "me@asia" {
		t.Fatalf("added = %+v, want the repo's label and ssh target carried over", added)
	}
	if added.Worktree != nil || len(added.Sessions) != 0 {
		t.Fatalf("added = %+v, want a pr facet only", added)
	}
	if !added.Has(FacetPR) || added.PR.Number != 37238 {
		t.Fatalf("added pr = %+v, want #37238", added.PR)
	}
}

// The branch may already be a workspace, in which case AttachPRs' job is done and
// creating a second one would duplicate the row.
func TestAddPRWorkspacesLeavesKnownBranchesAlone(t *testing.T) {
	snap := AttachPRs(Index([]GitRecord{
		{ID: ID{Host: "asia", Repo: "/srv/mono", Branch: "topic"}, RepoLabel: "monorepo",
			RepoFullName: "ahrefs-core/monorepo"},
	}, nil), []PRRecord{{FullName: "ahrefs-core/monorepo", Branch: "topic", PR: PR{Number: 1}}})
	got := AddPRWorkspaces(snap, []PRRecord{
		{FullName: "ahrefs-core/monorepo", Branch: "topic", PR: PR{Number: 1}},
	})
	if len(got.Workspaces) != 1 {
		t.Fatalf("workspaces = %+v, want no duplicate of a branch already known", got.Workspaces)
	}
}

// A repo cloned on two machines could take the worktree on either, so both are
// offered rather than one being picked arbitrarily.
func TestAddPRWorkspacesOffersEveryHostThatHasTheRepository(t *testing.T) {
	snap := Index([]GitRecord{
		{ID: ID{Host: "local", Repo: "/home/mono", Branch: "a"}, RepoLabel: "monorepo", RepoFullName: "o/mono"},
		{ID: ID{Host: "asia", Repo: "/srv/mono", Branch: "b"}, RepoLabel: "monorepo", RepoFullName: "o/mono"},
	}, nil)
	got := AddPRWorkspaces(snap, []PRRecord{{FullName: "o/mono", Branch: "theirs", PR: PR{Number: 2}}})
	hosts := map[string]bool{}
	for _, ws := range got.Workspaces {
		if ws.ID.Branch == "theirs" {
			hosts[ws.ID.Host] = true
		}
	}
	if !hosts["local"] || !hosts["asia"] {
		t.Fatalf("hosts offering the branch = %v, want both clones", hosts)
	}
}

// A fork pull request names the base repo, and the clone workdash tracks may be
// either side of it.
func TestAddPRWorkspacesPlacesAForkPullRequestByHeadRepository(t *testing.T) {
	snap := Index([]GitRecord{
		{ID: ID{Host: "local", Repo: "/a", Branch: "main"}, RepoLabel: "dune", RepoFullName: "Khady/dune"},
	}, nil)
	got := AddPRWorkspaces(snap, []PRRecord{
		{FullName: "ocaml/dune", HeadFullName: "Khady/dune", Branch: "theirs", PR: PR{Number: 3}},
	})
	if len(got.Workspaces) != 2 {
		t.Fatalf("workspaces = %+v, want the fork's clone to host it", got.Workspaces)
	}
}

// A pull request in a repository workdash does not track has nowhere to go. Making
// one up would produce a row whose actions cannot run.
func TestAddPRWorkspacesIgnoresUntrackedRepositories(t *testing.T) {
	snap := Index([]GitRecord{
		{ID: ID{Host: "local", Repo: "/a", Branch: "main"}, RepoLabel: "dune", RepoFullName: "ocaml/dune"},
	}, nil)
	got := AddPRWorkspaces(snap, []PRRecord{{FullName: "someone/else", Branch: "x", PR: PR{Number: 4}}})
	if len(got.Workspaces) != 1 {
		t.Fatalf("workspaces = %+v, want nothing added for an untracked repository", got.Workspaces)
	}
}

// Added workspaces have no commit date, so they must land where undated ones
// belong — after everything dated — rather than wherever they were appended.
func TestAddPRWorkspacesKeepsTheSnapshotSorted(t *testing.T) {
	old := time.Unix(1000, 0)
	snap := Index([]GitRecord{
		{ID: ID{Host: "local", Repo: "/a", Branch: "dated"}, RepoLabel: "dune",
			RepoFullName: "ocaml/dune", CommittedAt: &old},
	}, nil)
	got := AddPRWorkspaces(snap, []PRRecord{{FullName: "ocaml/dune", Branch: "undated", PR: PR{Number: 5}}})
	if got.Workspaces[0].ID.Branch != "dated" {
		t.Fatalf("order = %+v, want the dated workspace first", got.Workspaces)
	}
}
