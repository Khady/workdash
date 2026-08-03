package main

import (
	"fmt"
	"hash/crc32"
	"math"
	"strings"
	"time"

	"github.com/Khady/workdash/internal/workspace"
)

// badge is one short coloured token. Keeping the colour beside the text — rather
// than baking tview markup in — is what makes every derivation in this file a
// pure function the tests can compare.
type badge struct {
	Text  string
	Color string
	// URL makes the text a terminal hyperlink when the terminal supports OSC 8.
	URL string
}

// row is everything one line of the table shows. It is derived once per snapshot
// so that filtering and redrawing never re-walk the workspace model.
type row struct {
	WS      workspace.Workspace
	Host    string
	Repo    string
	Branch  string
	Facets  []badge
	PR      badge
	Session string
	Age     string
	// Unlinked marks a tmux session that matches no worktree workdash knows
	// about. The snapshot reports those rather than hiding them, so the table
	// does too — but they are not workspaces, so no action resolves for them.
	Unlinked bool
	haystack string
}

// hostColor derives a stable hue per host, the same way internal/ui does: the
// set of hosts is not known ahead of time, so a fixed palette would either run
// out or need configuring. Saturation and value are pinned so every host lands
// at the same brightness and no host is unreadable on a dark terminal.
func hostColor(host string) string {
	hue := float64(crc32.ChecksumIEEE([]byte(host))%360) / 360
	r, g, b := hsvToRGB(hue, 0.55, 0.95)
	return fmt.Sprintf("#%02x%02x%02x", int(r*255), int(g*255), int(b*255))
}

// facetBadges renders the facet triple as a fixed three-slot grid: worktree,
// session, pull request, always in that order, with a dot where the facet is
// absent. A fixed grid is the whole point — "worktree,session" as text makes
// every row a different width and forces you to read it, while a grid lets a
// column of dots tell you at a glance which branches have nothing running.
// facetBadges renders worktree and session presence as fixed slots, so a column
// of dots shows at a glance which branches have nothing running. There is no PR
// slot: the PR column already carries the number, and a badge saying "there is a
// PR" next to a column saying "#37654" is the same fact twice.
func facetBadges(ws workspace.Workspace) []badge {
	absent := badge{Text: "·", Color: colorAbsent}
	badges := []badge{absent, absent}
	if ws.Worktree != nil {
		// The repo's main checkout is worth telling apart from a gwt worktree:
		// it is the one you must not delete, and the old TUI underlined it for
		// the same reason.
		text := "W"
		if ws.Worktree.IsMain {
			text = "M"
		}
		badges[0] = badge{Text: text, Color: colorWorktree}
	}
	if len(ws.Sessions) > 0 {
		badges[1] = badge{Text: "S", Color: colorSession}
	}
	return badges
}

// prBadge is the pull request number coloured by state, matching
// internal/ui.prStatusPart: green open, blue draft, purple merged, red closed.
// A state workdash does not recognise reads as open rather than disappearing —
// an unfamiliar state is still a pull request you want to see.
func prBadge(ws workspace.Workspace) badge {
	if ws.PR == nil {
		return badge{}
	}
	color := colorPROpen
	switch strings.ToUpper(ws.PR.State) {
	case "MERGED":
		color = colorPRMerged
	case "CLOSED":
		color = colorPRClosed
	default:
		if ws.PR.Draft {
			color = colorPRDraft
		}
	}
	return badge{Text: fmt.Sprintf("#%d", ws.PR.Number), Color: color, URL: ws.PR.URL}
}

// sessionSummary describes the workspace's tmux state in one cell: the session
// you would land in, how much is open in it, whether someone is already there,
// and a count of the sessions this cell is not showing. Extra sessions are
// counted rather than listed because the count is the actionable part — it is
// the signal that `{session}` picked one of several.
func sessionSummary(ws workspace.Workspace) string {
	if len(ws.Sessions) == 0 {
		return ""
	}
	first := ws.Sessions[0]
	parts := []string{first.Name}
	if first.Windows > 0 {
		parts = append(parts, fmt.Sprintf("%d win", first.Windows))
	}
	if first.Attached {
		parts = append(parts, "attached")
	}
	if extra := len(ws.Sessions) - 1; extra > 0 {
		parts = append(parts, fmt.Sprintf("+%d", extra))
	}
	return strings.Join(parts, "  ")
}

// buildRows keeps the snapshot's order. That order is recency, which is what
// makes a list of hundreds navigable; re-sorting it alphabetically here would
// throw away the one piece of ranking the core already did.
func buildRows(now time.Time, snap workspace.Snapshot) []row {
	rows := make([]row, 0, len(snap.Workspaces)+len(snap.Orphans))
	for _, ws := range snap.Workspaces {
		rows = append(rows, rowFor(now, ws, ws.RepoLabel, false))
	}
	// Orphan sessions come last: they are the least identified thing in the
	// snapshot, and no action resolves for them.
	for _, orphan := range snap.Orphans {
		ws := workspace.Workspace{
			ID:       workspace.ID{Host: orphan.Host, Branch: orphan.Session.Name},
			Sessions: []workspace.Session{orphan.Session},
		}
		rows = append(rows, rowFor(now, ws, "(unlinked)", true))
	}
	return rows
}

func rowFor(now time.Time, ws workspace.Workspace, repo string, unlinked bool) row {
	session := sessionSummary(ws)
	return row{
		WS:       ws,
		Host:     ws.ID.Host,
		Repo:     repo,
		Branch:   ws.ID.Branch,
		Facets:   facetBadges(ws),
		PR:       prBadge(ws),
		Session:  session,
		Age:      formatAge(now, ws.CommittedAt),
		Unlinked: unlinked,
		haystack: strings.ToLower(strings.Join([]string{ws.ID.Host, repo, ws.ID.Branch, sessionNames(ws)}, " ")),
	}
}

func sessionNames(ws workspace.Workspace) string {
	names := make([]string, 0, len(ws.Sessions))
	for _, session := range ws.Sessions {
		names = append(names, session.Name)
	}
	return strings.Join(names, " ")
}

// matchesFilter is an AND over whitespace-separated terms, each a
// case-insensitive substring of host, repo, branch and session names. Terms
// rather than one string because the useful query is two coordinates at once —
// "asia dune" — and the fields they match are in no fixed order on the line.
// view is one of the old dashboard's Tab-cycled modes, expressed as facet
// requirements: those modes were always "show me the things that have X".
type view struct {
	Label string
	Needs []string
	// Unlinked says whether sessions with no workspace belong in this view. They
	// are sessions, so All and Tmux show them; they are not branches, so the
	// worktree, PR and branch views do not.
	Unlinked bool
}

var views = []view{
	{Label: "All", Unlinked: true},
	{Label: "Tmux", Needs: []string{workspace.FacetSession}, Unlinked: true},
	{Label: "Worktrees", Needs: []string{workspace.FacetWorktree}},
	{Label: "PRs", Needs: []string{workspace.FacetPR}},
	// "Branches" in the old dashboard meant the ones checked out nowhere, which is
	// facet absence rather than a kind of its own.
	{Label: "Branches", Needs: []string{"!" + workspace.FacetWorktree}},
}

func nextView(current, delta int) int {
	return ((current+delta)%len(views) + len(views)) % len(views)
}

func matchesView(r row, v view) bool {
	if r.Unlinked {
		return v.Unlinked
	}
	return workspace.Matches(r.WS, v.Needs)
}

func matchesFilter(r row, query string) bool {
	for _, term := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(r.haystack, term) {
			return false
		}
	}
	return true
}

func filterRows(rows []row, query string, viewIndex int) []row {
	v := views[0]
	if viewIndex >= 0 && viewIndex < len(views) {
		v = views[viewIndex]
	}
	matched := make([]row, 0, len(rows))
	for _, r := range rows {
		if matchesView(r, v) && matchesFilter(r, query) {
			matched = append(matched, r)
		}
	}
	return matched
}

// renderBadges turns badges into tview markup. Empty badges render as nothing,
// so the same function formats a three-slot facet grid and a lone PR number.
func renderBadges(badges []badge) string {
	parts := []string{}
	for _, item := range badges {
		if item.Text == "" {
			continue
		}
		parts = append(parts, "["+item.Color+"::b]"+item.Text+"[-:-:-]")
	}
	return strings.Join(parts, " ")
}

func hsvToRGB(h, s, v float64) (float64, float64, float64) {
	i := math.Floor(h * 6)
	f := h*6 - i
	p := v * (1 - s)
	q := v * (1 - f*s)
	t := v * (1 - (1-f)*s)
	switch int(i) % 6 {
	case 0:
		return v, t, p
	case 1:
		return q, v, p
	case 2:
		return p, v, t
	case 3:
		return p, q, v
	case 4:
		return t, p, v
	default:
		return v, p, q
	}
}
