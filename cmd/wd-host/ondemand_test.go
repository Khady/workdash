package main

import (
	"errors"
	"testing"

	"github.com/Khady/workdash/internal/wdconf"
	"github.com/Khady/workdash/internal/workspace"
)

// trackedSnapshot has a clone of the repository but not the branch, which is what
// looking at somebody else's pull request looks like from here.
func trackedSnapshot() workspace.Snapshot {
	return workspace.Snapshot{Workspaces: []workspace.Workspace{{
		ID:           workspace.ID{Host: "asia", Repo: "/srv/mono", Branch: "mine"},
		RepoLabel:    "monorepo",
		RepoFullName: "ahrefs-core/monorepo",
		SSHTarget:    "me@asia",
	}}}
}

func stubFetch(t *testing.T, record workspace.PRRecord, err error) *int {
	t.Helper()
	calls := 0
	previous := fetchPRByNumber
	fetchPRByNumber = func(account, owner, name string, number int) (workspace.PRRecord, error) {
		calls++
		return record, err
	}
	t.Cleanup(func() { fetchPRByNumber = previous })
	return &calls
}

func koonwenPR() workspace.PRRecord {
	return workspace.PRRecord{
		FullName: "ahrefs-core/monorepo", HeadFullName: "ahrefs-core/monorepo",
		Branch: "koonwen/ci/dune-engine",
		PR: workspace.PR{Number: 37238, Title: "ci: dune engine for CI", State: "OPEN",
			URL: "https://github.com/ahrefs-core/monorepo/pull/37238"},
	}
}

// The case that started this: a colleague's pull request, which no cached search
// covers, on a repository workdash does track.
func TestWithPRFromPageMakesAnUnknownPullRequestActionable(t *testing.T) {
	calls := stubFetch(t, koonwenPR(), nil)
	url := "https://github.com/ahrefs-core/monorepo/pull/37238"
	snap, warnings := withPRFromPage(wdconf.Config{}, trackedSnapshot(), url)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if *calls != 1 {
		t.Fatalf("fetches = %d, want exactly one", *calls)
	}
	if len(matchingWorkspaces(snap, url)) == 0 {
		t.Fatalf("workspaces = %+v, want the pull request to have one", snap.Workspaces)
	}
	// And it has to be actionable, not merely present: creating the worktree is the
	// whole reason for the lookup.
	groups := groupsForPR(snap, fakeActions(), url)
	if len(groups) != 1 {
		t.Fatalf("groups = %+v, want one", groups)
	}
	labels := []string{}
	for _, action := range groups[0].Actions {
		labels = append(labels, action.Label)
	}
	found := false
	for _, label := range labels {
		if label == "create worktree" {
			found = true
		}
	}
	if !found {
		t.Fatalf("actions = %v, want creating the worktree offered", labels)
	}
}

// A pull request the snapshot already answers for must cost nothing: the cached
// searches cover your own work, which is most of what you look at.
func TestWithPRFromPageDoesNotFetchWhatIsAlreadyKnown(t *testing.T) {
	calls := stubFetch(t, workspace.PRRecord{}, errors.New("must not be called"))
	if _, warnings := withPRFromPage(wdconf.Config{}, fakeSnapshot(), prURL); len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if *calls != 0 {
		t.Fatalf("fetches = %d, want none for a pull request already in the snapshot", *calls)
	}
}

// No clone of the repository means nothing could be created for it, so the call is
// not worth making — and browsing GitHub would otherwise fire one per page.
func TestWithPRFromPageSkipsRepositoriesItDoesNotTrack(t *testing.T) {
	calls := stubFetch(t, workspace.PRRecord{}, errors.New("must not be called"))
	withPRFromPage(wdconf.Config{}, trackedSnapshot(), "https://github.com/someone/else/pull/1")
	if *calls != 0 {
		t.Fatalf("fetches = %d, want none for an untracked repository", *calls)
	}
}

// GitHub being unreachable costs the pull request facet, not the answer: whatever
// the snapshot did have still renders, with the reason attached.
func TestWithPRFromPageReportsAFailedLookupAsAWarning(t *testing.T) {
	stubFetch(t, workspace.PRRecord{}, errors.New("HTTP 401: Bad credentials"))
	snap, warnings := withPRFromPage(wdconf.Config{},
		trackedSnapshot(), "https://github.com/ahrefs-core/monorepo/pull/37238")
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one naming the pull request", warnings)
	}
	if len(snap.Workspaces) != 1 {
		t.Fatalf("workspaces = %+v, want the snapshot left as it was", snap.Workspaces)
	}
}

// The credential has to be the one that can see the repository. Asking with the
// wrong account reports a private repository as absent rather than as forbidden,
// which reads as "workdash does not know this PR".
func TestWithPRFromPageAsksWithTheRepositorysAccount(t *testing.T) {
	asked := ""
	previous := fetchPRByNumber
	fetchPRByNumber = func(account, owner, name string, number int) (workspace.PRRecord, error) {
		asked = account
		return koonwenPR(), nil
	}
	t.Cleanup(func() { fetchPRByNumber = previous })

	cfg := wdconf.Config{Hosts: []wdconf.Host{{Label: "asia", Repos: []wdconf.Repo{
		{Root: "/srv/mono", GHAccount: "work"},
	}}}}
	withPRFromPage(cfg, trackedSnapshot(), "https://github.com/ahrefs-core/monorepo/pull/37238")
	if asked != "work" {
		t.Fatalf("account = %q, want the one configured for this clone", asked)
	}
}
