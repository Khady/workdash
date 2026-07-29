package collect

import (
	"strings"

	"github.com/Khady/workdash/internal/workspace"
)

type Repo struct {
	Root  string
	Label string
}

var (
	worktreeArgv = []string{"git", "worktree", "list", "--porcelain"}
	branchArgv   = []string{"git", "for-each-ref", "--format=%(refname:short)", "refs/heads"}
)

func Git(runner Runner, host string, repo Repo) ([]workspace.GitRecord, error) {
	worktreeOut, err := runner.Run(repo.Root, worktreeArgv)
	if err != nil {
		return nil, err
	}
	branchOut, err := runner.Run(repo.Root, branchArgv)
	if err != nil {
		return nil, err
	}
	records := parseWorktrees(worktreeOut, host, repo)
	for _, branch := range strings.Split(branchOut, "\n") {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			continue
		}
		records = append(records, workspace.GitRecord{
			ID:        workspace.ID{Host: host, Repo: repo.Root, Branch: branch},
			RepoLabel: repo.Label,
		})
	}
	return records, nil
}

// parseWorktrees reads `git worktree list --porcelain`. A detached worktree
// yields no record because there is no branch to key a workspace on, but it
// still counts as a block so only the genuinely first worktree is main.
func parseWorktrees(out, host string, repo Repo) []workspace.GitRecord {
	records := []workspace.GitRecord{}
	path, head, branch := "", "", ""
	seen := 0
	flush := func() {
		if path == "" {
			return
		}
		isMain := seen == 0
		seen++
		if branch != "" {
			records = append(records, workspace.GitRecord{
				ID:        workspace.ID{Host: host, Repo: repo.Root, Branch: branch},
				RepoLabel: repo.Label,
				Worktree:  &workspace.Worktree{Path: path, Head: head, IsMain: isMain},
			})
		}
		path, head, branch = "", "", ""
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch refs/heads/"):
			branch = strings.TrimPrefix(line, "branch refs/heads/")
		}
	}
	flush()
	return records
}
