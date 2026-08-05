package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/Khady/workdash/internal/wdconf"
	"github.com/Khady/workdash/internal/workspace"
	"github.com/gdamore/tcell/v2"
)

// The table carries a header row, so every table index is one ahead of the row
// slice. This is the one piece of the tview wiring worth pinning: getting that
// offset wrong runs an action on the row above the one you selected. Constructing
// the app touches no terminal — tview only opens a screen in Run — so this stays
// headless-safe.
func TestPopulateMapsTableRowsBackToWorkspaces(t *testing.T) {
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	a := newApp(wdconf.Config{})
	a.rows = buildRows(now, sampleSnapshot(now))
	a.populate(false)

	if got, want := a.table.GetRowCount(), len(a.rows)+1; got != want {
		t.Fatalf("table rows = %d, want %d: one header plus every row", got, want)
	}
	if got := a.table.GetCell(0, 0).Text; got != "HOST" {
		t.Fatalf("row 0 = %q, want the header", got)
	}
	if got := a.table.GetCell(1, 0).Text; got != "asia" {
		t.Fatalf("first workspace host cell = %q, want %q", got, "asia")
	}
	if selected := a.selected(); selected == nil || selected.Branch != "louis/ocaml" {
		t.Fatalf("selected() = %+v, want the first workspace", selected)
	}

	a.setQuery("dune")
	if got, want := a.table.GetRowCount(), 2; got != want {
		t.Fatalf("filtered table rows = %d, want %d: the header plus the one match", got, want)
	}
	if selected := a.selected(); selected == nil || selected.Repo != "dune" {
		t.Fatalf("selected() = %+v, want the surviving row", selected)
	}

	a.setQuery("nothing-matches-this")
	if selected := a.selected(); selected != nil {
		t.Fatalf("selected() = %+v, want nil when the filter empties the table", selected)
	}
}

// The first load must land on the newest row at the top. Preserving the selection
// is right for a reload, but on the first one there is nothing to preserve and
// skipping the scroll reset leaves the viewport wherever tview last had it —
// which for a list of hundreds is the bottom, i.e. the oldest work.
func TestFirstLoadDoesNotPreserveSelection(t *testing.T) {
	if preserveOnLoad(time.Time{}) {
		t.Fatal("first load must not preserve: there is no prior selection, and the view has to start at the top")
	}
	if !preserveOnLoad(time.Unix(1000, 0)) {
		t.Fatal("a reload must preserve the selection")
	}
}

// manySnapshot is shaped like the real one in the way that matters here: far more
// rows than fit on screen, only a couple with a session, and about half with a
// pull request. So Tab passes through a view shorter than the viewport on its way
// to one much longer.
func manySnapshot(now time.Time) workspace.Snapshot {
	snap := workspace.Snapshot{}
	for i := 0; i < 200; i++ {
		at := now.Add(-time.Duration(i) * time.Hour)
		ws := workspace.Workspace{
			ID:          workspace.ID{Host: "asia", Repo: "/srv/mono", Branch: fmt.Sprintf("louis/b%03d", i)},
			RepoLabel:   "monorepo",
			Worktree:    &workspace.Worktree{Path: fmt.Sprintf("/srv/wt/%03d", i)},
			CommittedAt: &at,
		}
		if i%2 == 0 {
			ws.PR = &workspace.PR{Number: 1000 + i, State: "OPEN"}
		}
		if i == 7 {
			ws.Sessions = []workspace.Session{{Name: "one", Windows: 1}}
		}
		snap.Workspaces = append(snap.Workspaces, ws)
	}
	return snap
}

// drawn renders the table the way the event loop does, so the assertions see the
// offset tview actually settled on rather than the one we asked for.
func drawn(t *testing.T, a *app, height int) (offset, selected int) {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("screen.Init() error = %v", err)
	}
	defer screen.Fini()
	screen.SetSize(120, height)
	a.table.SetRect(0, 0, 120, height)
	a.table.Draw(screen)
	offset, _ = a.table.GetOffset()
	selected, _ = a.table.GetSelection()
	return offset, selected
}

// Every view starts at its first row, drawn at the top.
//
// Tab used to carry the selection over. That kept your place only when the row
// survived the new view's filter, so where you landed depended on which views you
// had passed through — and it exposed tview's sticky trackEnd flag, set by any
// table shorter than its viewport, which drew the next longer view scrolled to the
// bottom with the selection off screen above. This walks the whole cycle rather
// than jumping straight to PRs, because passing through the short Tmux view is
// what used to break it.
func TestTabStartsEachViewAtTheTop(t *testing.T) {
	const height = 20
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	a := newApp(wdconf.Config{})
	a.rows = buildRows(now, manySnapshot(now))
	a.populate(false)
	drawn(t, a, height)

	// Driven through the real key handler, so the test cannot drift from what Tab
	// actually does.
	for _, label := range []string{"Tmux", "Worktrees", "PRs", "Branches", "All"} {
		a.handleKey(tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone))
		if views[a.view].Label != label {
			t.Fatalf("Tab landed on %q, want %q", views[a.view].Label, label)
		}
		offset, selected := drawn(t, a, height)
		if len(a.shown) == 0 {
			continue
		}
		// A view change starts at the top, so the first data row is both selected and
		// the first one drawn.
		if selected != 1 || offset != 0 {
			t.Fatalf("%s view: selected row %d at offset %d, want row 1 at offset 0 (%d shown)",
				label, selected, offset, len(a.shown))
		}
	}
}

// Clearing trackEnd must not become "scroll back to the top". A reload keeps the
// viewport where it was, because a row moving under the cursor while you read it is
// how a dashboard runs the wrong command.
func TestReloadKeepsTheViewportWhereItWas(t *testing.T) {
	const height = 20
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	a := newApp(wdconf.Config{})
	a.rows = buildRows(now, manySnapshot(now))
	a.populate(false)
	drawn(t, a, height)

	// Somewhere in the middle, as if the user had scrolled there.
	a.table.Select(120, 0)
	a.table.SetOffset(105, 0)
	offsetBefore, selectedBefore := drawn(t, a, height)

	a.populate(true)
	offsetAfter, selectedAfter := drawn(t, a, height)
	if offsetAfter != offsetBefore {
		t.Fatalf("offset moved from %d to %d across a reload", offsetBefore, offsetAfter)
	}
	if selectedAfter != selectedBefore {
		t.Fatalf("selection moved from row %d to %d across a reload", selectedBefore, selectedAfter)
	}
}

// A snapshot that grows back must not leave the viewport at the bottom.
//
// This is the reload half of the trackEnd problem, and it is reachable: asia
// unreachable leaves nine local rows, which is fewer than fit on screen, and any
// table shorter than its viewport sets the flag. When the host comes back, the
// reload preserves the selection — and without clearing the flag the offset is
// pinned to the last row of eight hundred.
func TestReloadAfterAShortSnapshotKeepsTheSelectionOnScreen(t *testing.T) {
	const height = 20
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	a := newApp(wdconf.Config{})

	few := manySnapshot(now)
	few.Workspaces = few.Workspaces[:5]
	a.rows = buildRows(now, few)
	a.populate(false)
	drawn(t, a, height)

	a.rows = buildRows(now, manySnapshot(now))
	a.populate(true)
	offset, selected := drawn(t, a, height)
	if selected <= offset || selected > offset+height-1 {
		t.Fatalf("selected row %d is off screen with offset %d (rows %d..%d visible)",
			selected, offset, offset+1, offset+height-1)
	}
}
