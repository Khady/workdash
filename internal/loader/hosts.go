package loader

import (
	"os"
	"os/exec"
	"strings"

	"github.com/Khady/workdash/internal/config"
	"github.com/Khady/workdash/internal/model"
	"github.com/Khady/workdash/internal/source"
)

type HostContext struct {
	Label       string
	SSHTarget   string
	Repos       []RepoConfig
	TmuxEnabled bool
	InsideTmux  bool
	Git         source.GitRunner
	Tmux        source.TmuxRunner
}

type RepoConfig struct {
	Root      string
	Label     string
	GHAccount string
	Remote    bool
}

type HostLoadResult struct {
	Items    []model.WorkItem
	Warnings []string
}

func BuildHostContexts(cfg config.DashboardConfig, cwd string) []HostContext {
	localRepos := make([]RepoConfig, 0, len(cfg.LocalHost.Repos)+1)
	for _, repo := range cfg.LocalHost.Repos {
		localRepos = append(localRepos, RepoConfig{Root: repo.Root, Label: repo.Label, GHAccount: repo.GHAccount})
	}
	localRepos = injectLaunchRepo(localRepos, cwd)

	hosts := []HostContext{{
		Label:       "local",
		Repos:       localRepos,
		TmuxEnabled: cfg.LocalHost.TmuxEnabled,
		InsideTmux:  os.Getenv("TMUX") != "",
		Git:         source.LocalGitRunner{},
		Tmux:        source.LocalTmuxRunner{},
	}}
	for _, host := range cfg.RemoteHosts {
		repos := make([]RepoConfig, 0, len(host.Repos))
		for _, repo := range host.Repos {
			repos = append(repos, RepoConfig{Root: repo.Root, Label: repo.Label, GHAccount: repo.GHAccount, Remote: true})
		}
		runner := source.NewSSHRunner(host.SSHTarget)
		hosts = append(hosts, HostContext{
			Label:       host.Label,
			SSHTarget:   host.SSHTarget,
			Repos:       repos,
			TmuxEnabled: host.TmuxEnabled,
			Git:         runner,
			Tmux:        runner,
		})
	}
	return hosts
}

func injectLaunchRepo(repos []RepoConfig, cwd string) []RepoConfig {
	root := FindGitRoot(cwd)
	if root == "" {
		return repos
	}
	for _, repo := range repos {
		if repo.Root == root {
			return repos
		}
	}
	label := root
	if idx := strings.LastIndex(strings.TrimRight(root, "/"), "/"); idx >= 0 {
		label = strings.TrimRight(root, "/")[idx+1:]
	}
	return append(repos, RepoConfig{Root: root, Label: label})
}

func FindGitRoot(path string) string {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
