package collect

import (
	"strings"
	"testing"
)

func TestPlanForCoversEveryRepoAndTmux(t *testing.T) {
	host := Host{Label: "asia", TmuxEnabled: true, Repos: []Repo{
		{Root: "/srv/a", Label: "a"}, {Root: "/srv/b", Label: "b"},
	}}
	plan := PlanFor(host)
	if len(plan.Sections) != 7 {
		t.Fatalf("sections = %d %+v, want 2 repos x 3 git commands + 1 tmux", len(plan.Sections), plan.Sections)
	}
	keys := map[string]bool{}
	for _, section := range plan.Sections {
		if keys[section.Key] {
			t.Fatalf("duplicate section key %q", section.Key)
		}
		keys[section.Key] = true
	}
	if !keys["tmux"] {
		t.Fatalf("keys = %v, want a tmux section when tmux is enabled", keys)
	}
}

// The remote URL is what gives a workspace its "owner/name", and without it no
// pull request can ever find the repo — so it belongs in the same round trip as
// the worktrees and branches rather than in a separate call per repo.
func TestPlanForAsksEveryRepoForItsRemote(t *testing.T) {
	host := Host{Label: "asia", Repos: []Repo{
		{Root: "/srv/a", Label: "a"}, {Root: "/srv/b", Label: "b"},
	}}
	plan := PlanFor(host)
	for i := range host.Repos {
		found := false
		for _, section := range plan.Sections {
			if section.Key != remoteKey(i) {
				continue
			}
			found = true
			if got := strings.Join(section.Argv, " "); got != "git remote get-url origin" {
				t.Fatalf("remote argv = %q, want %q", got, "git remote get-url origin")
			}
			if section.Cwd != host.Repos[i].Root {
				t.Fatalf("remote section cwd = %q, want %q", section.Cwd, host.Repos[i].Root)
			}
		}
		if !found {
			t.Fatalf("no %q section in %+v", remoteKey(i), plan.Sections)
		}
	}
}

func TestPlanForSkipsTmuxWhenDisabled(t *testing.T) {
	plan := PlanFor(Host{Label: "asia", Repos: []Repo{{Root: "/srv/a", Label: "a"}}})
	for _, section := range plan.Sections {
		if section.Key == "tmux" {
			t.Fatal("tmux section should be absent when tmux is disabled")
		}
	}
}

func TestPlanForRunsGitInTheRepoRoot(t *testing.T) {
	plan := PlanFor(Host{Label: "asia", Repos: []Repo{{Root: "/srv/a", Label: "a"}}})
	for _, section := range plan.Sections {
		if section.Cwd != "/srv/a" {
			t.Fatalf("section %q cwd = %q, want /srv/a", section.Key, section.Cwd)
		}
	}
}
