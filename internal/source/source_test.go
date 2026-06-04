package source

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Khady/workdash/internal/model"
)

func TestSplitSentinel(t *testing.T) {
	payload, code, err := SplitSentinel("hello\n"+SentinelPrefix+"7\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if payload != "hello\n" || code != 7 {
		t.Fatalf("payload=%q code=%d", payload, code)
	}
}

func TestSSHFailFastOptionsForwardAgent(t *testing.T) {
	if len(SSHFailFastOptions) == 0 || SSHFailFastOptions[0] != "-A" {
		t.Fatalf("SSHFailFastOptions should enable agent forwarding first: %#v", SSHFailFastOptions)
	}
}

func TestSSHRunnerBuildGHCommandWithoutRepoRoot(t *testing.T) {
	got := NewSSHRunner("me@host").BuildGHCommand("", []string{"search", "prs"}, "")
	if got != "'env' 'gh' 'search' 'prs'" {
		t.Fatalf("unexpected command: %s", got)
	}
}

func TestParseTmuxSessions(t *testing.T) {
	got := ParseTmuxSessions("main\t2\t1\t/repo\n", "local", "")
	if len(got) != 1 || got[0].Name != "main" || got[0].Windows != 2 || got[0].Path != "/repo" {
		t.Fatalf("unexpected sessions: %#v", got)
	}
}

func TestParseWindowListingBySession(t *testing.T) {
	got := ParseWindowListingBySession("main\t0:shell\nmain\t1:codex\nother\t0:logs\n")
	if len(got["main"]) != 2 || got["main"][0] != "0:shell" || got["main"][1] != "1:codex" {
		t.Fatalf("unexpected main windows: %#v", got)
	}
	if len(got["other"]) != 1 || got["other"][0] != "0:logs" {
		t.Fatalf("unexpected other windows: %#v", got)
	}
}

func TestParsePRListing(t *testing.T) {
	got, err := ParsePRListing(`[{"number":1,"headRefName":"a"},{"number":2,"headRefName":"a"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if got["a"].Number != 2 {
		t.Fatalf("unexpected PR map: %#v", got)
	}
}

func TestParseSearchPRListing(t *testing.T) {
	got, err := ParseSearchPRListing(`[{"number":1,"title":"Fix CI","url":"https://github.com/example/repo/pull/1","state":"open","isDraft":false,"updatedAt":"2026-04-21T02:47:26Z","repository":{"name":"repo","nameWithOwner":"example/repo"}}]`, "local", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("unexpected records: %#v", got)
	}
	if got[0].RepoLabel != "example/repo" || got[0].Status != "open" || got[0].UpdatedAt == nil {
		t.Fatalf("unexpected record: %#v", got[0])
	}
}

func TestParseGraphQLSearchPRListing(t *testing.T) {
	got, hasNextPage, endCursor, err := ParseGraphQLSearchPRListing(`{"data":{"search":{"nodes":[{"number":1,"title":"Fix CI","url":"https://github.com/example/repo/pull/1","state":"OPEN","isDraft":false,"updatedAt":"2026-04-21T02:47:26Z","headRefName":"feature/fix","repository":{"name":"repo","nameWithOwner":"example/repo"},"headRepository":{"nameWithOwner":"fork/repo"}}],"pageInfo":{"hasNextPage":true,"endCursor":"cursor-1"}}}}`, "local", "")
	if err != nil {
		t.Fatal(err)
	}
	if !hasNextPage || endCursor != "cursor-1" {
		t.Fatalf("unexpected page info: hasNextPage=%v endCursor=%q", hasNextPage, endCursor)
	}
	if len(got) != 1 || got[0].HeadRefName != "feature/fix" || got[0].RepoFullName != "example/repo" || got[0].HeadRepoFullName != "fork/repo" {
		t.Fatalf("unexpected records: %#v", got)
	}
}

func TestEnrichPRItemsLinksToKnownWorktree(t *testing.T) {
	items := []model.WorkItem{
		{Kind: model.KindWorktree, RepoRoot: "/repo", RepoLabel: "repo", RepoFullName: "example/repo", Path: "/repo/wt", Branch: "feature/fix", PRURL: "https://github.com/example/repo/pull/1"},
		{Kind: model.KindPR, RepoLabel: "example/repo", RepoFullName: "example/repo", Title: "Fix CI", Branch: "feature/fix", PRURL: "https://github.com/example/repo/pull/1"},
	}
	got := EnrichPRItems(items)
	if got[1].Path != "/repo/wt" || got[1].Branch != "feature/fix" || got[1].RepoRoot != "/repo" {
		t.Fatalf("unexpected linked PR item: %#v", got[1])
	}
}

func TestEnrichPRItemsLinksBranchByRepoAndHeadRef(t *testing.T) {
	items := []model.WorkItem{
		{Kind: model.KindBranch, RepoRoot: "/repo", RepoLabel: "repo", RepoFullName: "example/repo", Title: "feature/fix", Branch: "feature/fix"},
		{Kind: model.KindPR, RepoLabel: "example/repo", RepoFullName: "example/repo", Title: "Fix CI", Branch: "feature/fix", PRNumber: 12, PRURL: "https://github.com/example/repo/pull/12", PRStatus: model.PROpen},
	}
	got := EnrichPRItems(items)
	if got[0].PRNumber != 12 || got[0].PRURL == "" || got[0].PRStatus != model.PROpen {
		t.Fatalf("unexpected enriched branch item: %#v", got[0])
	}
}

func TestEnrichPRItemsLinksForkWorktreeByHeadRepoAndHeadRef(t *testing.T) {
	items := []model.WorkItem{
		{Kind: model.KindWorktree, RepoRoot: "/repo", RepoLabel: "repo", RepoFullName: "fork/repo", Path: "/repo/wt", Branch: "feature/fix"},
		{Kind: model.KindPR, RepoLabel: "upstream/repo", RepoFullName: "upstream/repo", PRHeadRepoFullName: "fork/repo", Title: "Fix CI", Branch: "feature/fix", PRNumber: 12, PRURL: "https://github.com/upstream/repo/pull/12", PRStatus: model.PROpen},
	}
	got := EnrichPRItems(items)
	if got[0].PRNumber != 12 || got[0].PRURL == "" || got[0].PRStatus != model.PROpen {
		t.Fatalf("unexpected enriched worktree item: %#v", got[0])
	}
}

type concurrencyTrackingRunner struct {
	mu          sync.Mutex
	currentGH   int
	maxGH       int
	callCountGH int
}

func (r *concurrencyTrackingRunner) RunGit(repoRoot string, args []string) (string, error) {
	return "", nil
}

func (r *concurrencyTrackingRunner) RunGH(repoRoot string, args []string, ghToken string) (string, error) {
	r.mu.Lock()
	r.currentGH++
	r.callCountGH++
	if r.currentGH > r.maxGH {
		r.maxGH = r.currentGH
	}
	r.mu.Unlock()

	time.Sleep(25 * time.Millisecond)

	r.mu.Lock()
	r.currentGH--
	r.mu.Unlock()
	return `{"data":{"search":{"nodes":[{"number":1,"title":"Fix CI","url":"https://github.com/example/repo/pull/1","state":"OPEN","isDraft":false,"updatedAt":"2026-04-21T02:47:26Z","headRefName":"main","repository":{"name":"repo","nameWithOwner":"example/repo"}}],"pageInfo":{"hasNextPage":false,"endCursor":""}}}}`, nil
}

func (r *concurrencyTrackingRunner) RunGHAuth(args []string) (string, error) {
	return "", nil
}

func TestFetchAccountPRsLimitsGHConcurrency(t *testing.T) {
	prCacheMu.Lock()
	prCache = map[string]prCacheEntry{}
	accountPRCache = map[string]accountPRCacheEntry{}
	ghTokenCache = map[string]string{}
	prCacheMu.Unlock()

	runner := &concurrencyTrackingRunner{}
	var wg sync.WaitGroup
	errs := make(chan error, ghRequestLimit*3)

	for i := 0; i < ghRequestLimit*3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			prs, err := FetchAccountPRs(fmt.Sprintf("account-%d", i), "local", "", runner)
			if err != nil {
				errs <- err
				return
			}
			if len(prs) != 1 || prs[0].Number != 1 {
				errs <- fmt.Errorf("unexpected PRs: %#v", prs)
			}
		}(i)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if runner.maxGH > ghRequestLimit {
		t.Fatalf("max concurrent gh calls = %d, want <= %d", runner.maxGH, ghRequestLimit)
	}
	if runner.callCountGH != ghRequestLimit*3 {
		t.Fatalf("gh call count = %d, want %d", runner.callCountGH, ghRequestLimit*3)
	}
}
