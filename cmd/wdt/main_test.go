package main

import (
	"testing"
	"time"

	"github.com/Khady/workdash/internal/wdconf"
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
