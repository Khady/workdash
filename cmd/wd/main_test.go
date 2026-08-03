package main

// The executor selection and where-dispatch these tests used to cover moved to
// internal/dispatch, which is where both frontends now get them; see
// internal/dispatch/dispatch_test.go.
import (
	"strings"
	"testing"

	"github.com/Khady/workdash/internal/workspace"
)

func TestSnapshotFailedReportsTotalCollectorFailure(t *testing.T) {
	if !snapshotFailed(0, []string{"repo: fatal: not a git repository"}) {
		t.Fatal("an empty snapshot with warnings is a failure, not an empty list")
	}
	if snapshotFailed(0, nil) {
		t.Fatal("an empty snapshot with no warnings is a legitimately empty list")
	}
	if snapshotFailed(3, []string{"tmux: permission denied"}) {
		t.Fatal("a partial snapshot is a warning, not a failure")
	}
}

func TestPrintSnapshotListsFacetsAndSessions(t *testing.T) {
	var out strings.Builder
	printSnapshot(&out, sampleSnapshot())
	text := out.String()
	if !strings.Contains(text, "topic") || !strings.Contains(text, "solo") {
		t.Fatalf("output = %q, want every workspace listed", text)
	}
	if strings.Contains(text, "\t") {
		t.Fatalf("output = %q, want aligned columns rather than tabs", text)
	}
}

// The same filter the TUI's Tab cycles, available to scripts. It reuses the
// action language's spelling, so `!worktree` means the same thing everywhere.
func TestParseNeedsFlagReadsFacetsAndRest(t *testing.T) {
	needs, rest, err := parseNeeds([]string{"--needs", "session", "--needs", "!worktree", "--json"})
	if err != nil {
		t.Fatalf("parseNeeds() error = %v", err)
	}
	if len(needs) != 2 || needs[0] != "session" || needs[1] != "!worktree" {
		t.Fatalf("needs = %v", needs)
	}
	if len(rest) != 1 || rest[0] != "--json" {
		t.Fatalf("rest = %v, want the other flags untouched", rest)
	}
}

func TestParseNeedsRejectsAnUnknownFacet(t *testing.T) {
	if _, _, err := parseNeeds([]string{"--needs", "worktee"}); err == nil {
		t.Fatal("parseNeeds() error = nil, want a rejection naming the unknown facet")
	}
}

func TestParseNeedsRejectsAMissingValue(t *testing.T) {
	if _, _, err := parseNeeds([]string{"--needs"}); err == nil {
		t.Fatal("parseNeeds() error = nil, want an error about the missing value")
	}
}

func TestFilterWorkspacesAppliesNeeds(t *testing.T) {
	snap := workspace.Snapshot{Workspaces: []workspace.Workspace{
		{ID: workspace.ID{Host: "local", Branch: "a"}, Worktree: &workspace.Worktree{Path: "/wt"}},
		{ID: workspace.ID{Host: "local", Branch: "b"}},
	}}
	got := filterWorkspaces(snap, []string{"!worktree"})
	if len(got.Workspaces) != 1 || got.Workspaces[0].ID.Branch != "b" {
		t.Fatalf("filtered = %+v, want only the branch with no worktree", got.Workspaces)
	}
	// Orphan sessions are sessions, so a session filter keeps them and any other
	// filter drops them.
	snap.Orphans = []workspace.SessionRecord{{Host: "local", Session: workspace.Session{Name: "stray"}}}
	if got := filterWorkspaces(snap, []string{"session"}); len(got.Orphans) != 1 {
		t.Fatal("a session filter should keep unlinked sessions")
	}
	if got := filterWorkspaces(snap, []string{"worktree"}); len(got.Orphans) != 0 {
		t.Fatal("a worktree filter should drop unlinked sessions")
	}
}
