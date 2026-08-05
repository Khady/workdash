package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// terminalTitle is what the window is called from the outside: which view is on
// screen and how many rows it has. The name alone is not enough in a window list
// full of shells, and the count is the one number that says whether the filter
// you left running is hiding everything.
//
// It stays terse because of where it ends up. tmux takes it as the pane title,
// automatic-rename can make it the window name, and set-titles then puts that in
// the terminal's own title bar — three places that are all short.
func terminalTitle(viewIndex int, query string, shown int) string {
	label := views[0].Label
	if viewIndex >= 0 && viewIndex < len(views) {
		label = views[viewIndex].Label
	}
	parts := []string{"wdt", label}
	if query = strings.TrimSpace(query); query != "" {
		parts = append(parts, "/"+query)
	}
	return strings.Join(append(parts, fmt.Sprintf("%d", shown)), " · ")
}

// titler writes the title to the terminal, and puts back whatever was there when
// wdt exits.
//
// It writes the escape sequence itself rather than calling tcell's SetTitle,
// because that is a no-op exactly where wdt runs. tcell only emits a title for a
// terminal its terminfo entry marks as xterm-like, and the built-in tmux entry is
// not: measured on a pty, TERM=xterm-256color emits `ESC ]2;…` while
// TERM=tmux-256color emits nothing at all. Since wdt lives in a tmux popup, going
// through tcell would mean setting no title ever.
//
// tmux does honour the sequences, both of them — checked on a scratch server:
// `ESC ]2;x BEL` sets pane_title, and the title stack (`CSI 22;2t` to push,
// `CSI 23;2t` to pop) restores the previous one. Popping matters because
// automatic-rename would otherwise leave the window called "wdt" after quitting.
type titler struct {
	// Out is where the sequences go. It is /dev/tty rather than stdout, because
	// tcell talks to the terminal that way too and stdout may not be the terminal
	// at all.
	Out io.Writer
	// last is what was written, so a redraw that changes nothing writes nothing.
	last string
}

func newTitler() *titler {
	// A terminal we cannot open is not a reason to fail: the TUI works fine
	// untitled, so the writes go nowhere and everything else carries on.
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return &titler{Out: io.Discard}
	}
	return &titler{Out: tty}
}

// push saves the terminal's current title, to be restored by pop.
func (t *titler) push() {
	if t == nil || t.Out == nil {
		return
	}
	fmt.Fprint(t.Out, "\x1b[22;2t")
}

// pop restores the title saved by push. It also clears `last`, so a titler reused
// after popping does not skip the next set as a duplicate.
func (t *titler) pop() {
	if t == nil || t.Out == nil {
		return
	}
	t.last = ""
	fmt.Fprint(t.Out, "\x1b[23;2t")
}

func (t *titler) set(title string) {
	if t == nil || t.Out == nil || title == t.last {
		return
	}
	t.last = title
	// OSC 2 is the window title only. OSC 0 would set the icon name as well, which
	// nothing here wants to speak for.
	fmt.Fprintf(t.Out, "\x1b]2;%s\x07", title)
}
