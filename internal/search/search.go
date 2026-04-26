package search

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Khady/workdash/internal/model"
)

var tokenPattern = regexp.MustCompile(`[A-Za-z0-9]+`)
var kindPriority = map[model.ItemKind]int{model.KindTmux: 0, model.KindWorktree: 1, model.KindPR: 2, model.KindBranch: 3}

func FilterItems(items []model.WorkItem, query string, mode model.Mode, sortOrder model.SortOrder) []model.WorkItem {
	relevant := []model.WorkItem{}
	for _, item := range items {
		if matchesMode(item, mode) {
			relevant = append(relevant, item)
		}
	}
	normalized := strings.ToLower(strings.TrimSpace(query))
	if normalized == "" {
		sort.SliceStable(relevant, func(i, j int) bool {
			a, b := relevant[i], relevant[j]
			if sortOrder == model.SortRecent {
				at, bt := float64(1<<62), float64(1<<62)
				if a.LastActivityAt != nil {
					at = -float64(a.LastActivityAt.Unix())
				}
				if b.LastActivityAt != nil {
					bt = -float64(b.LastActivityAt.Unix())
				}
				if at != bt {
					return at < bt
				}
				return strings.ToLower(a.Title) < strings.ToLower(b.Title)
			}
			if a.ScoreHint != b.ScoreHint {
				return a.ScoreHint > b.ScoreHint
			}
			if kindPriority[a.Kind] != kindPriority[b.Kind] {
				return kindPriority[a.Kind] < kindPriority[b.Kind]
			}
			return strings.ToLower(a.Title) < strings.ToLower(b.Title)
		})
		return relevant
	}
	type scored struct {
		score int
		item  model.WorkItem
	}
	scoredItems := []scored{}
	for _, item := range relevant {
		score, ok := scoreQuery(normalized, searchableText(item))
		if ok {
			scoredItems = append(scoredItems, scored{score: score + item.ScoreHint, item: item})
		}
	}
	sort.SliceStable(scoredItems, func(i, j int) bool {
		a, b := scoredItems[i], scoredItems[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if kindPriority[a.item.Kind] != kindPriority[b.item.Kind] {
			return kindPriority[a.item.Kind] < kindPriority[b.item.Kind]
		}
		return strings.ToLower(a.item.Title) < strings.ToLower(b.item.Title)
	})
	out := make([]model.WorkItem, len(scoredItems))
	for i, item := range scoredItems {
		out[i] = item.item
	}
	return out
}

func searchableText(item model.WorkItem) string {
	parts := []string{}
	switch item.Kind {
	case model.KindPR:
		parts = append(parts, item.HostLabel, "PR", firstNonEmpty(item.RepoLabel, item.Title), item.Title, item.PRURL)
	case model.KindTmux:
		parts = append(parts, item.HostLabel, "TM", firstNonEmpty(item.Session, item.Title), item.Path)
		if item.PathMissing {
			parts = append(parts, "missing path")
		}
		if item.TmuxWindows != nil {
			parts = append(parts, strconv.Itoa(*item.TmuxWindows), "windows")
		}
		if item.TmuxAttached != nil {
			parts = append(parts, strconv.Itoa(*item.TmuxAttached), "attached")
		}
		parts = append(parts, item.TmuxWindowNames...)
	case model.KindWorktree:
		parts = append(parts, item.HostLabel, "WT", firstNonEmpty(item.RepoLabel, item.Title), firstNonEmpty(item.Branch, "detached HEAD"), item.Path)
	case model.KindBranch:
		parts = append(parts, item.HostLabel, "BR", firstNonEmpty(item.RepoLabel, item.Title), item.Title)
		if item.Path != "" {
			parts = append(parts, item.Path)
		}
	}
	parts = append(parts, searchableEnrichmentParts(item)...)
	return strings.ToLower(strings.Join(parts, "  "))
}

func searchableEnrichmentParts(item model.WorkItem) []string {
	parts := []string{}
	if item.PRNumber != 0 {
		parts = append(parts, "PR", strconv.Itoa(item.PRNumber), item.PRURL, string(item.PRStatus))
		if item.PRIsDraft {
			parts = append(parts, "draft")
		}
	}
	if item.Upstream != "" {
		parts = append(parts, item.Upstream)
	}
	if item.AheadCount != nil {
		parts = append(parts, "unpushed", strconv.Itoa(*item.AheadCount))
	}
	return parts
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func BuildDisplayEntries(items []model.WorkItem, query string, mode model.Mode, sortOrder model.SortOrder) []model.DisplayEntry {
	filtered := FilterItems(items, query, mode, sortOrder)
	if strings.TrimSpace(query) != "" || mode != model.ModeAll || sortOrder == model.SortRecent {
		out := make([]model.DisplayEntry, len(filtered))
		for i := range filtered {
			item := filtered[i]
			out[i] = model.DisplayEntry{Label: item.Title, Item: &item}
		}
		return out
	}
	entries := []model.DisplayEntry{}
	worktreeLimit := cappedMainWorktreeCount(filtered)
	addSection := func(label string, kind model.ItemKind, limit int) {
		section := []model.WorkItem{}
		for _, item := range filtered {
			if item.Kind == kind {
				section = append(section, item)
			}
		}
		if len(section) == 0 {
			return
		}
		entries = append(entries, model.DisplayEntry{Label: label})
		if limit > 0 && len(section) > limit {
			section = section[:limit]
		}
		for _, item := range section {
			item := item
			entries = append(entries, model.DisplayEntry{Label: item.Title, Item: &item})
		}
	}
	addSection("Tmux", model.KindTmux, 0)
	addSection("Worktrees", model.KindWorktree, worktreeLimit)
	addSection("PRs", model.KindPR, 20)
	addSection("Branches", model.KindBranch, 20)
	return entries
}

func cappedMainWorktreeCount(items []model.WorkItem) int {
	count := 0
	for _, item := range items {
		if item.Kind == model.KindWorktree && item.IsPrimary {
			count++
		}
	}
	if count < 20 {
		return 20
	}
	return count
}

func matchesMode(item model.WorkItem, mode model.Mode) bool {
	switch mode {
	case model.ModeAll:
		return true
	case model.ModePR:
		return item.Kind == model.KindPR
	case model.ModeWorktree:
		return item.Kind == model.KindWorktree
	case model.ModeBranch:
		return item.Kind == model.KindBranch
	case model.ModeTmux:
		return item.Kind == model.KindTmux
	default:
		return true
	}
}

func scoreQuery(query, haystack string) (int, bool) {
	if strings.HasPrefix(haystack, query) {
		return 1200, true
	}
	for _, token := range tokenPattern.FindAllString(haystack, -1) {
		if strings.HasPrefix(token, query) {
			return 1000, true
		}
	}
	if idx := strings.Index(haystack, query); idx >= 0 {
		return 800 - idx, true
	}
	if score, ok := subsequenceScore(query, haystack); ok {
		return 500 + score, true
	}
	return 0, false
}

func subsequenceScore(query, haystack string) (int, bool) {
	qi := 0
	first, last := -1, -1
	for i, ch := range haystack {
		if qi >= len(query) {
			break
		}
		if byte(ch) == query[qi] {
			if first < 0 {
				first = i
			}
			last = i
			qi++
		}
	}
	if qi != len(query) || first < 0 {
		return 0, false
	}
	penalty := last - first - len(query)
	if penalty < 0 {
		penalty = 0
	}
	score := 100 - penalty
	if score < 0 {
		score = 0
	}
	return score, true
}
