package collect

import (
	"errors"
	"testing"

	"github.com/Khady/workdash/internal/workspace"
)

type fakeTransport struct {
	out map[string]string
	err error
}

func (f fakeTransport) Run(Plan) (map[string]string, error) { return f.out, f.err }

func TestCollectHostBuildsRecordsFromSections(t *testing.T) {
	host := Host{Label: "asia", SSHTarget: "me@asia", TmuxEnabled: true,
		Repos: []Repo{{Root: "/srv/a", Label: "a"}}}
	transport := fakeTransport{out: map[string]string{
		worktreeKey(0): "worktree /srv/a\nHEAD abc\nbranch refs/heads/main\n",
		branchKey(0):   "main\ntopic\n",
		"tmux":         "work\t/srv/a\t3\t1\n",
	}}
	git, sessions, err := CollectHost(transport, host)
	if err != nil {
		t.Fatalf("CollectHost() error = %v", err)
	}
	snap := workspace.Index(git, sessions)
	if len(snap.Workspaces) != 2 {
		t.Fatalf("workspaces = %d %+v, want main and topic", len(snap.Workspaces), snap.Workspaces)
	}
	if snap.Workspaces[0].ID.Host != "asia" {
		t.Fatalf("host = %q, want asia", snap.Workspaces[0].ID.Host)
	}
	if len(snap.Orphans) != 0 {
		t.Fatalf("orphans = %+v, want the session attached to /srv/a", snap.Orphans)
	}
}

// Real tmux sessions have spaces in their names — asia has several.
func TestCollectHostHandlesSessionNamesWithSpaces(t *testing.T) {
	host := Host{Label: "asia", TmuxEnabled: true, Repos: []Repo{{Root: "/srv/a", Label: "a"}}}
	transport := fakeTransport{out: map[string]string{
		worktreeKey(0): "worktree /srv/a\nHEAD abc\nbranch refs/heads/main\n",
		branchKey(0):   "main\n",
		"tmux":         "agents monorepo develop ocaml\t/srv/a\t4\t0\n",
	}}
	_, sessions, err := CollectHost(transport, host)
	if err != nil {
		t.Fatalf("CollectHost() error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].Session.Name != "agents monorepo develop ocaml" {
		t.Fatalf("sessions = %+v, want the spaced name intact", sessions)
	}
}

func TestCollectHostKeepsGoodReposWhenOneSectionIsEmpty(t *testing.T) {
	host := Host{Label: "asia", Repos: []Repo{
		{Root: "/srv/broken", Label: "broken"}, {Root: "/srv/good", Label: "good"},
	}}
	transport := fakeTransport{out: map[string]string{
		worktreeKey(1): "worktree /srv/good\nHEAD abc\nbranch refs/heads/main\n",
		branchKey(1):   "main\n",
	}}
	git, _, err := CollectHost(transport, host)
	if err != nil {
		t.Fatalf("CollectHost() error = %v", err)
	}
	if len(git) == 0 {
		t.Fatal("records = none, want the working repo's records despite the empty sections")
	}
	for _, record := range git {
		if record.RepoLabel == "broken" {
			t.Fatalf("record %+v came from the repo with no output", record)
		}
	}
}

// Whichever record wins the merge has to carry the full name, because which one
// creates the workspace is not knowable: a branch with no worktree is created by
// the branch record, one that is checked out by the worktree record.
func TestCollectHostStampsRepoFullNameOnEveryRecord(t *testing.T) {
	host := Host{Label: "asia", Repos: []Repo{{Root: "/srv/a", Label: "a"}}}
	transport := fakeTransport{out: map[string]string{
		worktreeKey(0): "worktree /srv/a\nHEAD abc\nbranch refs/heads/main\n",
		branchKey(0):   "main\nidle\n",
		remoteKey(0):   "git@github.com:Khady/workdash.git\n",
	}}
	git, _, err := CollectHost(transport, host)
	if err != nil {
		t.Fatalf("CollectHost() error = %v", err)
	}
	if len(git) != 3 {
		t.Fatalf("records = %d %+v, want one worktree and two branch records", len(git), git)
	}
	for _, record := range git {
		if record.RepoFullName != "Khady/workdash" {
			t.Fatalf("record %+v full name = %q, want Khady/workdash", record.ID, record.RepoFullName)
		}
	}
	snap := workspace.Index(git, nil)
	for _, ws := range snap.Workspaces {
		if ws.RepoFullName != "Khady/workdash" {
			t.Fatalf("workspace %s full name = %q, want Khady/workdash", ws.ID, ws.RepoFullName)
		}
	}
}

// A repo with no remote is still a perfectly usable workspace; it just cannot
// ever match a pull request.
func TestCollectHostToleratesARepoWithNoRemote(t *testing.T) {
	host := Host{Label: "asia", Repos: []Repo{{Root: "/srv/a", Label: "a"}}}
	transport := fakeTransport{out: map[string]string{
		worktreeKey(0): "worktree /srv/a\nHEAD abc\nbranch refs/heads/main\n",
		branchKey(0):   "main\n",
	}}
	git, _, err := CollectHost(transport, host)
	if err != nil {
		t.Fatalf("CollectHost() error = %v", err)
	}
	if len(git) == 0 {
		t.Fatal("records = none, want the repo despite having no remote")
	}
	for _, record := range git {
		if record.RepoFullName != "" {
			t.Fatalf("record %+v full name = %q, want empty", record.ID, record.RepoFullName)
		}
	}
}

func TestCollectHostReportsTransportFailure(t *testing.T) {
	transport := fakeTransport{err: errors.New("Network is unreachable")}
	if _, _, err := CollectHost(transport, Host{Label: "asia"}); err == nil {
		t.Fatal("CollectHost() error = nil, want the transport failure")
	}
}
