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

func TestSelectRejectsAmbiguousBranch(t *testing.T) {
	_, err := Select(snapshot(), "topic")
	if err == nil {
		t.Fatal("Select() error = nil, want ambiguity error")
	}
	if got := err.Error(); !strings.Contains(got, "a:topic") || !strings.Contains(got, "b:topic") {
		t.Fatalf("error = %q, want both candidates listed", got)
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
