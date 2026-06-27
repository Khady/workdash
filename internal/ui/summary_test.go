package ui

import (
	"testing"
	"time"

	"github.com/Khady/workdash/internal/model"
)

func TestPRStatusPartUsesNumberOnlyWithStatusColor(t *testing.T) {
	tests := []struct {
		name string
		item model.WorkItem
		want StyledPart
	}{
		{
			name: "open",
			item: model.WorkItem{PRNumber: 123, PRStatus: model.PROpen},
			want: StyledPart{Text: "#123", Color: "green"},
		},
		{
			name: "draft",
			item: model.WorkItem{PRNumber: 123, PRStatus: model.PROpen, PRIsDraft: true},
			want: StyledPart{Text: "#123", Color: "blue"},
		},
		{
			name: "merged",
			item: model.WorkItem{PRNumber: 123, PRStatus: model.PRMerged},
			want: StyledPart{Text: "#123", Color: "purple"},
		},
		{
			name: "closed",
			item: model.WorkItem{PRNumber: 123, PRStatus: model.PRClosed},
			want: StyledPart{Text: "#123", Color: "red"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prStatusPart(tt.item)
			if got != tt.want {
				t.Fatalf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestGitStatusPartsPlacesUnpushedFirstAndAgeLast(t *testing.T) {
	ahead := 2
	activity := time.Now().Add(-2 * time.Hour)
	got := gitStatusParts(model.WorkItem{
		PRNumber:       123,
		PRURL:          "https://github.com/example/repo/pull/123",
		PRStatus:       model.PROpen,
		AheadCount:     &ahead,
		LastActivityAt: &activity,
	})

	if len(got) != 3 {
		t.Fatalf("got %#v", got)
	}
	if got[0].Text != "+2 unpushed" || got[1].Text != "#123" || got[2].Text != "2h" {
		t.Fatalf("unexpected order: %#v", got)
	}
	if got[1].URL != "https://github.com/example/repo/pull/123" {
		t.Fatalf("expected PR URL on PR part, got %#v", got[1])
	}
}

func TestGitStatusPartsShowsTmuxMarkerForWorktree(t *testing.T) {
	got := gitStatusParts(model.WorkItem{
		Kind:           model.KindWorktree,
		HasTmuxSession: true,
	})

	if len(got) != 1 || got[0] != (StyledPart{Text: "tmux", Color: "yellow"}) {
		t.Fatalf("unexpected tmux marker: %#v", got)
	}
}

func TestFormatStyledPartsWrapsHyperlinks(t *testing.T) {
	got := FormatStyledParts([]StyledPart{{
		Text:  "#123",
		Color: "green",
		URL:   "https://github.com/example/repo/pull/123",
	}})

	want := "[#22c55e::b][:::https://github.com/example/repo/pull/123]#123[:::-][white::-]"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPlainItemSummaryStaysPlainWithoutHyperlinkMarkup(t *testing.T) {
	summary := PlainItemSummary(model.WorkItem{
		Kind:      model.KindBranch,
		HostLabel: "local",
		RepoLabel: "repo",
		Title:     "feature/foo",
		PRNumber:  123,
		PRURL:     "https://github.com/example/repo/pull/123",
		PRStatus:  model.PROpen,
	})

	if summary != "local  BR  repo  feature/foo  #123" {
		t.Fatalf("got %q", summary)
	}
}
