package main

import (
	"reflect"
	"testing"
	"time"

	"github.com/Khady/workdash/internal/workspace"
)

func TestHostColorMatchesTheLegacyDashboardPalette(t *testing.T) {
	// Pinned against internal/ui.HostColor: the same host must get the same
	// colour in both frontends, or the two tools look like different products.
	tests := map[string]string{
		"local": "#6df290",
		"asia":  "#6df2de",
		"foo":   "#f26db1",
		"bar":   "#f26d9d",
	}
	for host, want := range tests {
		if got := hostColor(host); got != want {
			t.Errorf("hostColor(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestFacetBadgesAlwaysReportsThreeSlots(t *testing.T) {
	dot := badge{Text: "·", Color: colorAbsent}
	tests := []struct {
		name string
		ws   workspace.Workspace
		want []badge
	}{
		{
			name: "a bare branch has no facet at all",
			ws:   workspace.Workspace{},
			want: []badge{dot, dot, dot},
		},
		{
			name: "a linked worktree",
			ws:   workspace.Workspace{Worktree: &workspace.Worktree{Path: "/w"}},
			want: []badge{{Text: "W", Color: colorWorktree}, dot, dot},
		},
		{
			name: "the repo's main checkout is distinguished from a gwt worktree",
			ws:   workspace.Workspace{Worktree: &workspace.Worktree{Path: "/w", IsMain: true}},
			want: []badge{{Text: "M", Color: colorWorktree}, dot, dot},
		},
		{
			name: "a session without a worktree still occupies its own slot",
			ws:   workspace.Workspace{Sessions: []workspace.Session{{Name: "solo"}}},
			want: []badge{dot, {Text: "S", Color: colorSession}, dot},
		},
		{
			name: "all three",
			ws: workspace.Workspace{
				Worktree: &workspace.Worktree{Path: "/w"},
				Sessions: []workspace.Session{{Name: "solo"}},
				PR:       &workspace.PR{Number: 7, State: "OPEN"},
			},
			want: []badge{{Text: "W", Color: colorWorktree}, {Text: "S", Color: colorSession}, {Text: "P", Color: colorPR}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := facetBadges(test.ws); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("facetBadges() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestPRBadgeColoursByState(t *testing.T) {
	tests := []struct {
		name string
		pr   *workspace.PR
		want badge
	}{
		{"no pull request", nil, badge{}},
		{"open", &workspace.PR{Number: 12, State: "OPEN"}, badge{Text: "#12", Color: colorPROpen}},
		{"lowercase state from another source", &workspace.PR{Number: 12, State: "open"}, badge{Text: "#12", Color: colorPROpen}},
		{"draft", &workspace.PR{Number: 12, State: "OPEN", Draft: true}, badge{Text: "#12", Color: colorPRDraft}},
		{"merged", &workspace.PR{Number: 12, State: "MERGED"}, badge{Text: "#12", Color: colorPRMerged}},
		{"a merged draft is merged first", &workspace.PR{Number: 12, State: "MERGED", Draft: true}, badge{Text: "#12", Color: colorPRMerged}},
		{"closed", &workspace.PR{Number: 12, State: "CLOSED"}, badge{Text: "#12", Color: colorPRClosed}},
		{"an unknown state is treated as open rather than hidden", &workspace.PR{Number: 12, State: "WEIRD"}, badge{Text: "#12", Color: colorPROpen}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := prBadge(workspace.Workspace{PR: test.pr})
			if got != test.want {
				t.Fatalf("prBadge() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestSessionSummary(t *testing.T) {
	tests := []struct {
		name string
		ws   workspace.Workspace
		want string
	}{
		{"no session", workspace.Workspace{}, ""},
		{
			"one session reports its windows",
			workspace.Workspace{Sessions: []workspace.Session{{Name: "solo", Windows: 2}}},
			"solo  2 win",
		},
		{
			"an attached session says so",
			workspace.Workspace{Sessions: []workspace.Session{{Name: "solo", Windows: 1, Attached: true}}},
			"solo  1 win  attached",
		},
		{
			"extra sessions are counted rather than dropped",
			workspace.Workspace{Sessions: []workspace.Session{
				{Name: "agent", Windows: 3},
				{Name: "shell", Windows: 1},
			}},
			"agent  3 win  +1",
		},
		{
			"a session with no window count still names itself",
			workspace.Workspace{Sessions: []workspace.Session{{Name: "solo"}}},
			"solo",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sessionSummary(test.ws); got != test.want {
				t.Fatalf("sessionSummary() = %q, want %q", got, test.want)
			}
		})
	}
}

func sampleSnapshot(now time.Time) workspace.Snapshot {
	hoursAgo := func(h int) *time.Time {
		at := now.Add(-time.Duration(h) * time.Hour)
		return &at
	}
	return workspace.Snapshot{
		Workspaces: []workspace.Workspace{
			{
				ID:          workspace.ID{Host: "asia", Repo: "/srv/monorepo", Branch: "louis/ocaml"},
				RepoLabel:   "monorepo",
				Worktree:    &workspace.Worktree{Path: "/srv/wt"},
				Sessions:    []workspace.Session{{Name: "gwt-monorepo-louis-ocaml", Windows: 2}},
				PR:          &workspace.PR{Number: 208, State: "OPEN"},
				CommittedAt: hoursAgo(14),
			},
			{
				ID:          workspace.ID{Host: "local", Repo: "/home/me/dune", Branch: "main"},
				RepoLabel:   "dune",
				CommittedAt: hoursAgo(72),
			},
		},
		Orphans: []workspace.SessionRecord{
			{Host: "local", Session: workspace.Session{Name: "claude rc", Windows: 1, Attached: true}},
		},
	}
}

func TestBuildRowsPreservesSnapshotOrderAndFillsColumns(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	rows := buildRows(now, sampleSnapshot(now))
	if len(rows) != 3 {
		t.Fatalf("buildRows() returned %d rows, want 2 workspaces plus 1 unlinked session", len(rows))
	}
	first := rows[0]
	if first.Host != "asia" || first.Repo != "monorepo" || first.Branch != "louis/ocaml" {
		t.Errorf("row 0 = %+v, want the snapshot's first workspace verbatim", first)
	}
	if first.Age != "14h" {
		t.Errorf("row 0 age = %q, want %q", first.Age, "14h")
	}
	if first.PR.Text != "#208" {
		t.Errorf("row 0 pr = %+v, want #208", first.PR)
	}
	if first.Session != "gwt-monorepo-louis-ocaml  2 win" {
		t.Errorf("row 0 session = %q, want the session summary", first.Session)
	}
	if len(first.Facets) != 3 || first.Facets[0].Text != "W" {
		t.Errorf("row 0 facets = %+v, want three slots starting with the worktree", first.Facets)
	}
	if first.Unlinked {
		t.Error("row 0 is a real workspace, not an unlinked session")
	}
	second := rows[1]
	if second.Host != "local" || second.Repo != "dune" || second.Age != "3d" {
		t.Errorf("row 1 = %+v, want the second workspace in snapshot order", second)
	}
	if second.Facets[0].Text != "·" {
		t.Errorf("row 1 facets = %+v, want an empty worktree slot", second.Facets)
	}
	orphan := rows[2]
	if !orphan.Unlinked {
		t.Error("the last row is an unlinked session and must say so")
	}
	if orphan.Host != "local" || orphan.Repo != "(unlinked)" || orphan.Branch != "claude rc" {
		t.Errorf("orphan row = %+v, want the session reported under (unlinked)", orphan)
	}
	if orphan.Facets[1].Text != "S" {
		t.Errorf("orphan facets = %+v, want the session slot filled", orphan.Facets)
	}
	if orphan.Session != "claude rc  1 win  attached" {
		t.Errorf("orphan session = %q, want its own summary", orphan.Session)
	}
	if orphan.Age != "" {
		t.Errorf("orphan age = %q, want blank: a session has no commit time", orphan.Age)
	}
}

func TestMatchesFilter(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	rows := buildRows(now, sampleSnapshot(now))
	monorepo, dune := rows[0], rows[1]
	tests := []struct {
		name  string
		row   row
		query string
		want  bool
	}{
		{"an empty query matches everything", dune, "", true},
		{"host", monorepo, "asia", true},
		{"repo", monorepo, "monorepo", true},
		{"branch", monorepo, "ocaml", true},
		{"session name", monorepo, "gwt-monorepo", true},
		{"case is ignored", monorepo, "OCaml", true},
		{"whitespace only is an empty query", dune, "   ", true},
		{"every term must match", monorepo, "asia ocaml", true},
		{"one failing term rejects the row", monorepo, "asia dune", false},
		{"a term matching nothing", monorepo, "zzz", false},
		{"another host's row is rejected", dune, "asia", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matchesFilter(test.row, test.query); got != test.want {
				t.Fatalf("matchesFilter(%q) = %v, want %v", test.query, got, test.want)
			}
		})
	}
}

func TestFilterRowsKeepsOrderAndNeverReturnsNil(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	rows := buildRows(now, sampleSnapshot(now))
	all := filterRows(rows, "")
	if len(all) != len(rows) || all[0].Branch != rows[0].Branch {
		t.Fatalf("filterRows(all) = %d rows, want the input untouched", len(all))
	}
	one := filterRows(rows, "monorepo")
	if len(one) != 1 || one[0].Repo != "monorepo" {
		t.Fatalf("filterRows(%q) = %+v, want only the monorepo row", "monorepo", one)
	}
	none := filterRows(rows, "nothing-matches-this")
	if none == nil {
		t.Fatal("filterRows() returned nil, want an empty slice so callers can range over it")
	}
	if len(none) != 0 {
		t.Fatalf("filterRows() = %+v, want no rows", none)
	}
}

func TestRenderBadgesTagsEachSlotAndSkipsEmptyOnes(t *testing.T) {
	got := renderBadges([]badge{{Text: "W", Color: colorWorktree}, {Text: "·", Color: colorAbsent}})
	want := "[" + colorWorktree + "::b]W[-:-:-] [" + colorAbsent + "::b]·[-:-:-]"
	if got != want {
		t.Fatalf("renderBadges() = %q, want %q", got, want)
	}
	if got := renderBadges([]badge{{}}); got != "" {
		t.Fatalf("renderBadges(empty badge) = %q, want the empty string", got)
	}
}
