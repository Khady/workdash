// Command wdt is the workdash TUI: one screen listing every workspace the core
// knows about, and an action menu over the actions the core resolves for the one
// you pick.
//
// It exists because the fzf picker it replaces cannot say anything about a row.
// Eight hundred identical monochrome lines make you read every one of them; the
// same eight hundred with a coloured host, a facet grid and an age let you find
// the branch you want by shape. Nothing here re-derives state: the snapshot, the
// action resolution and the execute path are the same code `wd` runs, so the two
// frontends cannot disagree.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/Khady/workdash/internal/action"
	"github.com/Khady/workdash/internal/dispatch"
	"github.com/Khady/workdash/internal/snapshot"
	"github.com/Khady/workdash/internal/wdconf"
)

const usage = `usage: wdt

A dashboard over every configured workspace. Keys:

  type            filter over host, repo, branch and session names
  backspace       delete a filter character
  up/down         move, also page up/down, home/end
  enter           open the action menu for the selected workspace
  ctrl-r          reload the snapshot
  ?               keys and shortcuts
  esc             clear the filter, then quit
  q / ctrl-c      quit`

const footerText = "type to filter  ·  tab view  ·  ↑↓ move  ·  enter actions  ·  ^R reload  ·  ? help  ·  q quit"

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "-h" || arg == "--help" {
			fmt.Println(usage)
			return
		}
		fmt.Fprintf(os.Stderr, "wdt: unexpected argument %q\n\n%s\n", arg, usage)
		os.Exit(2)
	}
	cfg, err := wdconf.Load(wdconf.DefaultPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, "wdt: "+err.Error())
		os.Exit(1)
	}
	if err := newApp(cfg).run(); err != nil {
		fmt.Fprintln(os.Stderr, "wdt: "+err.Error())
		os.Exit(1)
	}
}

type app struct {
	tv     *tview.Application
	pages  *tview.Pages
	root   *tview.Flex
	table  *tview.Table
	filter *tview.TextView
	status *tview.TextView
	notice *tview.TextView

	cfg wdconf.Config
	// rows is the whole snapshot and shown is what the filter left of it. Both
	// are kept so filtering never touches the model or the network.
	rows     []row
	shown    []row
	warnings []string
	message  string
	query    string
	// view indexes views: the old dashboard cycled these with Tab.
	view     int
	loading  bool
	loadedAt time.Time
	// modal names the page currently covering the table, if any. Keys go straight
	// to it while it is up.
	modal string
}

func newApp(cfg wdconf.Config) *app {
	a := &app{
		tv:     tview.NewApplication(),
		pages:  tview.NewPages(),
		cfg:    cfg,
		table:  tview.NewTable().SetSelectable(true, false).SetFixed(1, 0),
		filter: tview.NewTextView().SetDynamicColors(true),
		status: tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignRight),
		notice: tview.NewTextView().SetDynamicColors(true),
	}
	footer := tview.NewTextView().SetDynamicColors(true).SetText("[" + colorMuted + "]" + footerText)
	top := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(a.filter, 0, 1, false).
		AddItem(a.status, 20, 0, false)
	a.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(top, 1, 0, false).
		AddItem(a.notice, 0, 0, false).
		AddItem(a.table, 0, 1, true).
		AddItem(footer, 1, 0, false)
	a.pages.AddPage("main", a.root, true, true)
	a.tv.SetInputCapture(a.handleKey)
	a.populate(false)
	return a
}

func (a *app) run() error {
	a.load()
	return a.tv.SetRoot(a.pages, true).EnableMouse(false).SetFocus(a.table).Run()
}

// load refreshes the snapshot off the event loop. Collecting eleven repos over
// ssh costs a tenth of a second warm and seconds cold, which is long enough that
// doing it inline would freeze the keyboard.
// preserveOnLoad decides whether a load keeps the current selection. A reload
// should: the row you were about to act on is usually still there, and moving it
// under the cursor is how a dashboard runs the wrong command. The first load must
// not, because there is nothing to preserve and the viewport has to be put at the
// top — otherwise it opens on the oldest branch in the list.
func preserveOnLoad(loadedAt time.Time) bool {
	return !loadedAt.IsZero()
}

func (a *app) load() {
	loadedBefore := a.loadedAt
	a.loading = true
	a.renderChrome()
	go func() {
		snap, warnings := snapshot.Load(a.cfg)
		rows := buildRows(time.Now(), snap)
		a.tv.QueueUpdateDraw(func() {
			a.rows = rows
			a.warnings = warnings
			a.loadedAt = time.Now()
			a.loading = false
			// Keep the selection across a reload: the row you were about to act on
			// is usually still there, and moving it under the cursor is how a
			// dashboard runs the wrong command.
			a.populate(preserveOnLoad(loadedBefore))
		})
	}()
}

func (a *app) handleKey(event *tcell.EventKey) *tcell.EventKey {
	// A modal owns the keyboard while it is up, except for the one key that must
	// always work.
	if a.modal != "" {
		if event.Key() == tcell.KeyCtrlC {
			a.tv.Stop()
			return nil
		}
		return event
	}
	switch event.Key() {
	case tcell.KeyCtrlC:
		a.tv.Stop()
		return nil
	case tcell.KeyTab:
		a.view = nextView(a.view, 1)
		a.populate(true)
		return nil
	case tcell.KeyBacktab:
		a.view = nextView(a.view, -1)
		a.populate(true)
		return nil
	case tcell.KeyCtrlR:
		a.message = ""
		a.load()
		return nil
	case tcell.KeyEnter:
		a.openMenu()
		return nil
	case tcell.KeyEsc:
		// Esc clears the filter first, so escaping out of a search never also
		// throws away the session.
		if a.query != "" {
			a.setQuery("")
			return nil
		}
		a.tv.Stop()
		return nil
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if a.query != "" {
			a.setQuery(a.query[:len(a.query)-1])
		}
		return nil
	case tcell.KeyRune:
		return a.handleRune(event)
	}
	// Arrows, page up/down, home and end are the table's own; letting it move the
	// selection keeps scrolling and the fixed header consistent.
	return event
}

// handleRune routes typing into the filter, which is what makes this a picker
// rather than a viewer. "q" is the one exception, and only while the filter is
// empty: quitting has to be reachable without a modifier, and because the filter
// matches substrings rather than prefixes, a branch called "queue-fix" is still
// findable by typing "ueue".
func (a *app) handleRune(event *tcell.EventKey) *tcell.EventKey {
	switch r := event.Rune(); r {
	case 'q':
		if a.query == "" {
			a.tv.Stop()
			return nil
		}
		a.setQuery(a.query + string(r))
	case '?':
		if a.query == "" {
			a.showHelp()
			return nil
		}
		a.setQuery(a.query + string(r))
	default:
		a.setQuery(a.query + string(r))
	}
	return nil
}

func (a *app) setQuery(query string) {
	a.query = query
	a.populate(false)
}

// populate rebuilds the table from rows and the query. Everything it draws comes
// from the pure derivations in row.go, so what the screen says is exactly what
// those tests pin.
func (a *app) populate(preserve bool) {
	previous := ""
	if preserve {
		if selected := a.selected(); selected != nil {
			previous = rowKey(*selected)
		}
	}
	a.shown = filterRows(a.rows, a.query, a.view)
	a.table.Clear()
	for column, title := range []string{"HOST", "REPO", "BRANCH", "W S", "↑", "PR", "SESSION", "AGE"} {
		a.table.SetCell(0, column, tview.NewTableCell(title).
			SetTextColor(tcell.GetColor(colorMuted)).
			SetSelectable(false))
	}
	selected := 1
	for index, r := range a.shown {
		if previous != "" && rowKey(r) == previous {
			selected = index + 1
		}
		a.setRow(index+1, r)
	}
	if len(a.shown) > 0 {
		a.table.Select(selected, 0)
	}
	if !preserve {
		a.table.SetOffset(0, 0)
	}
	a.renderChrome()
}

// setRow writes one workspace. Colour carries meaning per column: the hue is the
// host, brightness is the branch, and everything else is muted context.
func (a *app) setRow(index int, r row) {
	branchColor := colorText
	if r.Unlinked {
		// An unlinked session is a tmux session workdash cannot attribute to a
		// worktree. It is reported rather than hidden, but it is not a workspace,
		// so it does not get a workspace's contrast.
		branchColor = colorMuted
	}
	a.table.SetCell(index, 0, styled(r.Host, hostColor(r.Host), true).SetMaxWidth(10))
	a.table.SetCell(index, 1, styled(r.Repo, colorMuted, false).SetMaxWidth(18))
	a.table.SetCell(index, 2, styled(r.Branch, branchColor, !r.Unlinked).SetMaxWidth(46).SetExpansion(1))
	a.table.SetCell(index, 3, tagged(renderBadges(r.Facets)))
	// Unpushed commits sit beside the facet grid: it is state, like the facets, and
	// it is what tells you a worktree is not safe to delete.
	a.table.SetCell(index, 4, styled(r.Unpushed, colorPRDraft, true).SetAlign(tview.AlignRight).SetMaxWidth(3))
	pr := tagged(renderBadges([]badge{r.PR})).SetAlign(tview.AlignRight).SetMaxWidth(7)
	if r.PR.URL != "" {
		// tcell emits OSC 8 from the style, so the number is clickable in a
		// terminal that supports it and plain text in one that does not.
		pr.SetStyle(pr.Style.Url(r.PR.URL))
	}
	a.table.SetCell(index, 5, pr)
	a.table.SetCell(index, 6, styled(r.Session, colorMuted, false).SetMaxWidth(38).SetExpansion(1))
	a.table.SetCell(index, 7, styled(r.Age, colorMuted, false).SetAlign(tview.AlignRight).SetMaxWidth(5))
}

// selectedBackground is the one thing selection changes. Repainting the whole row
// in one colour would throw away every distinction the palette just made.
const selectedBackground = tcell.ColorDarkSlateGray

func styled(text, color string, bold bool) *tview.TableCell {
	style := tcell.StyleDefault.Foreground(tcell.GetColor(color)).Background(tcell.ColorBlack)
	if bold {
		style = style.Bold(true)
	}
	// tview.Escape, because a branch or a tmux session may legitimately contain
	// "[", which would otherwise be read as a colour tag and eat the text.
	return tview.NewTableCell(tview.Escape(text)).
		SetStyle(style).
		SetSelectedStyle(style.Background(selectedBackground))
}

// tagged is for text that already carries its own colour tags: the badges keep
// their hue when the row is selected because their reset tag falls back to the
// cell's style, which is the same but for the background.
func tagged(text string) *tview.TableCell {
	style := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)
	return tview.NewTableCell(text).
		SetStyle(style).
		SetSelectedStyle(style.Background(selectedBackground))
}

func (a *app) renderChrome() {
	a.filter.SetText(prompt(a.query, a.view))
	a.status.SetText("[" + colorMuted + "]" + statusLine(len(a.shown), len(a.rows), a.loadedAt, a.loading))

	lines := []string{}
	if a.message != "" {
		lines = append(lines, "["+colorWorktree+"]"+tview.Escape(a.message))
	}
	if warnings := warningsLine(a.warnings); warnings != "" {
		lines = append(lines, "["+colorPRClosed+"]"+tview.Escape(warnings))
	}
	a.notice.SetText(strings.Join(lines, "  "))
	// The notice line takes no space at all when there is nothing to say, which is
	// the normal case.
	height := 0
	if len(lines) > 0 {
		height = 1
	}
	a.root.ResizeItem(a.notice, height, 0)
}

func (a *app) selected() *row {
	index, _ := a.table.GetSelection()
	// Row 0 is the header, so table rows are one ahead of the slice.
	if index-1 < 0 || index-1 >= len(a.shown) {
		return nil
	}
	return &a.shown[index-1]
}

// rowKey identifies a row across reloads. Unlinked sessions are keyed separately
// so a session named after a branch cannot inherit that branch's selection.
func rowKey(r row) string {
	if r.Unlinked {
		return "session:" + r.Host + ":" + r.Branch
	}
	return menuTitle(r)
}

func (a *app) openMenu() {
	selected := a.selected()
	if selected == nil {
		return
	}
	if selected.Unlinked {
		a.say("unlinked tmux session: no workspace to resolve actions for")
		return
	}
	resolved := action.Resolve(selected.WS, a.cfg.Actions)
	if len(resolved) == 0 {
		a.say("no action applies to " + menuTitle(*selected))
		return
	}
	list := tview.NewList().ShowSecondaryText(true)
	list.SetMainTextColor(tcell.GetColor(colorText)).
		SetSecondaryTextColor(tcell.GetColor(colorMuted)).
		SetShortcutColor(tcell.GetColor(colorSession)).
		SetSelectedBackgroundColor(selectedBackground).
		SetSelectedTextColor(tcell.GetColor(colorText))
	for index, item := range resolved {
		// Shortcuts are positional digits: the menu is short, and a per-action
		// letter would collide with the action ids the config is free to choose.
		shortcut := rune(0)
		if index < 9 {
			shortcut = rune('1' + index)
		}
		chosen := item
		list.AddItem(tview.Escape(actionLabel(chosen)), tview.Escape(actionDetail(chosen)), shortcut, func() {
			a.runAction(chosen)
		})
	}
	list.SetDoneFunc(a.closeModal)
	list.SetBorder(true).
		SetBorderColor(tcell.GetColor(colorMuted)).
		SetTitle(" " + tview.Escape(menuTitle(*selected)) + " ").
		SetTitleColor(tcell.GetColor(hostColor(selected.Host)))
	a.showModal("menu", list, 96, min(3+len(resolved)*2, 30))
}

// runAction executes exactly the way `wd run` does: internal/dispatch picks the
// executor for the host and routes the where. Running it off the event loop is
// what keeps the TUI alive afterwards — a pane action returns as soon as tmux has
// the window, and the dashboard is still there to run the next one.
func (a *app) runAction(resolved action.Resolved) {
	a.closeModal()
	a.showBusy(actionLabel(resolved))
	go func() {
		out, err := func() (string, error) {
			// dispatch.Host, not resolved.Host: an action with run_on = "local"
			// runs here even when the workspace it names lives on another machine.
			executor, err := dispatch.ExecutorFor(a.cfg, dispatch.Host(resolved))
			if err != nil {
				return "", err
			}
			return dispatch.Execute(executor, resolved)
		}()
		a.tv.QueueUpdateDraw(func() {
			a.closeModal()
			if err != nil {
				a.warnings = append(a.warnings, actionLabel(resolved)+": "+err.Error())
				a.renderChrome()
				return
			}
			if resolved.Where == wdconf.WhereCapture {
				if strings.TrimSpace(out) == "" {
					a.say(actionLabel(resolved) + ": no output")
					return
				}
				a.showOutput(actionLabel(resolved), out)
				return
			}
			// A pane created a session or a window and an attach changed who is
			// looking at one, so the facets on screen are now one step behind.
			a.message = out
			a.load()
		})
	}()
}

func (a *app) say(message string) {
	a.message = message
	a.renderChrome()
}

func (a *app) showBusy(label string) {
	view := tview.NewTextView().SetText("running " + label + " …").SetTextAlign(tview.AlignCenter)
	view.SetBorder(true).SetBorderColor(tcell.GetColor(colorMuted))
	a.showModal("busy", view, 60, 5)
}

func (a *app) showOutput(label, out string) {
	view := tview.NewTextView().SetText(out).SetScrollable(true).SetWrap(false)
	view.SetBorder(true).
		SetBorderColor(tcell.GetColor(colorMuted)).
		SetTitle(" " + tview.Escape(label) + " ")
	view.SetInputCapture(dismissOn(a.closeModal))
	a.showModal("output", view, 110, 30)
}

func (a *app) showHelp() {
	view := tview.NewTextView().SetText(usage).SetScrollable(true)
	view.SetBorder(true).SetBorderColor(tcell.GetColor(colorMuted)).SetTitle(" keys ")
	view.SetInputCapture(dismissOn(a.closeModal))
	a.showModal("help", view, 76, 18)
}

// dismissOn closes a read-only modal on the three keys everyone reaches for,
// while leaving the arrows to scroll it.
func dismissOn(close func()) func(*tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Key() == tcell.KeyEnter || event.Rune() == 'q' {
			close()
			return nil
		}
		return event
	}
}

func (a *app) showModal(name string, primitive tview.Primitive, width, height int) {
	a.modal = name
	a.pages.AddPage(name, center(primitive, width, height), true, true)
	a.tv.SetFocus(primitive)
}

func (a *app) closeModal() {
	if a.modal == "" {
		return
	}
	a.pages.RemovePage(a.modal)
	a.modal = ""
	a.tv.SetFocus(a.table)
}

func center(primitive tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(nil, 0, 1, false).
			AddItem(primitive, width, 0, true).
			AddItem(nil, 0, 1, false), height, 0, true).
		AddItem(nil, 0, 1, false)
}
