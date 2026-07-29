package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Khady/workdash/internal/action"
	"github.com/Khady/workdash/internal/launch"
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

func TestExecutorForPicksLocalOrRemoteByHost(t *testing.T) {
	cfg := wdconf.Config{Hosts: []wdconf.Host{
		{Label: "asia", SSHTarget: "me@asia", AttachArgv: []string{"mosh", "me@asia"}},
	}}
	local, err := executorFor(cfg, wdconf.LocalHost)
	if err != nil {
		t.Fatalf("executorFor(local) error = %v", err)
	}
	if _, ok := local.(launch.Tmux); !ok {
		t.Fatalf("local executor = %T, want launch.Tmux", local)
	}
	remote, err := executorFor(cfg, "asia")
	if err != nil {
		t.Fatalf("executorFor(asia) error = %v", err)
	}
	got, ok := remote.(launch.Remote)
	if !ok {
		t.Fatalf("remote executor = %T, want launch.Remote", remote)
	}
	if got.Target != "me@asia" || len(got.Attach) != 2 {
		t.Fatalf("remote executor = %+v, want the host's target and attach argv", got)
	}
}

func TestExecutorForRejectsUnknownHost(t *testing.T) {
	if _, err := executorFor(wdconf.Config{}, "nowhere"); err == nil {
		t.Fatal("executorFor() error = nil, want an error naming the unknown host")
	}
}

func TestCollectPRsQueriesEachAccountOnceAndMergesThem(t *testing.T) {
	asked := []string{}
	records, warnings := collectPRs([]string{"acct-a", "acct-b"}, func(account string) ([]workspace.PRRecord, error) {
		asked = append(asked, account)
		return []workspace.PRRecord{{FullName: "o/" + account, Branch: "main", PR: workspace.PR{Number: 1}}}, nil
	})
	if len(asked) != 2 {
		t.Fatalf("asked = %v, want one call per account", asked)
	}
	if len(records) != 2 {
		t.Fatalf("records = %+v, want both accounts merged", records)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
}

// GitHub being down must cost the pr facet, not the snapshot.
func TestCollectPRsWarnsPerAccountAndKeepsTheRest(t *testing.T) {
	records, warnings := collectPRs([]string{"broken", "fine"}, func(account string) ([]workspace.PRRecord, error) {
		if account == "broken" {
			return nil, errors.New("HTTP 401: Bad credentials")
		}
		return []workspace.PRRecord{{FullName: "o/r", Branch: "main", PR: workspace.PR{Number: 2}}}, nil
	})
	if len(records) != 1 || records[0].PR.Number != 2 {
		t.Fatalf("records = %+v, want the working account's PRs", records)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "broken") {
		t.Fatalf("warnings = %v, want one naming the failed account", warnings)
	}
}

func TestCollectPRsWithNoAccountsStillQueriesAmbientAuth(t *testing.T) {
	asked := []string{}
	if _, warnings := collectPRs(nil, func(account string) ([]workspace.PRRecord, error) {
		asked = append(asked, account)
		return nil, nil
	}); len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(asked) != 1 || asked[0] != "" {
		t.Fatalf("asked = %q, want a single ambient-auth call", asked)
	}
}

func TestPRPolicyServesAFreshEntryWithoutRefreshing(t *testing.T) {
	decision := prPolicy(true, 5*time.Minute, wdconf.DefaultPRCacheTTL, wdconf.DefaultPRRefreshAfter)
	if !decision.useCache || decision.refreshInBackground || decision.fetchNow {
		t.Fatalf("decision = %+v, want cache only", decision)
	}
}

// Past the soft limit the list still renders instantly and the refetch happens
// behind the user's back — the popup must never block on GitHub.
func TestPRPolicyServesStaleAndRefreshesInBackground(t *testing.T) {
	decision := prPolicy(true, 30*time.Minute, wdconf.DefaultPRCacheTTL, wdconf.DefaultPRRefreshAfter)
	if !decision.useCache || !decision.refreshInBackground || decision.fetchNow {
		t.Fatalf("decision = %+v, want stale cache plus a background refresh", decision)
	}
}

func TestPRPolicyBlocksOnlyBeyondTheHardLimit(t *testing.T) {
	decision := prPolicy(true, 48*time.Hour, wdconf.DefaultPRCacheTTL, wdconf.DefaultPRRefreshAfter)
	if decision.useCache || !decision.fetchNow {
		t.Fatalf("decision = %+v, want a blocking fetch", decision)
	}
}

func TestPRPolicyFetchesWhenThereIsNoCache(t *testing.T) {
	decision := prPolicy(false, 0, wdconf.DefaultPRCacheTTL, wdconf.DefaultPRRefreshAfter)
	if decision.useCache || !decision.fetchNow {
		t.Fatalf("decision = %+v, want a blocking fetch on a cold cache", decision)
	}
}
