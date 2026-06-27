package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/Khady/workdash/internal/actions"
	"github.com/Khady/workdash/internal/config"
	"github.com/Khady/workdash/internal/loader"
	"github.com/Khady/workdash/internal/logging"
	"github.com/Khady/workdash/internal/model"
	"github.com/Khady/workdash/internal/search"
)

type WorkdashApp struct {
	app       *tview.Application
	pages     *tview.Pages
	root      *tview.Flex
	searchBox *tview.InputField
	modeText  *tview.TextView
	warnings  *tview.TextView
	table     *tview.Table
	details   *tview.TextView

	configPath              string
	configEditor            string
	terminalLauncher        string
	terminalLauncherDefault bool
	commandDefinitions      []config.CommandConfig
	cwd                     string

	items           []model.WorkItem
	warningLines    []string
	loading         bool
	mode            model.Mode
	sortOrder       model.SortOrder
	query           string
	detailsVisible  bool
	selectedByRow   map[int]*model.WorkItem
	selectedAction  model.ShellAction
	returnCode      int
	inlineSuccess   int
	inlineFailure   int
	terminalSuccess int
	terminalFailure int
	menu            *actionMenuState
}

type actionMenuState struct {
	item                model.WorkItem
	options             []actions.ItemActionOption
	selected            int
	pendingConfirmation *actions.ItemActionOption
	view                *tview.TextView
	frame               *tview.Frame
	scrollOffset        int
}

func NewWorkdashApp(configPath, cwd string) (*WorkdashApp, error) {
	logging.Printf("workdash start cwd=%q config=%q log=%q", cwd, configPath, logging.DefaultLogPath())
	cfgResult, err := config.Load(configPath)
	if err != nil {
		logging.Printf("config load failed config=%q error=%q", configPath, err.Error())
		return nil, err
	}
	a := &WorkdashApp{
		app:                     tview.NewApplication(),
		pages:                   tview.NewPages(),
		configPath:              configPath,
		configEditor:            cfgResult.Config.ConfigEditor,
		terminalLauncher:        cfgResult.Config.TerminalLauncher,
		terminalLauncherDefault: cfgResult.Config.TerminalLauncherDefault,
		commandDefinitions:      cfgResult.Config.Commands,
		cwd:                     cwd,
		mode:                    model.ModeAll,
		sortOrder:               model.SortDefault,
		selectedByRow:           map[int]*model.WorkItem{},
		returnCode:              130,
	}
	a.build()
	return a, nil
}

func (a *WorkdashApp) Run() (model.ShellAction, int, error) {
	SetTerminalTitle("workdash")
	go func() {
		err := loader.StreamItems(a.configPath, a.cwd, func(items []model.WorkItem, warnings []string, done bool) {
			a.app.QueueUpdateDraw(func() {
				a.items = items
				a.warningLines = warnings
				a.loading = !done
				a.refreshSelection(false)
			})
		})
		if err != nil {
			a.app.QueueUpdateDraw(func() {
				a.warningLines = append(a.warningLines, "Load failed: "+err.Error())
				a.loading = false
				a.refresh()
			})
		}
	}()
	if err := a.app.SetRoot(a.pages, true).EnableMouse(false).Run(); err != nil {
		return nil, a.returnCode, err
	}
	return a.selectedAction, a.returnCode, nil
}

func (a *WorkdashApp) build() {
	a.searchBox = tview.NewInputField().SetPlaceholder("Type to filter worktrees, branches, and tmux sessions")
	searchStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)
	placeholderStyle := tcell.StyleDefault.Foreground(tcell.ColorGray).Background(tcell.ColorBlack)
	a.searchBox.SetFieldStyle(searchStyle)
	a.searchBox.SetPlaceholderStyle(placeholderStyle)
	a.searchBox.SetLabelStyle(searchStyle)
	a.searchBox.SetBackgroundColor(tcell.ColorBlack)
	a.searchBox.SetChangedFunc(func(text string) {
		a.query = text
		a.refreshSelection(false)
	})
	a.modeText = tview.NewTextView().SetDynamicColors(true)
	a.warnings = tview.NewTextView().SetDynamicColors(true)
	a.table = tview.NewTable().SetSelectable(true, false).SetFixed(0, 0)
	a.details = tview.NewTextView().SetDynamicColors(true).SetWrap(true)

	top := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(a.modeText, 10, 0, false).
		AddItem(a.searchBox, 0, 1, true)
	content := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(a.table, 0, 3, false)
	a.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(top, 1, 0, true).
		AddItem(a.warnings, 1, 0, false).
		AddItem(content, 0, 1, false)
	a.pages.AddPage("main", a.root, true, true)
	a.app.SetFocus(a.searchBox)
	a.app.SetInputCapture(a.handleKey)
	a.refresh()
}

func (a *WorkdashApp) handleKey(event *tcell.EventKey) *tcell.EventKey {
	if a.menu != nil {
		return a.handleMenuKey(event)
	}
	switch event.Key() {
	case tcell.KeyCtrlB:
		a.detailsVisible = !a.detailsVisible
		a.rebuildContent()
		return nil
	case tcell.KeyCtrlR:
		a.reload()
		return nil
	case tcell.KeyCtrlS:
		if a.sortOrder == model.SortRecent {
			a.sortOrder = model.SortDefault
		} else {
			a.sortOrder = model.SortRecent
		}
		a.refreshSelection(false)
		return nil
	case tcell.KeyTab:
		a.nextMode(1)
		return nil
	case tcell.KeyBacktab:
		a.nextMode(-1)
		return nil
	case tcell.KeyUp:
		a.moveSelection(-1)
		return nil
	case tcell.KeyDown:
		a.moveSelection(1)
		return nil
	case tcell.KeyHome:
		a.selectBoundary(false)
		return nil
	case tcell.KeyEnd:
		a.selectBoundary(true)
		return nil
	case tcell.KeyPgUp:
		a.moveSelection(-a.pageStep())
		return nil
	case tcell.KeyPgDn:
		a.moveSelection(a.pageStep())
		return nil
	case tcell.KeyEnter:
		a.openActionMenu()
		return nil
	case tcell.KeyEsc:
		if a.query != "" {
			a.query = ""
			a.searchBox.SetText("")
			a.refresh()
		} else {
			a.selectedAction = actions.NoopAction{}
			a.returnCode = a.noopReturnCode()
			a.app.Stop()
		}
		return nil
	case tcell.KeyF1:
		a.showHelp()
		return nil
	case tcell.KeyF2:
		a.openConfig()
		return nil
	}
	return event
}

func (a *WorkdashApp) refresh() {
	a.refreshSelection(true)
}

func (a *WorkdashApp) refreshSelection(preserveSelection bool) {
	a.updateMode()
	a.updateWarnings()
	a.populateTable(preserveSelection)
	a.syncDetails()
}

func (a *WorkdashApp) reload() {
	a.items = nil
	a.warningLines = nil
	a.loading = true
	a.refresh()
	go func() {
		_ = loader.StreamItems(a.configPath, a.cwd, func(items []model.WorkItem, warnings []string, done bool) {
			a.app.QueueUpdateDraw(func() {
				a.items = items
				a.warningLines = warnings
				a.loading = !done
				a.refreshSelection(false)
			})
		})
	}()
}

func (a *WorkdashApp) updateMode() {
	label := modeLabel(a.mode)
	if a.sortOrder == model.SortRecent {
		label += " Rec"
	}
	a.modeText.SetText("[::bu]" + label + "[-:-:-]")
}

func modeLabel(mode model.Mode) string {
	switch mode {
	case model.ModeAll:
		return "All"
	case model.ModePR:
		return "PR"
	case model.ModeTmux:
		return "TM"
	case model.ModeWorktree:
		return "WT"
	case model.ModeBranch:
		return "BR"
	default:
		return strings.Title(string(mode))
	}
}

func (a *WorkdashApp) updateWarnings() {
	lines := []string{}
	if a.loading {
		lines = append(lines, "Loading...")
	}
	lines = append(lines, a.warningLines...)
	a.warnings.SetText(strings.Join(lines, "\n"))
	height := 1
	if len(lines) > 1 {
		height = len(lines)
	}
	a.root.ResizeItem(a.warnings, height, 0)
}

func (a *WorkdashApp) populateTable(preserve bool) {
	selectedKey := ""
	if preserve {
		if selected := a.selectedItem(); selected != nil {
			selectedKey = selected.Key()
		}
	}
	a.table.Clear()
	if !preserve {
		a.table.SetOffset(0, 0)
		a.table.ScrollToBeginning()
	}
	a.selectedByRow = map[int]*model.WorkItem{}
	entries := search.BuildDisplayEntries(a.items, a.query, a.mode, a.sortOrder)
	maxRightParts := 0
	for _, entry := range entries {
		if entry.Item != nil {
			_, rightParts := SummaryParts(*entry.Item)
			maxRightParts = max(maxRightParts, len(rightParts))
		}
	}
	selectedRow := 0
	for row, entry := range entries {
		if entry.IsHeader() {
			cell := tview.NewTableCell(strings.ToUpper(entry.Label)).SetTextColor(tcell.ColorGray).SetSelectable(false)
			a.table.SetCell(row, 0, cell)
			a.table.SetCell(row, 1, tview.NewTableCell("").SetSelectable(false))
			a.table.SetCell(row, 2, tview.NewTableCell("").SetSelectable(false).SetExpansion(1))
			for column := 3; column < 3+maxRightParts; column++ {
				a.table.SetCell(row, column, tview.NewTableCell("").SetSelectable(false))
			}
			continue
		}
		item := *entry.Item
		left, rightParts := SummaryParts(item)
		host, marker, rest := splitLeftParts(left)
		selectedBaseStyle := tcell.StyleDefault.
			Foreground(tcell.ColorWhite).
			Background(tcell.ColorDarkSlateGray)
		kindStyle := tcell.StyleDefault.
			Foreground(tcell.GetColor(kindColor(item.Kind))).
			Background(tcell.ColorBlack).
			Attributes(tcell.AttrBold)
		selectedKindStyle := tcell.StyleDefault.
			Foreground(tcell.GetColor(kindColor(item.Kind))).
			Background(tcell.ColorDarkSlateGray).
			Attributes(tcell.AttrBold)
		if item.Kind == model.KindWorktree && item.IsPrimary {
			kindStyle = kindStyle.Underline(true)
			selectedKindStyle = selectedKindStyle.Underline(true)
		}
		kindCell := tview.NewTableCell(marker).
			SetStyle(kindStyle).
			SetSelectedStyle(selectedKindStyle)
		a.table.SetCell(row, 0, tview.NewTableCell(host).
			SetStyle(tcell.StyleDefault.Foreground(tcell.GetColor(HostColor(item.HostLabel))).Background(tcell.ColorBlack).Attributes(tcell.AttrBold)).
			SetSelectedStyle(tcell.StyleDefault.Foreground(tcell.GetColor(HostColor(item.HostLabel))).Background(tcell.ColorDarkSlateGray).Attributes(tcell.AttrBold)))
		a.table.SetCell(row, 1, kindCell)
		a.table.SetCell(row, 2, tview.NewTableCell(rest).
			SetText(tview.Escape(rest)).
			SetExpansion(1).
			SetStyle(tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)).
			SetSelectedStyle(selectedBaseStyle))
		for column := 3; column < 3+maxRightParts; column++ {
			a.table.SetCell(row, column, tview.NewTableCell("").
				SetStyle(tcell.StyleDefault.Background(tcell.ColorBlack)).
				SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorDarkSlateGray)))
		}
		firstRightColumn := 3 + maxRightParts - len(rightParts)
		for index, part := range rightParts {
			color := tcell.GetColor(styleColor(part.Color))
			text := formatStyledPart(part)
			if index > 0 {
				text = " " + text
			}
			a.table.SetCell(row, firstRightColumn+index, tview.NewTableCell(text).
				SetStyle(tcell.StyleDefault.Foreground(color).Background(tcell.ColorBlack)).
				SetSelectedStyle(tcell.StyleDefault.Foreground(color).Background(tcell.ColorDarkSlateGray)).
				SetAlign(tview.AlignRight))
		}
		a.selectedByRow[row] = &item
		if selectedKey != "" && item.Key() == selectedKey {
			selectedRow = row
		}
	}
	if len(entries) > 0 {
		a.table.Select(selectedRow, 0)
		if !preserve {
			a.table.SetOffset(0, 0)
			a.table.ScrollToBeginning()
		}
	}
}

func splitLeftParts(parts []string) (string, string, string) {
	host, marker := "", ""
	restParts := []string{}
	if len(parts) > 0 {
		host = parts[0]
	}
	if len(parts) > 1 {
		marker = parts[1]
	}
	if len(parts) > 2 {
		restParts = parts[2:]
	}
	return host, marker, strings.Join(restParts, "  ")
}

func (a *WorkdashApp) selectedItem() *model.WorkItem {
	row, _ := a.table.GetSelection()
	return a.selectedByRow[row]
}

func (a *WorkdashApp) syncDetails() {
	a.details.SetText(Details(a.selectedItem()))
}

func (a *WorkdashApp) moveSelection(delta int) {
	row, _ := a.table.GetSelection()
	next := row + delta
	for next >= 0 && next < a.table.GetRowCount() {
		if a.selectedByRow[next] != nil {
			a.table.Select(next, 0)
			a.syncDetails()
			return
		}
		next += delta
	}
}

func (a *WorkdashApp) selectBoundary(last bool) {
	start, end, step := 0, a.table.GetRowCount(), 1
	if last {
		start, end, step = a.table.GetRowCount()-1, -1, -1
	}
	for row := start; row != end; row += step {
		if a.selectedByRow[row] != nil {
			a.table.Select(row, 0)
			a.syncDetails()
			return
		}
	}
}

func (a *WorkdashApp) pageStep() int {
	_, _, _, height := a.table.GetRect()
	if height <= 2 {
		return 10
	}
	return max(1, height-2)
}

func (a *WorkdashApp) rebuildContent() {
	content := tview.NewFlex().SetDirection(tview.FlexColumn).AddItem(a.table, 0, 3, false)
	if a.detailsVisible {
		content.AddItem(a.details, 42, 0, false)
	}
	a.root.RemoveItem(a.root.GetItem(2))
	a.root.AddItem(content, 0, 1, false)
	a.syncDetails()
}

func (a *WorkdashApp) nextMode(delta int) {
	modes := []model.Mode{model.ModeAll, model.ModeTmux, model.ModeWorktree, model.ModePR, model.ModeBranch}
	idx := 0
	for i, mode := range modes {
		if mode == a.mode {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(modes)) % len(modes)
	a.setMode(modes[idx])
}

func (a *WorkdashApp) setMode(mode model.Mode) {
	a.mode = mode
	a.refreshSelection(false)
}

func (a *WorkdashApp) openActionMenu() {
	item := a.selectedItem()
	if item == nil {
		return
	}
	options := actions.ActionsForItem(*item, a.commandDefinitions, a.terminalLauncher != "", a.terminalLauncherDefault)
	if len(options) == 0 {
		return
	}
	view := tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(true).
		SetScrollable(true).
		SetWrap(false)
	frame := tview.NewFrame(view).
		SetBorders(1, 1, 0, 0, 1, 1)
	frame.SetBorder(true).SetTitle("Actions for " + item.Title).SetTitleAlign(tview.AlignCenter)
	a.menu = &actionMenuState{item: *item, options: options, view: view, frame: frame}
	a.updateMenu()
	a.pages.AddPage("menu", center(frame, 96, a.menuHeight(len(options))), true, true)
}

func (a *WorkdashApp) handleMenuKey(event *tcell.EventKey) *tcell.EventKey {
	menu := a.menu
	switch event.Key() {
	case tcell.KeyEsc:
		if menu.pendingConfirmation != nil {
			menu.pendingConfirmation = nil
			a.updateMenu()
		} else {
			a.closeMenu()
		}
		return nil
	case tcell.KeyUp:
		if menu.pendingConfirmation == nil && menu.selected > 0 {
			menu.selected--
			a.updateMenu()
		}
		return nil
	case tcell.KeyDown:
		if menu.pendingConfirmation == nil && menu.selected < len(menu.options)-1 {
			menu.selected++
			a.updateMenu()
		}
		return nil
	case tcell.KeyHome:
		if menu.pendingConfirmation == nil {
			menu.selected = 0
			a.updateMenu()
		}
		return nil
	case tcell.KeyEnd:
		if menu.pendingConfirmation == nil && len(menu.options) > 0 {
			menu.selected = len(menu.options) - 1
			a.updateMenu()
		}
		return nil
	case tcell.KeyPgUp:
		if menu.pendingConfirmation == nil {
			menu.selected = max(0, menu.selected-8)
			a.updateMenu()
		}
		return nil
	case tcell.KeyPgDn:
		if menu.pendingConfirmation == nil && len(menu.options) > 0 {
			menu.selected = min(len(menu.options)-1, menu.selected+8)
			a.updateMenu()
		}
		return nil
	case tcell.KeyEnter:
		if menu.pendingConfirmation != nil {
			option := *menu.pendingConfirmation
			a.closeMenu()
			a.handleMenuAction(option.Action)
			return nil
		}
		a.activateMenuOption(menu.selected)
		return nil
	}
	r := event.Rune()
	if menu.pendingConfirmation != nil {
		return nil
	}
	if r >= '1' && r <= '9' {
		a.activateMenuOption(int(r - '1'))
		return nil
	}
	for i, option := range menu.options {
		if option.Shortcut != 0 && option.Shortcut == r {
			a.activateMenuOption(i)
			return nil
		}
	}
	return nil
}

func (a *WorkdashApp) activateMenuOption(index int) {
	if index < 0 || index >= len(a.menu.options) {
		return
	}
	option := a.menu.options[index]
	a.menu.selected = index
	if option.Confirmation != nil {
		a.menu.pendingConfirmation = &option
		a.updateMenu()
		return
	}
	a.closeMenu()
	a.handleMenuAction(option.Action)
}

func (a *WorkdashApp) handleMenuAction(action any) {
	switch v := action.(type) {
	case model.ShellAction:
		a.handleSelectedAction(v)
	}
}

func (a *WorkdashApp) updateMenu() {
	menu := a.menu
	if menu == nil {
		return
	}
	if menu.pendingConfirmation != nil {
		p := menu.pendingConfirmation.Confirmation
		menu.view.SetText(fmt.Sprintf("[::b]%s\n\n%s", p.Title, p.Detail))
		a.setMenuHelp(fmt.Sprintf("Enter: %s   Esc: Back", p.ConfirmLabel))
		menu.view.ScrollToBeginning()
		return
	}
	a.setMenuHelp("Enter: Run   1-9: Run by position   Shortcut: Run   Esc: Close")
	a.keepMenuSelectionVisible()
	lines := []string{menu.item.Subtitle, ""}
	for i, option := range menu.options {
		prefix := "  "
		if i == menu.selected {
			prefix = "> "
		}
		index := ""
		if i < 9 {
			index = tview.Escape(fmt.Sprintf("[%d]", i+1)) + " "
		}
		shortcut := ""
		if option.Shortcut != 0 {
			shortcut = tview.Escape(fmt.Sprintf("[%c]", option.Shortcut)) + " "
		}
		suffix := ""
		if option.Confirmation != nil {
			suffix = "..."
		}
		line := fmt.Sprintf(`["opt%d"]%s%s%s%s%s[""]`, i, prefix, index, shortcut, tview.Escape(option.Label), suffix)
		lines = append(lines, line, "    "+tview.Escape(option.Detail), "")
	}
	menu.view.SetText(strings.Join(lines, "\n"))
	menu.view.Highlight(fmt.Sprintf("opt%d", menu.selected))
	menu.view.ScrollTo(menu.scrollOffset, 0)
}

func (a *WorkdashApp) setMenuHelp(text string) {
	if a.menu == nil || a.menu.frame == nil {
		return
	}
	a.menu.frame.Clear()
	a.menu.frame.AddText(text, false, tview.AlignCenter, tcell.ColorWhite)
}

func (a *WorkdashApp) keepMenuSelectionVisible() {
	menu := a.menu
	if menu == nil {
		return
	}
	height := a.menuVisibleRows()
	selectedLine := 2 + menu.selected*3
	if selectedLine < menu.scrollOffset {
		menu.scrollOffset = selectedLine
	} else if selectedLine+2 >= menu.scrollOffset+height {
		menu.scrollOffset = selectedLine + 3 - height
	}
	if menu.scrollOffset < 0 {
		menu.scrollOffset = 0
	}
}

func (a *WorkdashApp) menuVisibleRows() int {
	if a.menu == nil || a.menu.view == nil {
		return 16
	}
	_, _, _, height := a.menu.view.GetRect()
	if height <= 2 {
		return 16
	}
	return max(1, height-2)
}

func (a *WorkdashApp) menuHeight(optionCount int) int {
	desired := min(36, max(14, 7+optionCount*3))
	return desired
}

func (a *WorkdashApp) closeMenu() {
	a.pages.RemovePage("menu")
	a.menu = nil
}

func (a *WorkdashApp) handleSelectedAction(action model.ShellAction) {
	if inline, ok := actions.AsInlineAction(action); ok {
		a.runInlineAction(inline)
		return
	}
	if a.terminalLauncher != "" && actions.IsTerminalLaunchAction(action) {
		go a.runTerminalLaunch(action)
		return
	}
	a.selectedAction = action
	a.returnCode = 0
	a.app.Stop()
}

func (a *WorkdashApp) runInlineAction(action actions.InlineAction) {
	title, detail, _ := actions.DescribeAction(action)
	modal := tview.NewTextView().SetText(title + "\n\n" + detail + "\n\nWorking...").SetDynamicColors(true)
	modal.SetBorder(true)
	a.pages.AddPage("busy", center(modal, 72, 8), true, true)
	go func() {
		err := actions.ExecuteInlineAction(action)
		a.app.QueueUpdateDraw(func() {
			a.pages.RemovePage("busy")
			if err != nil {
				a.inlineFailure++
				a.warningLines = append(a.warningLines, err.Error())
				a.refresh()
				return
			}
			a.inlineSuccess++
			if configured, ok := action.(actions.ConfiguredInlineAction); ok && configured.Run == "background" {
				a.refresh()
				return
			}
			switch action.(type) {
			case actions.OpenInBrowserAction:
				a.refresh()
			default:
				a.reload()
			}
		})
	}()
}

func (a *WorkdashApp) runTerminalLaunch(action model.ShellAction) {
	err := actions.ExecuteTerminalLaunchAction(action, a.terminalLauncher)
	a.app.QueueUpdateDraw(func() {
		if err != nil {
			a.terminalFailure++
			a.warningLines = append(a.warningLines, err.Error())
		} else {
			a.terminalSuccess++
		}
		a.refresh()
	})
}

func (a *WorkdashApp) openConfig() {
	if a.configEditor == "" && os.Getenv("EDITOR") == "" {
		a.warningLines = append(a.warningLines, "Cannot open config: set $EDITOR or configure `config_editor` in workdash")
		a.refresh()
		return
	}
	path := a.configPath
	if path == "" {
		path = config.DefaultConfigPath()
	}
	a.handleSelectedAction(a.preferTerminalAction(actions.EditConfigAction{Path: path, Editor: a.configEditor}))
}

func (a *WorkdashApp) preferTerminalAction(action model.ShellAction) model.ShellAction {
	if a.terminalLauncher == "" || !a.terminalLauncherDefault || actions.TerminalLaunchCommand(action) == "" {
		return action
	}
	return actions.TerminalLaunchAction{Action: action}
}

func (a *WorkdashApp) showHelp() {
	text := strings.Join([]string{
		"Keyboard Shortcuts", "",
		"Navigation",
		"  Up / Down            Move selection",
		"  Enter                Open action menu",
		"  Esc                  Clear search, then exit",
		"",
		"Modes",
		"  Tab / Shift+Tab      Cycle modes",
		"",
		"Actions",
		"  Ctrl+B               Toggle details sidebar",
		"  Ctrl+S               Toggle sort",
		"  Ctrl+R               Refresh dashboard",
		"  F1                   Show this help",
		"  F2                   Open config in editor",
	}, "\n")
	view := tview.NewTextView().SetText(text)
	view.SetBorder(true)
	a.pages.AddPage("help", center(view, 76, 28), true, true)
	old := a.app.GetInputCapture()
	a.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Key() == tcell.KeyEnter || event.Key() == tcell.KeyF1 {
			a.pages.RemovePage("help")
			a.app.SetInputCapture(old)
			return nil
		}
		return nil
	})
}

func (a *WorkdashApp) noopReturnCode() int {
	if a.inlineFailure > 0 || a.terminalFailure > 0 {
		return 1
	}
	if a.inlineSuccess > 0 || a.terminalSuccess > 0 {
		return 0
	}
	return 130
}

func center(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexColumn).
			AddItem(nil, 0, 1, false).
			AddItem(p, width, 0, true).
			AddItem(nil, 0, 1, false), height, 0, true).
		AddItem(nil, 0, 1, false)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func SetTerminalTitle(title string) {
	if os.Getenv("PYTEST_CURRENT_TEST") != "" {
		return
	}
	if fileInfo, _ := os.Stderr.Stat(); fileInfo == nil || (fileInfo.Mode()&os.ModeCharDevice) == 0 {
		return
	}
	seq := terminalTitleSequence(title, os.Getenv("TMUX") != "")
	_, _ = os.Stderr.WriteString(seq)
}

func terminalTitleSequence(title string, insideTmux bool) string {
	osc := "\033]0;" + title + "\007\033]2;" + title + "\007"
	if !insideTmux {
		return osc
	}
	return "\033Ptmux;" + strings.ReplaceAll(osc, "\033", "\033\033") + "\033\\"
}
