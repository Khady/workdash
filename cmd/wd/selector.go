package main

import (
	"fmt"
	"strings"

	"github.com/Khady/workdash/internal/workspace"
)

// Select resolves a selector to exactly one workspace. A selector is a branch
// name, optionally qualified as "repo:branch". Git forbids ":" in ref names, so
// splitting on the first colon is unambiguous even for branches with slashes.
func Select(snap workspace.Snapshot, selector string) (workspace.Workspace, error) {
	repo, branch := "", selector
	if idx := strings.Index(selector, ":"); idx > 0 {
		repo, branch = selector[:idx], selector[idx+1:]
	}
	matches := []workspace.Workspace{}
	for _, ws := range snap.Workspaces {
		if ws.ID.Branch != branch {
			continue
		}
		if repo != "" && ws.RepoLabel != repo {
			continue
		}
		matches = append(matches, ws)
	}
	switch len(matches) {
	case 0:
		return workspace.Workspace{}, fmt.Errorf("no workspace matches %q", selector)
	case 1:
		return matches[0], nil
	}
	names := []string{}
	for _, ws := range matches {
		names = append(names, ws.RepoLabel+":"+ws.ID.Branch)
	}
	return workspace.Workspace{}, fmt.Errorf("%q is ambiguous: %s", selector, strings.Join(names, ", "))
}
