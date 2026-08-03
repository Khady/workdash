package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Khady/workdash/internal/action"
	"github.com/Khady/workdash/internal/wdconf"
	"github.com/Khady/workdash/internal/workspace"
)

func TestStatusLine(t *testing.T) {
	loaded := time.Date(2026, 7, 28, 12, 3, 0, 0, time.Local)
	tests := []struct {
		name           string
		visible, total int
		loadedAt       time.Time
		loading        bool
		want           string
	}{
		{name: "the first load has nothing to report yet", loading: true, want: "loading"},
		{name: "everything visible", visible: 840, total: 840, loadedAt: loaded, want: "840  12:03"},
		{name: "a filter shows how much it hid", visible: 3, total: 840, loadedAt: loaded, want: "3/840  12:03"},
		{name: "a reload keeps the old count on screen", visible: 3, total: 840, loadedAt: loaded, loading: true, want: "3/840  reloading"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := statusLine(test.visible, test.total, test.loadedAt, test.loading)
			if got != test.want {
				t.Fatalf("statusLine() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestWarningsLine(t *testing.T) {
	if got := warningsLine(nil); got != "" {
		t.Fatalf("warningsLine(nil) = %q, want the empty string so the line collapses", got)
	}
	got := warningsLine([]string{"asia: connection timed out", "github: gh not found"})
	if !strings.Contains(got, "asia: connection timed out") || !strings.Contains(got, "github: gh not found") {
		t.Fatalf("warningsLine() = %q, want every warning named", got)
	}
	if strings.Count(got, "\n") != 0 {
		t.Fatalf("warningsLine() = %q, want one line so the table keeps its height", got)
	}
}

func TestActionLabelPrefersTheConfiguredLabel(t *testing.T) {
	if got := actionLabel(action.Resolved{ID: "gwt", Label: "create worktree"}); got != "create worktree" {
		t.Fatalf("actionLabel() = %q, want the configured label", got)
	}
	if got := actionLabel(action.Resolved{ID: "gwt"}); got != "gwt" {
		t.Fatalf("actionLabel() = %q, want the id when no label is configured", got)
	}
}

func TestActionDetailShowsWhereAndTheCommand(t *testing.T) {
	got := actionDetail(action.Resolved{
		ID:    "status",
		Where: wdconf.WhereCapture,
		Argv:  []string{"git", "status", "--short"},
		Cwd:   "/srv/wt",
	})
	if !strings.Contains(got, wdconf.WhereCapture) {
		t.Errorf("actionDetail() = %q, want the where so a pane is distinguishable from a capture", got)
	}
	if !strings.Contains(got, "git status --short") {
		t.Errorf("actionDetail() = %q, want the command that will run", got)
	}
	if !strings.Contains(got, "/srv/wt") {
		t.Errorf("actionDetail() = %q, want the directory it runs in", got)
	}
	shell := actionDetail(action.Resolved{ID: "gwt", Where: wdconf.WherePane, Argv: []string{"gwt", "topic"}, Shell: true})
	if !strings.Contains(shell, "shell") {
		t.Errorf("actionDetail() = %q, want shell actions marked: they resolve rc functions", shell)
	}
}

// An attach action carries no argv at all — there is no command, only a session
// to be taken to — so the detail line has to describe it some other way.
func TestActionDetailDescribesAnAttachByItsSession(t *testing.T) {
	got := actionDetail(action.Resolved{
		ID:      "attach",
		Where:   wdconf.WhereAttach,
		Session: "gwt-monorepo-topic",
		Cwd:     "/srv/wt",
	})
	if !strings.Contains(got, "gwt-monorepo-topic") {
		t.Errorf("actionDetail() = %q, want the session it will attach to", got)
	}
	if strings.Contains(got, "/srv/wt") {
		t.Errorf("actionDetail() = %q, want no directory: attaching runs nothing anywhere", got)
	}
	if strings.Contains(got, "  ·  ") || strings.HasSuffix(got, " ") {
		t.Errorf("actionDetail() = %q, want no filler left by the missing command", got)
	}
}

func TestMenuTitleIdentifiesTheWorkspaceFully(t *testing.T) {
	r := row{
		Host:   "asia",
		Repo:   "monorepo",
		Branch: "louis/ocaml",
		WS:     workspace.Workspace{PR: &workspace.PR{Number: 208, State: "OPEN"}},
	}
	got := menuTitle(r)
	for _, want := range []string{"asia", "monorepo", "louis/ocaml"} {
		if !strings.Contains(got, want) {
			t.Fatalf("menuTitle() = %q, want it to contain %q", got, want)
		}
	}
}

// The active view has to be visible, or a filtered list looks like a broken one:
// the old dashboard showed it in the top-left for exactly that reason.
func TestPromptShowsTheActiveView(t *testing.T) {
	if got := prompt("", 0); !strings.Contains(got, "All") {
		t.Fatalf("prompt = %q, want it to name the All view", got)
	}
	tmux := viewIndex("Tmux")
	got := prompt("", tmux)
	if !strings.Contains(got, "Tmux") {
		t.Fatalf("prompt = %q, want it to name the Tmux view", got)
	}
	if !strings.Contains(got, "tab") {
		t.Fatalf("prompt = %q, want it to say how to change view", got)
	}
}

func TestPromptShowsTheQueryWhileTyping(t *testing.T) {
	got := prompt("mono", 0)
	if !strings.Contains(got, "mono") {
		t.Fatalf("prompt = %q, want the query shown", got)
	}
}
