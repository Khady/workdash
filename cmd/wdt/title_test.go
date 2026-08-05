package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestTerminalTitleNamesTheViewAndTheCount(t *testing.T) {
	got := terminalTitle(viewIndex("PRs"), "", 176)
	if got != "wdt · PRs · 176" {
		t.Fatalf("title = %q, want the view and how many rows it has", got)
	}
}

// The filter belongs in the title: a window called "wdt · PRs · 3" with no sign of
// why it is three is the confusing case.
func TestTerminalTitleShowsTheFilter(t *testing.T) {
	got := terminalTitle(viewIndex("All"), "dune", 3)
	if !strings.Contains(got, "dune") || !strings.Contains(got, "3") {
		t.Fatalf("title = %q, want the query and the count", got)
	}
}

func TestTerminalTitleToleratesAnUnknownView(t *testing.T) {
	if got := terminalTitle(99, "", 0); !strings.HasPrefix(got, "wdt · "+views[0].Label) {
		t.Fatalf("title = %q, want it to fall back to the first view", got)
	}
}

// OSC 2 sets the window title; tmux takes it as pane_title, which is what makes
// automatic-rename and set-titles show it. Written directly rather than through
// tcell's SetTitle, which emits nothing under TERM=tmux-256color — measured on a
// pty — and tmux is where wdt runs.
func TestTitlerWritesOSC2(t *testing.T) {
	out := &bytes.Buffer{}
	titles := &titler{Out: out}
	titles.set("wdt · PRs · 176")
	if got, want := out.String(), "\x1b]2;wdt · PRs · 176\x07"; got != want {
		t.Fatalf("wrote %q, want %q", got, want)
	}
}

// renderChrome runs on every keypress and every draw. Rewriting an unchanged title
// would put an escape sequence on the wire for each of them.
func TestTitlerWritesNothingWhenTheTitleIsUnchanged(t *testing.T) {
	out := &bytes.Buffer{}
	titles := &titler{Out: out}
	titles.set("same")
	out.Reset()
	titles.set("same")
	if out.Len() != 0 {
		t.Fatalf("wrote %q for an unchanged title, want nothing", out.String())
	}
}

// Without the pop, automatic-rename leaves the window called "wdt" after quitting.
// tmux honours the title stack — verified on a scratch server — so the previous
// title comes back.
func TestTitlerPushesAndPopsTheTerminalsOwnTitle(t *testing.T) {
	out := &bytes.Buffer{}
	titles := &titler{Out: out}
	titles.push()
	titles.set("wdt · All · 1")
	titles.pop()
	got := out.String()
	if !strings.HasPrefix(got, "\x1b[22;2t") {
		t.Fatalf("wrote %q, want it to save the title first", got)
	}
	if !strings.HasSuffix(got, "\x1b[23;2t") {
		t.Fatalf("wrote %q, want it to restore the title last", got)
	}
}

// The TUI works untitled. A terminal that cannot be opened — no controlling tty at
// all — must not stop it from starting.
func TestTitlerToleratesNoTerminal(t *testing.T) {
	var titles *titler
	titles.push()
	titles.set("anything")
	titles.pop()
}
