package loader

import (
	"testing"

	"github.com/Khady/workdash/internal/model"
	"github.com/Khady/workdash/internal/source"
)

func TestSearchAccountsByHostPrefersLocalForSharedAccounts(t *testing.T) {
	hosts := []HostContext{
		{
			Label: "local",
			Repos: []RepoConfig{
				{GHAccount: "work"},
				{GHAccount: "secondary"},
			},
		},
		{
			Label:     "asia",
			SSHTarget: "me@asia",
			Repos: []RepoConfig{
				{GHAccount: "work"},
				{GHAccount: "secondary"},
				{GHAccount: "remote-only"},
			},
		},
	}

	got := searchAccountsByHost(hosts)

	local := got["local|"]
	if len(local) != 2 || local[0] != "work" || local[1] != "secondary" {
		t.Fatalf("unexpected local accounts: %#v", local)
	}
	asia := got["asia|me@asia"]
	if len(asia) != 1 || asia[0] != "remote-only" {
		t.Fatalf("unexpected asia accounts: %#v", asia)
	}
}

func TestSearchAccountsByHostKeepsDefaultSearchWhenNoAccountsConfigured(t *testing.T) {
	hosts := []HostContext{{Label: "asia", SSHTarget: "me@asia"}}

	got := searchAccountsByHost(hosts)
	asia := got["asia|me@asia"]
	if len(asia) != 1 || asia[0] != "" {
		t.Fatalf("unexpected default search accounts: %#v", asia)
	}
}

func TestLoadPipelineLetsTmuxInheritPREnrichment(t *testing.T) {
	items := []model.WorkItem{
		{Kind: model.KindWorktree, RepoRoot: "/repo", RepoLabel: "repo", RepoFullName: "example/repo", Path: "/repo/wt", Branch: "feature/fix"},
		{Kind: model.KindPR, RepoLabel: "example/repo", RepoFullName: "example/repo", Branch: "feature/fix", PRNumber: 42, PRURL: "https://github.com/example/repo/pull/42", PRStatus: model.PROpen},
		{Kind: model.KindTmux, Session: "main", Path: "/repo/wt"},
	}

	got := source.EnrichTmuxItems(source.EnrichPRItems(items))
	if got[2].PRNumber != 42 || got[2].PRURL == "" || got[2].PRStatus != model.PROpen {
		t.Fatalf("unexpected tmux item: %#v", got[2])
	}
	if got[0].PRNumber != 42 || got[0].PRURL == "" {
		t.Fatalf("unexpected worktree item: %#v", got[0])
	}
}
