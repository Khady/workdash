package snapshot

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Khady/workdash/internal/collect"
	"github.com/Khady/workdash/internal/wdconf"
	"github.com/Khady/workdash/internal/workspace"
)

func TestHostsFromConfigPutsLocalFirst(t *testing.T) {
	cfg := wdconf.Config{
		Repos: []wdconf.Repo{{Root: "/local/a", Label: "a"}},
		Hosts: []wdconf.Host{{Label: "asia", SSHTarget: "me@asia", TmuxEnabled: true,
			Repos: []wdconf.Repo{{Root: "/srv/b", Label: "b"}}}},
	}
	hosts := HostsFromConfig(cfg)
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
	snap, warnings := MergeHostResults([]HostResult{
		{Host: "local", Git: good},
		{Host: "asia", Err: errors.New("ssh: connect to host asia port 22: Network is unreachable")},
	})
	if len(snap.Workspaces) != 1 {
		t.Fatalf("workspaces = %+v, want the local one kept", snap.Workspaces)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "asia") {
		t.Fatalf("warnings = %v, want exactly one naming asia", warnings)
	}
}

func TestMergeHostResultsIndexesEveryHostTogether(t *testing.T) {
	snap, warnings := MergeHostResults([]HostResult{
		{Host: "local", Git: []workspace.GitRecord{{ID: workspace.ID{Host: "local", Repo: "/a", Branch: "main"}, RepoLabel: "a"}}},
		{Host: "asia", Git: []workspace.GitRecord{{ID: workspace.ID{Host: "asia", Repo: "/srv/b", Branch: "develop"}, RepoLabel: "b"}},
			Sessions: []workspace.SessionRecord{{Host: "asia", Session: workspace.Session{Name: "work", Path: "/srv/b"}}}},
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

func TestCollectPRsQueriesEachAccountOnceAndMergesThem(t *testing.T) {
	// The accounts are fetched concurrently — that is the point, since each one is a
	// network round trip — so the fake needs a lock. Without it the test raced with
	// itself and lost one of the two appends.
	var mu sync.Mutex
	asked := []string{}
	records, warnings := CollectPRs([]string{"acct-a", "acct-b"}, func(account string) ([]workspace.PRRecord, error) {
		mu.Lock()
		asked = append(asked, account)
		mu.Unlock()
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
	records, warnings := CollectPRs([]string{"broken", "fine"}, func(account string) ([]workspace.PRRecord, error) {
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
	if _, warnings := CollectPRs(nil, func(account string) ([]workspace.PRRecord, error) {
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
	decision := Policy(true, 5*time.Minute, wdconf.DefaultPRCacheTTL, wdconf.DefaultPRRefreshAfter)
	if !decision.UseCache || decision.RefreshInBackground || decision.FetchNow {
		t.Fatalf("decision = %+v, want cache only", decision)
	}
}

// Past the soft limit the list still renders instantly and the refetch happens
// behind the user's back — the popup must never block on GitHub.
func TestPRPolicyServesStaleAndRefreshesInBackground(t *testing.T) {
	decision := Policy(true, 30*time.Minute, wdconf.DefaultPRCacheTTL, wdconf.DefaultPRRefreshAfter)
	if !decision.UseCache || !decision.RefreshInBackground || decision.FetchNow {
		t.Fatalf("decision = %+v, want stale cache plus a background refresh", decision)
	}
}

func TestPRPolicyBlocksOnlyBeyondTheHardLimit(t *testing.T) {
	decision := Policy(true, 48*time.Hour, wdconf.DefaultPRCacheTTL, wdconf.DefaultPRRefreshAfter)
	if decision.UseCache || !decision.FetchNow {
		t.Fatalf("decision = %+v, want a blocking fetch", decision)
	}
}

func TestPRPolicyFetchesWhenThereIsNoCache(t *testing.T) {
	decision := Policy(false, 0, wdconf.DefaultPRCacheTTL, wdconf.DefaultPRRefreshAfter)
	if decision.UseCache || !decision.FetchNow {
		t.Fatalf("decision = %+v, want a blocking fetch on a cold cache", decision)
	}
}

// RefreshPRs is what `wd refresh` and the browser host's refresh button both do.
// It is here rather than in either frontend so the two cannot drift into caching
// different things.
func TestRefreshPRsStoresEveryAccountAndWarnsPerFailure(t *testing.T) {
	cfg := wdconf.Config{
		Repos: []wdconf.Repo{{Root: "/a", Label: "a", GHAccount: "work"}},
		Hosts: []wdconf.Host{{Label: "asia", Repos: []wdconf.Repo{{Root: "/b", Label: "b", GHAccount: "personal"}}}},
	}
	cache := collect.PRCache{Dir: t.TempDir(), TTL: time.Hour, Now: time.Now}
	fetched, warnings := RefreshPRs(cfg, cache, func(account string) ([]workspace.PRRecord, error) {
		if account == "personal" {
			return nil, errors.New("gh: HTTP 401")
		}
		return []workspace.PRRecord{
			{FullName: "o/r", Branch: "topic", PR: workspace.PR{Number: 1}},
			{FullName: "o/r", Branch: "other", PR: workspace.PR{Number: 2}},
		}, nil
	})
	if fetched != 2 {
		t.Fatalf("fetched = %d, want the two pull requests the working account returned", fetched)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "personal") {
		t.Fatalf("warnings = %v, want one naming the failing account", warnings)
	}
	records, _, ok := cache.Load("work")
	if !ok || len(records) != 2 {
		t.Fatalf("cache for work = %+v (ok=%v), want the fetched records stored", records, ok)
	}
	if _, _, ok := cache.Load("personal"); ok {
		t.Fatal("a failed fetch must not be cached")
	}
}

// With no gh_account configured there is still one fetch, under gh's own active
// login — the single-account case, which is most people.
func TestRefreshPRsWithNoAccountsStillFetchesOnce(t *testing.T) {
	calls := []string{}
	cache := collect.PRCache{Dir: t.TempDir(), TTL: time.Hour, Now: time.Now}
	fetched, warnings := RefreshPRs(wdconf.Config{}, cache, func(account string) ([]workspace.PRRecord, error) {
		calls = append(calls, account)
		return []workspace.PRRecord{{FullName: "o/r", Branch: "topic"}}, nil
	})
	if len(calls) != 1 || calls[0] != "" {
		t.Fatalf("calls = %q, want one fetch under the ambient account", calls)
	}
	if fetched != 1 || len(warnings) != 0 {
		t.Fatalf("fetched = %d, warnings = %v", fetched, warnings)
	}
}

// Accounts must be fetched concurrently, as the load path already does. Each one is
// now four GitHub requests rather than one — three paginated pages for the open set
// plus the history window — so doing them in series made the popup's refresh button
// wait for the sum: measured at 11.8s across two accounts.
//
// The barrier is the assertion: every fetch has to be in flight before any is
// allowed to finish, which a sequential implementation can never satisfy.
func TestRefreshPRsFetchesAccountsConcurrently(t *testing.T) {
	cfg := wdconf.Config{
		Repos: []wdconf.Repo{{Root: "/a", GHAccount: "work"}},
		Hosts: []wdconf.Host{{Label: "asia", Repos: []wdconf.Repo{{Root: "/b", GHAccount: "personal"}}}},
	}
	cache := collect.PRCache{Dir: t.TempDir(), TTL: time.Hour, Now: time.Now}
	arrived := make(chan string, 2)
	release := make(chan struct{})
	type outcome struct {
		stored   int
		warnings []string
	}
	done := make(chan outcome, 1)
	go func() {
		stored, warnings := RefreshPRs(cfg, cache, func(account string) ([]workspace.PRRecord, error) {
			arrived <- account
			<-release
			return []workspace.PRRecord{{FullName: "o/r", Branch: account, PR: workspace.PR{Number: 1}}}, nil
		})
		done <- outcome{stored, warnings}
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("only one account was in flight: the fetches are sequential")
		}
	}
	close(release)
	got := <-done
	if got.stored != 2 || len(got.warnings) != 0 {
		t.Fatalf("stored = %d, warnings = %v, want both accounts stored", got.stored, got.warnings)
	}
	for _, account := range []string{"work", "personal"} {
		if records, _, ok := cache.Load(account); !ok || len(records) != 1 {
			t.Fatalf("cache for %s = %+v (ok=%v), want its records stored", account, records, ok)
		}
	}
}
