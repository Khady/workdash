package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/Khady/workdash/internal/action"
	"github.com/Khady/workdash/internal/wdconf"
	"github.com/Khady/workdash/internal/workspace"
)

type fakeExecutor struct {
	pane    []action.Resolved
	capture []action.Resolved
	err     error
}

func (f *fakeExecutor) Pane(resolved action.Resolved) (string, error) {
	f.pane = append(f.pane, resolved)
	return "paned", f.err
}

func (f *fakeExecutor) Capture(resolved action.Resolved) (string, error) {
	f.capture = append(f.capture, resolved)
	return "captured", f.err
}

func TestExecuteRoutesPaneAndCaptureToTheRightVerb(t *testing.T) {
	executor := &fakeExecutor{}
	if _, err := execute(executor, action.Resolved{ID: "agent", Where: wdconf.WherePane}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if _, err := execute(executor, action.Resolved{ID: "status", Where: wdconf.WhereCapture}); err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if len(executor.pane) != 1 || executor.pane[0].ID != "agent" {
		t.Fatalf("pane calls = %+v, want one for agent", executor.pane)
	}
	if len(executor.capture) != 1 || executor.capture[0].ID != "status" {
		t.Fatalf("capture calls = %+v, want one for status", executor.capture)
	}
}

func TestExecuteReturnsExecutorFailure(t *testing.T) {
	executor := &fakeExecutor{err: errors.New("no server running")}
	if _, err := execute(executor, action.Resolved{ID: "agent", Where: wdconf.WherePane}); err == nil {
		t.Fatal("execute() error = nil, want the executor failure")
	}
}

func TestSnapshotFailedReportsTotalCollectorFailure(t *testing.T) {
	if !snapshotFailed(0, []string{"repo: fatal: not a git repository"}) {
		t.Fatal("an empty snapshot with warnings is a failure, not an empty list")
	}
	if snapshotFailed(0, nil) {
		t.Fatal("an empty snapshot with no warnings is a legitimately empty list")
	}
	if snapshotFailed(3, []string{"tmux: permission denied"}) {
		t.Fatal("a partial snapshot is a warning, not a failure")
	}
}

func TestPrintSnapshotListsFacetsAndSessions(t *testing.T) {
	var out strings.Builder
	printSnapshot(&out, snapshot())
	text := out.String()
	if !strings.Contains(text, "topic") || !strings.Contains(text, "solo") {
		t.Fatalf("output = %q, want every workspace listed", text)
	}
	if strings.Contains(text, "\t") {
		t.Fatalf("output = %q, want aligned columns rather than tabs", text)
	}
}

func TestHostsFromConfigPutsLocalFirst(t *testing.T) {
	cfg := wdconf.Config{
		Repos: []wdconf.Repo{{Root: "/local/a", Label: "a"}},
		Hosts: []wdconf.Host{{Label: "asia", SSHTarget: "me@asia", TmuxEnabled: true,
			Repos: []wdconf.Repo{{Root: "/srv/b", Label: "b"}}}},
	}
	hosts := hostsFromConfig(cfg)
	if len(hosts) != 2 {
		t.Fatalf("hosts = %+v, want local and asia", hosts)
	}
	if hosts[0].Label != wdconf.LocalHost || hosts[0].SSHTarget != "" {
		t.Fatalf("first host = %+v, want local with no ssh target", hosts[0])
	}
	if hosts[1].Label != "asia" || hosts[1].SSHTarget != "me@asia" || !hosts[1].TmuxEnabled {
		t.Fatalf("second host = %+v", hosts[1])
	}
	if len(hosts[1].Repos) != 1 || hosts[1].Repos[0].Root != "/srv/b" {
		t.Fatalf("asia repos = %+v", hosts[1].Repos)
	}
}

// A VPN drop should degrade one section of the list, not erase the machine in
// front of you.
func TestMergeHostResultsKeepsWorkingHostsWhenOneFails(t *testing.T) {
	good := []workspace.GitRecord{{ID: workspace.ID{Host: "local", Repo: "/a", Branch: "main"}, RepoLabel: "a"}}
	snap, warnings := mergeHostResults([]hostResult{
		{host: "local", git: good},
		{host: "asia", err: errors.New("ssh: connect to host asia port 22: Network is unreachable")},
	})
	if len(snap.Workspaces) != 1 {
		t.Fatalf("workspaces = %+v, want the local one kept", snap.Workspaces)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "asia") {
		t.Fatalf("warnings = %v, want exactly one naming asia", warnings)
	}
}

func TestMergeHostResultsIndexesEveryHostTogether(t *testing.T) {
	snap, warnings := mergeHostResults([]hostResult{
		{host: "local", git: []workspace.GitRecord{{ID: workspace.ID{Host: "local", Repo: "/a", Branch: "main"}, RepoLabel: "a"}}},
		{host: "asia", git: []workspace.GitRecord{{ID: workspace.ID{Host: "asia", Repo: "/srv/b", Branch: "develop"}, RepoLabel: "b"}},
			sessions: []workspace.SessionRecord{{Host: "asia", Session: workspace.Session{Name: "work", Path: "/srv/b"}}}},
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(snap.Workspaces) != 2 {
		t.Fatalf("workspaces = %+v, want one per host", snap.Workspaces)
	}
	if snap.Workspaces[0].ID.Host != "asia" {
		t.Fatalf("first host = %q, want asia sorted before local", snap.Workspaces[0].ID.Host)
	}
}
