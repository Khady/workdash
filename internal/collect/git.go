package collect

import (
	"regexp"
	"strconv"
	"strings"
	"time"

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
	// The commit date rides along in the same command: recency ordering is what
	// makes a long list navigable, and asking for it here costs nothing extra.
	branchArgv = []string{"git", "for-each-ref",
		"--format=%(refname:lstrip=2)%09%(committerdate:unix)%09%(upstream:short)%09%(upstream:track)",
		"refs/heads"}
	// The remote is what gives a repo its "owner/name", which is the only thing a
	// pull request can be matched on: GitHub knows nothing about local paths.
	remoteArgv = []string{"git", "remote", "get-url", "origin"}
)

// parseFullName turns a remote URL into "owner/name", or "" when it cannot.
//
// Remotes come in several shapes for the same repository — scp-style ssh,
// ssh:// with userinfo, https, each with or without the .git suffix — and they
// all have to reduce to the one form GitHub reports, or a workspace and its pull
// request never meet. Anything else is reported as unknown rather than guessed
// at: a repo with no origin prints nothing and exits non-zero, which the
// transport files as an empty section, and a wrong full name would attach some
// other repository's pull requests.
func parseFullName(out string) string {
	url := strings.TrimSpace(out)
	if url == "" {
		return ""
	}
	url = strings.TrimSuffix(url, ".git")
	switch {
	case strings.Contains(url, "://"):
		// scheme://[user@]host/owner/name
		_, rest, _ := strings.Cut(url, "://")
		host, path, ok := strings.Cut(rest, "/")
		if !ok || host == "" {
			return ""
		}
		url = path
	case strings.Contains(url, ":"):
		// user@host:owner/name
		_, url, _ = strings.Cut(url, ":")
	default:
		return ""
	}
	owner, name, ok := strings.Cut(strings.Trim(url, "/"), "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return ""
	}
	return owner + "/" + name
}

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
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		branch := strings.TrimSpace(fields[0])
		if branch == "" {
			continue
		}
		record := workspace.GitRecord{
			ID:        workspace.ID{Host: host, Repo: repo.Root, Branch: branch},
			RepoLabel: repo.Label,
		}
		if len(fields) > 1 {
			record.CommittedAt = parseUnixTime(fields[1])
		}
		if len(fields) > 2 {
			record.Upstream = strings.TrimSpace(fields[2])
		}
		if len(fields) > 3 {
			record.Ahead = parseTrack(fields[3], aheadPattern)
			record.Behind = parseTrack(fields[3], behindPattern)
		}
		records = append(records, record)
	}
	return records
}

// parseTrack reads a count out of git's track field, which looks like "[ahead 3]",
// "[behind 5]" or "[ahead 2, behind 5]".
func parseTrack(track string, pattern *regexp.Regexp) int {
	match := pattern.FindStringSubmatch(track)
	if match == nil {
		return 0
	}
	count, err := strconv.Atoi(match[1])
	if err != nil {
		return 0
	}
	return count
}

var (
	aheadPattern  = regexp.MustCompile(`ahead (\d+)`)
	behindPattern = regexp.MustCompile(`behind (\d+)`)
)

// parseUnixTime tolerates an absent or unparseable date: a branch with no
// timestamp is still a branch, it just sorts after the dated ones.
func parseUnixTime(text string) *time.Time {
	seconds, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
	if err != nil || seconds <= 0 {
		return nil
	}
	at := time.Unix(seconds, 0)
	return &at
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
