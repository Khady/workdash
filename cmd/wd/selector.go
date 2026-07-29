package main

import (
	"fmt"
	"strings"

	"github.com/Khady/workdash/internal/workspace"
)

// Select resolves a selector to exactly one workspace. The grammar is
// "[host:][repo:]branch": one, two or three colon-separated parts. Git forbids
// ":" in ref names and config validation forbids it in host and repo labels, so
// splitting on every colon is unambiguous even for branches with slashes.
//
// A two-part selector is read as "repo:branch", never as "host:branch" — that
// keeps the meaning the grammar always had, and leaves host qualification to
// the fully spelled out three-part form.
func Select(snap workspace.Snapshot, selector string) (workspace.Workspace, error) {
	host, repo, branch := "", "", selector
	switch parts := strings.Split(selector, ":"); len(parts) {
	case 2:
		repo, branch = parts[0], parts[1]
	case 3:
		host, repo, branch = parts[0], parts[1], parts[2]
	}
	// More than three parts cannot describe a workspace: branch keeps the
	// colons and so matches nothing, which the not-found error below reports.
	matches := []workspace.Workspace{}
	for _, ws := range snap.Workspaces {
		if ws.ID.Branch != branch {
			continue
		}
		if repo != "" && ws.RepoLabel != repo {
			continue
		}
		if host != "" && ws.ID.Host != host {
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
	// Candidates are fully qualified so each one is a selector the user can type
	// back verbatim; a bare "repo:branch" candidate can itself be ambiguous.
	names := []string{}
	for _, ws := range matches {
		names = append(names, ws.ID.Host+":"+ws.RepoLabel+":"+ws.ID.Branch)
	}
	return workspace.Workspace{}, fmt.Errorf("%q is ambiguous: %s", selector, strings.Join(names, ", "))
}
