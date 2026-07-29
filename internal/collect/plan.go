package collect

import "fmt"

// Host is one machine to collect from. An empty SSHTarget means the local
// machine, so local and remote differ only in transport — not in what gets
// collected or how it is parsed.
type Host struct {
	Label       string
	SSHTarget   string
	Repos       []Repo
	TmuxEnabled bool
}

// Section is one command whose output is filed under Key.
type Section struct {
	Key  string
	Cwd  string
	Argv []string
}

// Plan is everything a host needs fetched. Describing collection as data rather
// than as a sequence of calls is what lets a remote host run the whole thing in
// one round trip instead of one per command.
type Plan struct {
	Host     Host
	Sections []Section
}

func PlanFor(host Host) Plan {
	sections := []Section{}
	for i, repo := range host.Repos {
		sections = append(sections,
			Section{Key: worktreeKey(i), Cwd: repo.Root, Argv: worktreeArgv},
			Section{Key: branchKey(i), Cwd: repo.Root, Argv: branchArgv},
			Section{Key: remoteKey(i), Cwd: repo.Root, Argv: remoteArgv},
		)
	}
	if host.TmuxEnabled {
		// tmux is per-host, not per-repo, and needs no working directory.
		cwd := ""
		if len(host.Repos) > 0 {
			cwd = host.Repos[0].Root
		}
		sections = append(sections, Section{Key: "tmux", Cwd: cwd, Argv: tmuxArgv})
	}
	return Plan{Host: host, Sections: sections}
}

func worktreeKey(i int) string { return fmt.Sprintf("worktrees/%d", i) }

func branchKey(i int) string { return fmt.Sprintf("branches/%d", i) }

func remoteKey(i int) string { return fmt.Sprintf("remotes/%d", i) }
