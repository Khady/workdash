package collect

import "testing"

func TestPlanForCoversEveryRepoAndTmux(t *testing.T) {
	host := Host{Label: "asia", TmuxEnabled: true, Repos: []Repo{
		{Root: "/srv/a", Label: "a"}, {Root: "/srv/b", Label: "b"},
	}}
	plan := PlanFor(host)
	if len(plan.Sections) != 5 {
		t.Fatalf("sections = %d %+v, want 2 repos x 2 git commands + 1 tmux", len(plan.Sections), plan.Sections)
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
