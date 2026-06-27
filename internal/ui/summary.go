package ui

import (
	"fmt"
	"hash/crc32"
	"math"
	"strings"
	"time"

	"github.com/rivo/tview"

	"github.com/Khady/workdash/internal/model"
)

func PlainItemSummary(item model.WorkItem) string {
	left, right := SummaryParts(item)
	parts := append([]string{}, left...)
	for _, part := range right {
		parts = append(parts, part.Text)
	}
	return strings.Join(parts, "  ")
}

func SearchableText(item model.WorkItem) string {
	return strings.ToLower(PlainItemSummary(item))
}

type StyledPart struct {
	Text  string
	Color string
	URL   string
}

func SummaryParts(item model.WorkItem) ([]string, []StyledPart) {
	prefix := map[model.ItemKind]string{model.KindPR: "PR", model.KindWorktree: "WT", model.KindBranch: "BR", model.KindTmux: "TM"}[item.Kind]
	if item.Kind == model.KindPR {
		left := []string{item.HostLabel, prefix, firstNonEmpty(item.RepoLabel, "pull request"), firstNonEmpty(item.Title, fmt.Sprintf("#%d", item.PRNumber))}
		return left, gitStatusParts(item)
	}
	if item.Kind == model.KindTmux {
		left := []string{item.HostLabel, prefix, firstNonEmpty(item.Session, item.Title)}
		right := []StyledPart{}
		if item.PathMissing {
			right = append(right, StyledPart{Text: "missing path", Color: "red"})
		}
		if pr := prStatusPart(item); pr.Text != "" {
			right = append(right, pr)
		}
		if item.TmuxWindows != nil {
			right = append(right, StyledPart{Text: fmt.Sprintf("%d windows", *item.TmuxWindows), Color: "gray"})
		}
		if item.TmuxAttached != nil {
			right = append(right, StyledPart{Text: fmt.Sprintf("%d attached", *item.TmuxAttached), Color: "gray"})
		}
		return left, right
	}
	left := []string{item.HostLabel, prefix, firstNonEmpty(item.RepoLabel, item.Title)}
	if item.Kind == model.KindWorktree {
		left = append(left, firstNonEmpty(item.Branch, "detached HEAD"))
		return left, gitStatusParts(item)
	}
	left = append(left, item.Title)
	if item.Path != "" {
		left = append(left, item.Path)
	}
	return left, gitStatusParts(item)
}

func FormatRow(item model.WorkItem) (string, string) {
	left, right := SummaryParts(item)
	rightText := []string{}
	for _, part := range right {
		rightText = append(rightText, part.Text)
	}
	return strings.Join(left, "  "), strings.Join(rightText, "  ")
}

func FormatRowStyled(item model.WorkItem) (string, string) {
	left, right := SummaryParts(item)
	leftParts := []string{}
	for index, part := range left {
		escaped := tview.Escape(part)
		switch index {
		case 0:
			leftParts = append(leftParts, colorTag(HostColor(item.HostLabel), true, false)+escaped+plainTag())
		case 1:
			leftParts = append(leftParts, colorTag(kindColor(item.Kind), true, item.Kind == model.KindWorktree && item.IsPrimary)+escaped+plainTag())
		default:
			leftParts = append(leftParts, plainTag()+escaped)
		}
	}
	rightParts := []string{}
	for _, part := range right {
		rightParts = append(rightParts, colorTag(styleColor(part.Color), true, false)+tview.Escape(part.Text)+plainTag())
	}
	return strings.Join(leftParts, "  "), strings.Join(rightParts, "  ")
}

func FormatStyledParts(parts []StyledPart) string {
	out := []string{}
	for _, part := range parts {
		out = append(out, formatStyledPart(part))
	}
	return strings.Join(out, "  ")
}

func Details(item *model.WorkItem) string {
	if item == nil {
		return "No results"
	}
	lines := []string{"Type: " + string(item.Kind)}
	if item.RepoLabel != "" {
		lines = append(lines, "Repo: "+item.RepoLabel)
	}
	if item.RepoRoot != "" {
		lines = append(lines, "Repo root: "+item.RepoRoot)
	}
	if item.Path != "" {
		lines = append(lines, "Path: "+item.Path)
	}
	if item.PathMissing {
		lines = append(lines, "Path missing: yes")
	}
	if item.Branch != "" {
		lines = append(lines, "Branch: "+item.Branch)
	}
	if item.Kind == model.KindPR && item.Title != "" {
		lines = append(lines, "Title: "+item.Title)
	}
	if rel := FormatCoarseRelativeTime(item.LastActivityAt); rel != "" {
		lines = append(lines, "Last activity: "+rel+" ago")
	}
	if item.Upstream != "" {
		lines = append(lines, "Upstream: "+item.Upstream)
	}
	if item.AheadCount != nil {
		lines = append(lines, fmt.Sprintf("Unpushed commits: %d", *item.AheadCount))
	}
	if item.PRNumber != 0 {
		label := fmt.Sprintf("PR: #%d", item.PRNumber)
		statuses := []string{}
		if item.PRStatus != "" {
			statuses = append(statuses, string(item.PRStatus))
		}
		if item.PRIsDraft {
			statuses = append(statuses, "draft")
		}
		if len(statuses) > 0 {
			label += " (" + strings.Join(statuses, ", ") + ")"
		}
		lines = append(lines, label)
	}
	if item.PRURL != "" {
		lines = append(lines, "PR URL: "+hyperlink(item.PRURL, item.PRURL))
	}
	if item.Session != "" {
		lines = append(lines, "Session: "+item.Session)
	}
	if len(item.TmuxWindowNames) > 0 {
		lines = append(lines, "Windows:")
		for _, window := range item.TmuxWindowNames {
			lines = append(lines, "  "+window)
		}
	}
	action := "cancel"
	if item.Action != nil && item.Action.ToShell() != "" {
		action = item.Action.ToShell()
	}
	lines = append(lines, "Action: "+action)
	return strings.Join(lines, "\n")
}

func HostColor(host string) string {
	hue := float64(crc32.ChecksumIEEE([]byte(host))%360) / 360
	r, g, b := hsvToRGB(hue, 0.55, 0.95)
	return fmt.Sprintf("#%02x%02x%02x", int(r*255), int(g*255), int(b*255))
}

func kindColor(kind model.ItemKind) string {
	return map[model.ItemKind]string{
		model.KindPR:       "#f43f5e",
		model.KindWorktree: "#34d399",
		model.KindBranch:   "#60a5fa",
		model.KindTmux:     "#f59e0b",
	}[kind]
}

func colorTag(color string, bold bool, underline bool) string {
	attrs := ""
	if bold {
		attrs += "b"
	}
	if underline {
		attrs += "u"
	}
	return "[" + color + "::" + attrs + "]"
}

func plainTag() string {
	return "[white::-]"
}

func styleColor(name string) string {
	switch name {
	case "green":
		return "#22c55e"
	case "red":
		return "#ef4444"
	case "purple":
		return "#a855f7"
	case "blue":
		return "#60a5fa"
	case "yellow":
		return "#f59e0b"
	case "gray":
		return "#9ca3af"
	default:
		return "#9ca3af"
	}
}

func gitStatusParts(item model.WorkItem) []StyledPart {
	parts := []StyledPart{}
	if item.AheadCount != nil && *item.AheadCount > 0 {
		parts = append(parts, StyledPart{Text: fmt.Sprintf("+%d unpushed", *item.AheadCount), Color: "yellow"})
	}
	if item.Kind == model.KindWorktree && item.HasTmuxSession {
		parts = append(parts, StyledPart{Text: "tmux", Color: "yellow"})
	}
	if pr := prStatusPart(item); pr.Text != "" {
		parts = append(parts, pr)
	}
	if rel := FormatCoarseRelativeTime(item.LastActivityAt); rel != "" {
		parts = append(parts, StyledPart{Text: rel, Color: "gray"})
	}
	return parts
}

func prStatusPart(item model.WorkItem) StyledPart {
	if item.PRNumber == 0 {
		return StyledPart{}
	}
	label := fmt.Sprintf("#%d", item.PRNumber)
	color := "green"
	if item.PRStatus == model.PRMerged {
		color = "purple"
	} else if item.PRStatus == model.PRClosed {
		color = "red"
	} else if item.PRIsDraft {
		color = "blue"
	}
	return StyledPart{Text: label, Color: color, URL: item.PRURL}
}

func formatStyledPart(part StyledPart) string {
	text := tview.Escape(part.Text)
	if part.URL != "" {
		text = hyperlink(part.URL, text)
	}
	return colorTag(styleColor(part.Color), true, false) + text + plainTag()
}

func hyperlink(url, text string) string {
	return "[:::" + url + "]" + text + "[:::-]"
}

func FormatCoarseRelativeTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	d := time.Since(*t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 10*7*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	default:
		return fmt.Sprintf("%dmo", int(math.Max(1, d.Hours()/(24*30))))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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
