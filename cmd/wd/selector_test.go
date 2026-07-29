package main

import (
	"strings"
	"testing"

	"github.com/Khady/workdash/internal/workspace"
)

func snapshot() workspace.Snapshot {
	return workspace.Snapshot{Workspaces: []workspace.Workspace{
		{ID: workspace.ID{Host: "local", Repo: "/a", Branch: "topic"}, RepoLabel: "a"},
		{ID: workspace.ID{Host: "local", Repo: "/b", Branch: "topic"}, RepoLabel: "b"},
		{ID: workspace.ID{Host: "local", Repo: "/a", Branch: "solo"}, RepoLabel: "a"},
		{ID: workspace.ID{Host: "local", Repo: "/a", Branch: "feature/nested"}, RepoLabel: "a"},
	}}
}

func TestSelectMatchesUniqueBranch(t *testing.T) {
	got, err := Select(snapshot(), "solo")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.RepoLabel != "a" || got.ID.Branch != "solo" {
		t.Fatalf("selected = %+v", got)
	}
}

// Candidates are listed fully qualified so every one of them is a selector the
// user can type back. A bare "repo:branch" candidate may itself be ambiguous
// once the same repo is cloned on two hosts.
func TestSelectRejectsAmbiguousBranch(t *testing.T) {
	_, err := Select(snapshot(), "topic")
	if err == nil {
		t.Fatal("Select() error = nil, want ambiguity error")
	}
	if got := err.Error(); !strings.Contains(got, "local:a:topic") || !strings.Contains(got, "local:b:topic") {
		t.Fatalf("error = %q, want both candidates listed host-qualified", got)
	}
}

func TestSelectAcceptsRepoQualifiedBranch(t *testing.T) {
	got, err := Select(snapshot(), "b:topic")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.RepoLabel != "b" {
		t.Fatalf("selected = %+v, want repo b", got)
	}
}

func TestSelectMatchesBranchContainingSlash(t *testing.T) {
	got, err := Select(snapshot(), "feature/nested")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.ID.Branch != "feature/nested" {
		t.Fatalf("selected = %+v, want feature/nested", got)
	}
}

func TestSelectReportsUnknownSelector(t *testing.T) {
	if _, err := Select(snapshot(), "nope"); err == nil {
		t.Fatal("Select() error = nil, want not-found error")
	}
}

// multiHostSnapshot has the same repo label ("mono") on two hosts plus one
// label unique to a single host, so it exercises both the ambiguous and the
// unambiguous two-part cases.
func multiHostSnapshot() workspace.Snapshot {
	return workspace.Snapshot{Workspaces: []workspace.Workspace{
		{ID: workspace.ID{Host: "local", Repo: "/a", Branch: "topic"}, RepoLabel: "mono"},
		{ID: workspace.ID{Host: "asia", Repo: "/srv/a", Branch: "topic"}, RepoLabel: "mono"},
		{ID: workspace.ID{Host: "asia", Repo: "/srv/b", Branch: "topic"}, RepoLabel: "svc"},
	}}
}

func TestSelectDisambiguatesByHost(t *testing.T) {
	snap := workspace.Snapshot{Workspaces: []workspace.Workspace{
		{ID: workspace.ID{Host: "local", Repo: "/a", Branch: "topic"}, RepoLabel: "mono"},
		{ID: workspace.ID{Host: "asia", Repo: "/srv/a", Branch: "topic"}, RepoLabel: "mono"},
	}}
	got, err := Select(snap, "asia:mono:topic")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.ID.Host != "asia" {
		t.Fatalf("selected host = %q, want asia", got.ID.Host)
	}
	_, err = Select(snap, "topic")
	if err == nil {
		t.Fatal("Select() error = nil, want ambiguity across hosts")
	}
	if !strings.Contains(err.Error(), "asia:mono:topic") || !strings.Contains(err.Error(), "local:mono:topic") {
		t.Fatalf("error = %q, want typeable host-qualified candidates", err.Error())
	}
}

// A two-part selector stays usable across hosts as long as the repo label picks
// out a single workspace on its own; the host part is only needed to break ties.
func TestSelectAcceptsRepoQualifiedBranchAcrossHosts(t *testing.T) {
	got, err := Select(multiHostSnapshot(), "svc:topic")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.ID.Host != "asia" || got.RepoLabel != "svc" {
		t.Fatalf("selected = %+v, want asia:svc", got)
	}
}

// Precedence: a two-part selector is repo:branch, never host:branch. Here "asia"
// is both a host name and a repo label, and the repo reading wins — that keeps
// the pre-existing "repo:branch" grammar meaning exactly what it always did, and
// host qualification is the newer, rarer case that must be spelled out in full
// as host:repo:branch.
func TestSelectTwoPartSelectorPrefersRepoOverHost(t *testing.T) {
	snap := workspace.Snapshot{Workspaces: []workspace.Workspace{
		{ID: workspace.ID{Host: "asia", Repo: "/srv/web", Branch: "topic"}, RepoLabel: "web"},
		{ID: workspace.ID{Host: "local", Repo: "/asia", Branch: "topic"}, RepoLabel: "asia"},
	}}
	got, err := Select(snap, "asia:topic")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.RepoLabel != "asia" || got.ID.Host != "local" {
		t.Fatalf("selected = %+v, want repo label asia on host local", got)
	}
}

func TestSelectReportsUnknownHost(t *testing.T) {
	_, err := Select(multiHostSnapshot(), "eu:mono:topic")
	if err == nil {
		t.Fatal("Select() error = nil, want not-found error")
	}
	if got := err.Error(); !strings.Contains(got, "eu:mono:topic") {
		t.Fatalf("error = %q, want the selector named", got)
	}
}
