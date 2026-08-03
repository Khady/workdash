// Package snapshot assembles the current state of every configured host into one
// workspace snapshot, and decides when pull requests are worth refetching.
//
// It lives outside cmd/wd so every frontend — the CLI, a TUI, later a daemon —
// gets the same snapshot by the same rules, rather than each re-deriving them.
package snapshot

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/Khady/workdash/internal/collect"
	"github.com/Khady/workdash/internal/wdconf"
	"github.com/Khady/workdash/internal/workspace"
)

// HostResult is one host's contribution. Errors are per host so an unreachable
// machine degrades its own section of the list and nothing else.
type HostResult struct {
	Host     string
	Git      []workspace.GitRecord
	Sessions []workspace.SessionRecord
	Err      error
}

// HostsFromConfig puts the local machine first. It is implied rather than
// configured, which is why `local` is a reserved label.
func HostsFromConfig(cfg wdconf.Config) []collect.Host {
	hosts := []collect.Host{{
		Label:       wdconf.LocalHost,
		Repos:       toCollectRepos(cfg.Repos),
		TmuxEnabled: true,
	}}
	for _, host := range cfg.Hosts {
		hosts = append(hosts, collect.Host{
			Label:       host.Label,
			SSHTarget:   host.SSHTarget,
			Repos:       toCollectRepos(host.Repos),
			TmuxEnabled: host.TmuxEnabled,
		})
	}
	return hosts
}

func toCollectRepos(repos []wdconf.Repo) []collect.Repo {
	out := make([]collect.Repo, 0, len(repos))
	for _, repo := range repos {
		out = append(out, collect.Repo{Root: repo.Root, Label: repo.Label})
	}
	return out
}

// Load collects every host concurrently. A slow or unreachable host
// costs its own latency, not the sum of all of them.
func Load(cfg wdconf.Config) (workspace.Snapshot, []string) {
	hosts := HostsFromConfig(cfg)
	results := make([]HostResult, len(hosts))
	var wg sync.WaitGroup
	for i, host := range hosts {
		wg.Add(1)
		go func(i int, host collect.Host) {
			defer wg.Done()
			git, sessions, err := collect.CollectHost(transportFor(host), host)
			results[i] = HostResult{Host: host.Label, Git: git, Sessions: sessions, Err: err}
		}(i, host)
	}
	// GitHub is the only network source here, so it is never allowed to make you
	// wait: a cached list renders instantly however old it is, and the refetch
	// happens in a detached process.
	cache := CacheFor(cfg)
	refreshWanted := false
	prs, prWarnings := CollectPRs(cfg.GHAccounts(), func(account string) ([]workspace.PRRecord, error) {
		records, age, ok := cache.Load(account)
		decision := Policy(ok, age, cfg.PRCacheTTL, cfg.PRRefreshAfter)
		if decision.RefreshInBackground {
			refreshWanted = true
		}
		if decision.UseCache {
			return records, nil
		}
		fetched, err := collect.PRs(collect.LocalRunner{}, account)
		if err != nil {
			return nil, err
		}
		cache.Store(account, fetched)
		return fetched, nil
	})
	if refreshWanted {
		SpawnBackgroundRefresh()
	}
	wg.Wait()
	snap, warnings := MergeHostResults(results)
	return workspace.AttachPRs(snap, prs), append(warnings, prWarnings...)
}

// transportFor is the whole local/remote distinction: an empty ssh target runs
// sections directly, anything else runs the entire plan in one ssh.
func transportFor(host collect.Host) collect.Transport {
	if host.SSHTarget == "" {
		return collect.Local{Runner: collect.LocalRunner{}}
	}
	return collect.SSH{Runner: collect.LocalRunner{}, Target: host.SSHTarget}
}

// Decision is what to do about one account's pull requests.
type Decision struct {
	UseCache            bool
	RefreshInBackground bool
	FetchNow            bool
}

// Below the soft limit the cache is simply used. Past it the cache is still what
// you see — an interactive picker that blocks for three seconds on api.github.com
// is worse than a list that is fifteen minutes stale — and a refresh is kicked
// off behind you. Only past the hard limit is waiting the lesser evil.
func Policy(cached bool, age, ttl, refreshAfter time.Duration) Decision {
	if !cached || age >= ttl {
		return Decision{FetchNow: true}
	}
	return Decision{UseCache: true, RefreshInBackground: age >= refreshAfter}
}

func CacheFor(cfg wdconf.Config) collect.PRCache {
	return collect.PRCache{Dir: collect.DefaultPRCacheDir(), TTL: cfg.PRCacheTTL, Now: time.Now}
}

// FetchPRs is the real fetch, one `gh` call for one account. It exists as a
// named function so RefreshPRs can take the fetch as a parameter and still have
// an obvious production value.
func FetchPRs(account string) ([]workspace.PRRecord, error) {
	return collect.PRs(collect.LocalRunner{}, account)
}

// RefreshPRs refetches every configured account with the caller waiting and
// stores what it gets. It returns how many pull requests were stored and one
// warning per account that failed.
//
// It lives here rather than in a frontend because every frontend needs it — the
// CLI's `wd refresh`, the detached background refresh, and the browser popup's
// refresh button — and three copies would eventually cache different things.
// A failed account is a warning, never a stored empty result: caching "you have
// no pull requests" because GitHub was briefly unauthorised would pin that
// answer for the whole TTL.
// Accounts go concurrently, as the load path's CollectPRs already does. Each one
// is four GitHub requests now rather than one — the open set is paginated, and the
// history window is its own call — so in series two accounts took 11.8s with the
// popup's refresh button waiting on the sum. The cache is one file per account, so
// the stores do not contend.
func RefreshPRs(cfg wdconf.Config, cache collect.PRCache, fetch func(string) ([]workspace.PRRecord, error)) (int, []string) {
	accounts := AccountsToFetch(cfg)
	type result struct {
		records []workspace.PRRecord
		err     error
	}
	results := make([]result, len(accounts))
	var wg sync.WaitGroup
	for i, account := range accounts {
		wg.Add(1)
		go func(i int, account string) {
			defer wg.Done()
			records, err := fetch(account)
			results[i] = result{records: records, err: err}
		}(i, account)
	}
	wg.Wait()
	// Storing and reporting happen here, in account order, so the warnings a caller
	// prints do not depend on which fetch finished first.
	stored := 0
	warnings := []string{}
	for i, account := range accounts {
		if results[i].err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", AccountLabel(account), results[i].err))
			continue
		}
		cache.Store(account, results[i].records)
		stored += len(results[i].records)
	}
	return stored, warnings
}

// SpawnBackgroundRefresh re-execs this binary with `refresh` detached, because a
// goroutine would die with this process the moment the list is printed. Every
// frontend that serves a stale cache is expected to call it, so it is exported:
// the browser host has the same "render now, catch up behind you" obligation the
// CLI has, and it answers `refresh` for exactly this reason.
func SpawnBackgroundRefresh() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(self, "refresh")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err == nil {
		_ = cmd.Process.Release()
	}
}

// CollectPRs fetches once per account rather than once per repo, concurrently,
// and turns each failure into a warning. With no accounts configured it still
// makes one call so gh's own active account is used.
// AccountsToFetch mirrors CollectPRs: no configured accounts still means one
// fetch, using gh's own active login.
func AccountsToFetch(cfg wdconf.Config) []string {
	accounts := cfg.GHAccounts()
	if len(accounts) == 0 {
		return []string{""}
	}
	return accounts
}

func AccountLabel(account string) string {
	if account == "" {
		return "github"
	}
	return account
}

func CollectPRs(accounts []string, fetch func(string) ([]workspace.PRRecord, error)) ([]workspace.PRRecord, []string) {
	if len(accounts) == 0 {
		accounts = []string{""}
	}
	type result struct {
		account string
		records []workspace.PRRecord
		Err     error
	}
	results := make([]result, len(accounts))
	var wg sync.WaitGroup
	for i, account := range accounts {
		wg.Add(1)
		go func(i int, account string) {
			defer wg.Done()
			records, err := fetch(account)
			results[i] = result{account: account, records: records, Err: err}
		}(i, account)
	}
	wg.Wait()
	records := []workspace.PRRecord{}
	warnings := []string{}
	for _, item := range results {
		if item.Err != nil {
			label := item.account
			if label == "" {
				label = "github"
			}
			warnings = append(warnings, fmt.Sprintf("%s: %v", label, item.Err))
			continue
		}
		records = append(records, item.records...)
	}
	return records, warnings
}

func MergeHostResults(results []HostResult) (workspace.Snapshot, []string) {
	git := []workspace.GitRecord{}
	sessions := []workspace.SessionRecord{}
	warnings := []string{}
	for _, result := range results {
		if result.Err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", result.Host, result.Err))
			continue
		}
		git = append(git, result.Git...)
		sessions = append(sessions, result.Sessions...)
	}
	return workspace.Index(git, sessions), warnings
}
