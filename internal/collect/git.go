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
	// lstrip=2 drops exactly "refs/heads/", matching how parseWorktrees keys a
	// branch. %(refname:short) would instead give the shortest unambiguous
	// name, so a branch sharing a name with a tag comes back as "heads/v1.0"
	// and never merges with its own worktree.
	branchArgv = []string{"git", "for-each-ref", "--format=%(refname:lstrip=2)", "refs/heads"}
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
	return append(parseWorktrees(worktreeOut, host, repo), parseBranches(branchOut, host, repo)...), nil
}

// parseBranches turns the branch list into records with no worktree facet. A
// branch that exists but is checked out nowhere is still a workspace worth
// acting on — it is exactly the state where "create the worktree" belongs.
func parseBranches(out, host string, repo Repo) []workspace.GitRecord {
	records := []workspace.GitRecord{}
	for _, branch := range strings.Split(out, "\n") {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			continue
		}
		records = append(records, workspace.GitRecord{
			ID:        workspace.ID{Host: host, Repo: repo.Root, Branch: branch},
			RepoLabel: repo.Label,
		})
	}
	return records
}

// parseWorktrees reads `git worktree list --porcelain`. A detached worktree
// yields no record because there is no branch to key a workspace on, but it
// still counts as a block so only the genuinely first worktree is main.
//
// A bare block is different: in a bare layout git lists the repository itself
// first, and it holds no checkout at all. It must not count, or the first real
// worktree would be reported as a secondary one.
func parseWorktrees(out, host string, repo Repo) []workspace.GitRecord {
	records := []workspace.GitRecord{}
	path, head, branch := "", "", ""
	bare := false
	seen := 0
	flush := func() {
		defer func() { path, head, branch, bare = "", "", "", false }()
		if path == "" || bare {
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
		case strings.TrimSpace(line) == "bare":
			bare = true
		}
	}
	flush()
	return records
}
