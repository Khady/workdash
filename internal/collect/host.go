package collect

import (
	"github.com/Khady/workdash/internal/workspace"
)

// CollectHost runs a host's plan through a transport and parses each section
// with the same parsers the local path uses.
//
// The transport is the only thing that differs between local and remote, which
// is what keeps the parsers as the single place command output is interpreted —
// remote collection cannot drift from local collection because there is only one
// implementation of "what does this porcelain mean".
func CollectHost(transport Transport, host Host) ([]workspace.GitRecord, []workspace.SessionRecord, error) {
	sections, err := transport.Run(PlanFor(host))
	if err != nil {
		return nil, nil, err
	}
	git := []workspace.GitRecord{}
	for i, repo := range host.Repos {
		// Both kinds of record carry the full name, because which one creates the
		// workspace depends on state workdash does not control: a checked-out
		// branch is created by its worktree record, an idle one by its branch
		// record, and the pull request has to find either.
		fullName := parseFullName(sections[remoteKey(i)])
		git = append(git, withFullName(parseWorktrees(sections[worktreeKey(i)], host.Label, repo), fullName)...)
		git = append(git, withFullName(parseBranches(sections[branchKey(i)], host.Label, repo), fullName)...)
	}
	return git, parseSessions(sections["tmux"], host.Label), nil
}

func withFullName(records []workspace.GitRecord, fullName string) []workspace.GitRecord {
	for i := range records {
		records[i].RepoFullName = fullName
	}
	return records
}
