package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/Khady/workdash/internal/action"
	"github.com/Khady/workdash/internal/wdconf"
)

// statusLine is the top right corner: how much of the list you are looking at,
// and how fresh it is. The filtered count is shown as a fraction because "3" on
// its own hides the difference between a precise filter and a broken collector.
func statusLine(visible, total int, loadedAt time.Time, loading bool) string {
	if loadedAt.IsZero() {
		if loading {
			return "loading"
		}
		return ""
	}
	count := fmt.Sprintf("%d", total)
	if visible != total {
		count = fmt.Sprintf("%d/%d", visible, total)
	}
	// A reload keeps the current rows on screen, so the timestamp would be a lie
	// for as long as it takes; say "reloading" instead.
	when := loadedAt.Local().Format("15:04")
	if loading {
		when = "reloading"
	}
	return count + "  " + when
}

// warningsLine flattens the snapshot's warnings onto one line. One line is
// deliberate: a host being unreachable must be visible, but it must not push the
// list around or take space away from it, and every warning is one short
// "source: reason" string by construction.
func warningsLine(warnings []string) string {
	if len(warnings) == 0 {
		return ""
	}
	flattened := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		flattened = append(flattened, strings.Join(strings.Fields(warning), " "))
	}
	return "⚠ " + strings.Join(flattened, "  ·  ")
}

func actionLabel(resolved action.Resolved) string {
	if resolved.Label != "" {
		return resolved.Label
	}
	return resolved.ID
}

// actionDetail spells out what running an entry will actually do. The argv is
// shown because an action's label is chosen by the user and can say anything;
// the command is the part you can check before pressing Enter on something like
// gwt-delete, which does not ask twice.
func actionDetail(resolved action.Resolved) string {
	parts := []string{resolved.Where}
	if command := strings.Join(resolved.Argv, " "); command != "" {
		parts = append(parts, command)
	}
	if resolved.Shell {
		parts = append(parts, "shell")
	}
	// An attach has no command and no directory: it moves you to a session that
	// already exists, so the session name is the only thing worth showing.
	if resolved.Where == wdconf.WhereAttach {
		if resolved.Session != "" {
			parts = append(parts, "session "+resolved.Session)
		}
		return strings.Join(parts, "  ")
	}
	if resolved.Cwd != "" {
		parts = append(parts, "in "+resolved.Cwd)
	}
	return strings.Join(parts, "  ")
}

// menuTitle names the workspace in the fully qualified host:repo:branch form, so
// the title of the menu is also a selector that can be pasted into `wd run`.
func menuTitle(r row) string {
	return r.Host + ":" + r.Repo + ":" + r.Branch
}
