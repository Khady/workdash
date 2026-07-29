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
		git = append(git, parseWorktrees(sections[worktreeKey(i)], host.Label, repo)...)
		git = append(git, parseBranches(sections[branchKey(i)], host.Label, repo)...)
	}
	return git, parseSessions(sections["tmux"], host.Label), nil
}
