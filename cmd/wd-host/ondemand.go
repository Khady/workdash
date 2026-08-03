package main

import (
	"fmt"
	"strings"

	"github.com/Khady/workdash/internal/collect"
	"github.com/Khady/workdash/internal/wdconf"
	"github.com/Khady/workdash/internal/workspace"
)

// fetchPRByNumber is the real lookup, replaced in tests. It is one `gh` call, and
// the only network request the browser path can make beyond the cached searches.
var fetchPRByNumber = func(account, owner, name string, number int) (workspace.PRRecord, error) {
	return collect.PRByNumber(collect.LocalRunner{}, account, owner, name, number)
}

// withPRFromPage adds the pull request on the page when the snapshot has never
// heard of it.
//
// The cached searches cover what you wrote and what you are involved in. They
// cannot cover a pull request you simply navigated to — measured against a real
// colleague's PR, it matched none of author, review-requested, commenter or
// mentions — and "all open pull requests in the repository" is 826 of them, nine
// paginated requests, to make one actionable. So this asks about exactly the one
// being looked at, and only when nothing already answers for it.
//
// A failure is a warning rather than an error: the popup should still show whatever
// the snapshot did have, and "GitHub is unreachable" is not a reason to render
// nothing.
func withPRFromPage(cfg wdconf.Config, snap workspace.Snapshot, canonical string) (workspace.Snapshot, []string) {
	if len(matchingWorkspaces(snap, canonical)) > 0 {
		return snap, nil
	}
	owner, name, number, err := parsePRURL(canonical)
	if err != nil {
		return snap, nil
	}
	// Which clone would host it, and therefore which credential to ask with. No
	// clone means nothing could be created for it anyway, so the call is not worth
	// making: AddPRWorkspaces would drop the record for want of a host.
	host, root, ok := repoHomeFor(snap, owner+"/"+name)
	if !ok {
		return snap, nil
	}
	record, err := fetchPRByNumber(cfg.AccountForRepo(host, root), owner, name, number)
	if err != nil {
		return snap, []string{fmt.Sprintf("%s/%s#%d: %v", owner, name, number, err)}
	}
	return workspace.AddPRWorkspaces(snap, []workspace.PRRecord{record}), nil
}

// repoHomeFor finds a clone of a repository by its GitHub full name. Comparison is
// case-insensitive on purpose: the full name comes from a git remote and the one
// being looked for comes from a URL, which the canonical form lowercased.
func repoHomeFor(snap workspace.Snapshot, fullName string) (host, root string, ok bool) {
	for _, ws := range snap.Workspaces {
		if ws.RepoFullName != "" && strings.EqualFold(ws.RepoFullName, fullName) {
			return ws.ID.Host, ws.ID.Repo, true
		}
	}
	return "", "", false
}
