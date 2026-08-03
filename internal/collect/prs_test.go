package collect

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// fakeEnvRunner answers only the commands a test declares, and records the
// environment each one was given. An unmapped argv is a test failure rather than
// an empty string: this package has already been bitten by a fake that returned
// "" for anything it did not recognise, which let a collector rename its command
// and keep passing.
type fakeEnvRunner struct {
	t    *testing.T
	out  map[string]string
	fail map[string]error
	// calls is every argv seen, in order, and envs is the environment each one
	// ran with, keyed by joined argv.
	calls []string
	envs  map[string][]string
}

func newFakeEnvRunner(t *testing.T, out map[string]string) *fakeEnvRunner {
	return &fakeEnvRunner{t: t, out: out, envs: map[string][]string{}}
}

func (f *fakeEnvRunner) Run(cwd string, argv []string) (string, error) {
	return f.RunEnv(cwd, nil, argv)
}

func (f *fakeEnvRunner) RunEnv(cwd string, env []string, argv []string) (string, error) {
	f.t.Helper()
	key := strings.Join(argv, " ")
	f.calls = append(f.calls, key)
	f.envs[key] = append([]string(nil), env...)
	if err, ok := f.fail[key]; ok {
		return "", err
	}
	out, ok := f.out[key]
	if !ok {
		f.t.Fatalf("unexpected command %q; mapped commands: %v", key, f.keys())
	}
	return out, nil
}

func (f *fakeEnvRunner) keys() []string {
	keys := make([]string, 0, len(f.out))
	for key := range f.out {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (f *fakeEnvRunner) called(prefix string) bool {
	for _, call := range f.calls {
		if strings.HasPrefix(call, prefix) {
			return true
		}
	}
	return false
}

// searchKey is the gh call PRs makes for the set it treats as complete, as the
// fake keys it. Spelling it once here keeps the tests from asserting the query
// string in five places.
func searchKey() string {
	return strings.Join(searchArgv(openSearch, true), " ")
}

func historyKey() string {
	return strings.Join(searchArgv(historySearch, false), " ")
}

// searches maps both of PRs' calls: the body under test for the open search, and
// an empty result for the history one. Most tests are about parsing a single
// response rather than about the union of the two, and the fake refuses unmapped
// commands on purpose.
func searches(body string) map[string]string {
	return map[string]string{searchKey(): body, historyKey(): emptySearchResponse}
}

// with adds one command to a map of them, so a test can spell its fixture in one
// expression.
func with(mapped map[string]string, key, value string) map[string]string {
	mapped[key] = value
	return mapped
}

const emptySearchResponse = `{"data":{"search":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}`

const tokenKey = "gh auth token --user work"

// A response captured from the real command.
const realResponse = `{"data":{"search":{"nodes":[` +
	`{"number":208,"url":"https://github.com/ahrefs/monorobot/pull/208","isDraft":false,"state":"MERGED","headRefName":"louis/pr-desc-unfurl","repository":{"nameWithOwner":"ahrefs/monorobot"}},` +
	`{"number":12,"url":"https://github.com/Khady/workdash/pull/12","isDraft":true,"state":"OPEN","headRefName":"rewrite/core-facets","repository":{"nameWithOwner":"Khady/workdash"}}` +
	`]}}}`

func TestPRsParsesTheRealResponse(t *testing.T) {
	runner := newFakeEnvRunner(t, with(searches(realResponse), tokenKey, "gho_secret\n"))
	records, err := PRs(runner, "work")
	if err != nil {
		t.Fatalf("PRs() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %d %+v, want 2", len(records), records)
	}
	first := records[0]
	if first.FullName != "ahrefs/monorobot" || first.Branch != "louis/pr-desc-unfurl" {
		t.Fatalf("first record = %+v, want it keyed by nameWithOwner and headRefName", first)
	}
	if first.PR.Number != 208 || first.PR.State != "MERGED" || first.PR.Draft {
		t.Fatalf("first pr = %+v, want #208 MERGED and not a draft", first.PR)
	}
	if first.PR.URL != "https://github.com/ahrefs/monorobot/pull/208" {
		t.Fatalf("first url = %q", first.PR.URL)
	}
	if second := records[1]; !second.PR.Draft || second.PR.State != "OPEN" {
		t.Fatalf("second pr = %+v, want the draft flag carried through", second.PR)
	}
}

// The token must reach gh through the environment only. /proc/<pid>/cmdline is
// world-readable and /proc/<pid>/environ is not, so a token in argv hands every
// local user a GitHub credential for as long as the call runs.
func TestPRsPassesTheTokenInTheEnvironmentAndNeverInArgv(t *testing.T) {
	runner := newFakeEnvRunner(t, with(searches(emptySearchResponse), tokenKey, "gho_secret\n"))
	if _, err := PRs(runner, "work"); err != nil {
		t.Fatalf("PRs() error = %v", err)
	}
	env := runner.envs[searchKey()]
	found := false
	for _, entry := range env {
		if entry == "GH_TOKEN=gho_secret" {
			found = true
		}
	}
	if !found {
		t.Fatalf("env = %v, want GH_TOKEN=gho_secret", env)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "gho_secret") {
			t.Fatalf("argv %q contains the token; /proc/<pid>/cmdline is world-readable", call)
		}
	}
}

// With no account configured there is nothing to look a token up for, and asking
// gh for one would fail; the ambient login is the right credential.
func TestPRsWithNoAccountUsesAmbientAuth(t *testing.T) {
	runner := newFakeEnvRunner(t, searches(emptySearchResponse))
	if _, err := PRs(runner, ""); err != nil {
		t.Fatalf("PRs() error = %v", err)
	}
	if runner.called("gh auth token") {
		t.Fatalf("calls = %v, want no token lookup for an empty account", runner.calls)
	}
	for _, entry := range runner.envs[searchKey()] {
		if strings.HasPrefix(entry, "GH_TOKEN=") {
			t.Fatalf("env = %v, want no GH_TOKEN so gh uses its own login", runner.envs[searchKey()])
		}
	}
}

// The search type is ISSUE, so a plain issue comes back as an empty PullRequest
// fragment. Those must not become records: a phantom keyed by "" would attach
// itself to whichever workspace also has an empty full name.
func TestPRsSkipsIssueShapedNodes(t *testing.T) {
	runner := newFakeEnvRunner(t, searches(`{"data":{"search":{"nodes":[`+
		`{},`+
		`{"number":0,"url":"","isDraft":false,"state":"","headRefName":"","repository":{"nameWithOwner":""}},`+
		`{"number":7,"url":"u","isDraft":false,"state":"OPEN","headRefName":"topic","repository":{"nameWithOwner":""}},`+
		`{"number":8,"url":"u","isDraft":false,"state":"OPEN","headRefName":"","repository":{"nameWithOwner":"a/b"}},`+
		`{"number":9,"url":"u","isDraft":false,"state":"OPEN","headRefName":"topic","repository":{"nameWithOwner":"a/b"}}`+
		`]}}}`))
	records, err := PRs(runner, "")
	if err != nil {
		t.Fatalf("PRs() error = %v", err)
	}
	if len(records) != 1 || records[0].PR.Number != 9 {
		t.Fatalf("records = %+v, want only the fully-shaped pull request #9", records)
	}
}

func TestPRsReportsATokenLookupFailure(t *testing.T) {
	runner := newFakeEnvRunner(t, searches(emptySearchResponse))
	runner.fail = map[string]error{tokenKey: errors.New("no such user work")}
	if _, err := PRs(runner, "work"); err == nil {
		t.Fatal("PRs() error = nil, want the token lookup failure")
	}
	if runner.called("gh api graphql") {
		t.Fatalf("calls = %v, want no query once the token could not be read", runner.calls)
	}
}

func TestPRsReportsAQueryFailure(t *testing.T) {
	runner := newFakeEnvRunner(t, nil)
	runner.fail = map[string]error{searchKey(): errors.New("HTTP 401: Bad credentials")}
	if _, err := PRs(runner, ""); err == nil {
		t.Fatal("PRs() error = nil, want the query failure")
	}
}

// A body that is not the expected JSON has to be an error, not zero records:
// "you have no pull requests" and "gh printed something else entirely" are
// different states and only the second is worth warning about.
func TestPRsReportsMalformedJSON(t *testing.T) {
	runner := newFakeEnvRunner(t, map[string]string{
		searchKey(): "gh: command not found",
	})
	if _, err := PRs(runner, ""); err == nil {
		t.Fatal("PRs() error = nil, want the malformed body reported")
	}
}

// The query has to ask for every field a PRRecord needs, and ask GitHub to
// restrict the search to the caller's own pull requests.
func TestPRsQueryAsksForTheFieldsItParses(t *testing.T) {
	argv := searchArgv(openSearch, true)
	joined := strings.Join(argv, " ")
	for _, want := range []string{"nameWithOwner", "headRefName", "isDraft", "state", "url", "number"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("query %q is missing %q", joined, want)
		}
	}
	if !strings.Contains(joined, "is:pr") {
		t.Fatalf("argv %q, want the search restricted to pull requests", joined)
	}
}

// A fork pull request has repository = the base repo and headRepository = the
// fork. The local clone's origin is the fork, so without the head repository
// such a PR can never find its workspace.
func TestPRsRecordsTheHeadRepositoryForForkPullRequests(t *testing.T) {
	body := `{"data":{"search":{"nodes":[{"number":10868,` +
		`"url":"https://github.com/ocaml/dune/pull/10868","isDraft":false,"state":"MERGED",` +
		`"headRefName":"louis/dune-watch","repository":{"nameWithOwner":"ocaml/dune"},` +
		`"headRepository":{"nameWithOwner":"Khady/dune"}}]}}}`
	runner := newFakeEnvRunner(t, searches(body))
	records, err := PRs(runner, "")
	if err != nil {
		t.Fatalf("PRs() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %+v, want 1", records)
	}
	if records[0].FullName != "ocaml/dune" || records[0].HeadFullName != "Khady/dune" {
		t.Fatalf("record = %+v, want base ocaml/dune and head Khady/dune", records[0])
	}
}

func TestPRsQueryAsksForTheHeadRepository(t *testing.T) {
	if !strings.Contains(prsQuery, "headRepository { nameWithOwner }") {
		t.Fatalf("query does not request headRepository:\n%s", prsQuery)
	}
}

// A deleted fork leaves headRepository null; the base match must still work.
func TestPRsToleratesNullHeadRepository(t *testing.T) {
	body := `{"data":{"search":{"nodes":[{"number":7,"url":"u","isDraft":false,"state":"OPEN",` +
		`"headRefName":"topic","repository":{"nameWithOwner":"o/r"},"headRepository":null}]}}}`
	runner := newFakeEnvRunner(t, searches(body))
	records, err := PRs(runner, "")
	if err != nil {
		t.Fatalf("PRs() error = %v", err)
	}
	if len(records) != 1 || records[0].FullName != "o/r" || records[0].HeadFullName != "" {
		t.Fatalf("records = %+v, want the base match with an empty head", records)
	}
}

// The old dashboard showed the PR title on its PR rows; a number alone does not
// tell you which PR it is. It is one more field in a query already being made.
func TestPRsRecordsTheTitle(t *testing.T) {
	body := `{"data":{"search":{"nodes":[{"number":7,"url":"u","isDraft":false,"state":"OPEN",` +
		`"title":"[AA] add ATD docs for shared jsonschema types",` +
		`"headRefName":"topic","repository":{"nameWithOwner":"o/r"},"headRepository":null}]}}}`
	runner := newFakeEnvRunner(t, searches(body))
	records, err := PRs(runner, "")
	if err != nil {
		t.Fatalf("PRs() error = %v", err)
	}
	if len(records) != 1 || records[0].PR.Title != "[AA] add ATD docs for shared jsonschema types" {
		t.Fatalf("record = %+v, want the title carried", records)
	}
}

func TestPRsQueryAsksForTheTitle(t *testing.T) {
	if !strings.Contains(prsQuery, "title") {
		t.Fatalf("query does not request the title:\n%s", prsQuery)
	}
}

// The set workdash treats as complete is "open pull requests I am involved in",
// not "pull requests I wrote". A colleague's review request is work you have, and
// author:@me could not see it.
func TestPRsAsksForEveryOpenPullRequestItIsInvolvedIn(t *testing.T) {
	if !strings.Contains(searchKey(), "is:pr is:open involves:@me") {
		t.Fatalf("open search = %q, want every open pull request involving the user", searchKey())
	}
	// The history search stays author-scoped: it exists for the merged and closed
	// ones, which is a question about your own work.
	if !strings.Contains(historyKey(), "author:@me") {
		t.Fatalf("history search = %q, want it scoped to the user's own pull requests", historyKey())
	}
}

// The open set must be paginated or it is not complete: GitHub returns 100 per
// page and the measured account has 227 open. Silently keeping the first page is
// exactly the truncation this replaces.
func TestPRsPaginatesTheOpenSearchAndNotTheHistoryWindow(t *testing.T) {
	if !strings.Contains(searchKey(), "--paginate") {
		t.Fatalf("open search = %q, want it paginated", searchKey())
	}
	if !strings.Contains(searchKey(), "pageInfo") || !strings.Contains(searchKey(), "endCursor") {
		t.Fatalf("open search = %q, want the cursor fields gh needs to follow pages", searchKey())
	}
	// The history window is deliberately one page: `involves:@me` across all states
	// is thousands of results and GitHub caps search at 1000 however you page it.
	if strings.Contains(historyKey(), "--paginate") {
		t.Fatalf("history search = %q, want a single page", historyKey())
	}
}

// gh --paginate writes one JSON document per page, concatenated. Unmarshalling the
// whole stream at once reads only the first page and reports no error, which would
// leave the truncation in place while looking fixed.
func TestPRsReadsEveryPageOfAPaginatedResponse(t *testing.T) {
	page := func(number int, branch string, more bool) string {
		return fmt.Sprintf(`{"data":{"search":{"pageInfo":{"hasNextPage":%t,"endCursor":"c"},"nodes":[`+
			`{"number":%d,"url":"https://github.com/o/r/pull/%d","state":"OPEN","headRefName":%q,`+
			`"repository":{"nameWithOwner":"o/r"}}]}}}`, more, number, number, branch)
	}
	runner := newFakeEnvRunner(t, searches(page(1, "one", true)+"\n"+page(2, "two", false)))
	records, err := PRs(runner, "")
	if err != nil {
		t.Fatalf("PRs() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("records = %+v, want both pages", records)
	}
	if records[0].Branch != "one" || records[1].Branch != "two" {
		t.Fatalf("records = %+v, want one record per page in order", records)
	}
}

// The two searches overlap on your own open pull requests, and the same pull
// request twice would attach twice and count twice.
func TestPRsMergesTheTwoSearchesWithoutDuplicates(t *testing.T) {
	both := `{"data":{"search":{"pageInfo":{"hasNextPage":false},"nodes":[` +
		`{"number":7,"url":"https://github.com/o/r/pull/7","state":"OPEN","headRefName":"mine",` +
		`"repository":{"nameWithOwner":"o/r"}}]}}}`
	runner := newFakeEnvRunner(t, map[string]string{searchKey(): both, historyKey(): both})
	records, err := PRs(runner, "")
	if err != nil {
		t.Fatalf("PRs() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %+v, want the shared pull request once", records)
	}
}

// A response of no bytes is not "you have no pull requests". gh exiting 0 with
// nothing on stdout means something went wrong that we cannot interpret, and
// caching an empty answer would pin it for the whole TTL.
func TestPRsRejectsAnEmptyResponse(t *testing.T) {
	runner := newFakeEnvRunner(t, searches(""))
	if _, err := PRs(runner, ""); err == nil {
		t.Fatal("PRs() error = nil, want empty output reported rather than read as no pull requests")
	}
}

// One pull request by number, for the page you simply navigated to: no search
// covers a PR you have no relationship with, and that is the case the browser has
// to answer.
func TestPRByNumberFetchesASinglePullRequest(t *testing.T) {
	body := `{"data":{"repository":{"pullRequest":{"number":37238,` +
		`"url":"https://github.com/ahrefs-core/monorepo/pull/37238","title":"ci: dune engine for CI",` +
		`"isDraft":false,"state":"OPEN","headRefName":"koonwen/ci/dune-engine",` +
		`"repository":{"nameWithOwner":"ahrefs-core/monorepo"},` +
		`"headRepository":{"nameWithOwner":"ahrefs-core/monorepo"}}}}}`
	key := strings.Join(prByNumberArgv("ahrefs-core", "monorepo", 37238), " ")
	runner := newFakeEnvRunner(t, map[string]string{key: body})
	record, err := PRByNumber(runner, "", "ahrefs-core", "monorepo", 37238)
	if err != nil {
		t.Fatalf("PRByNumber() error = %v", err)
	}
	if record.FullName != "ahrefs-core/monorepo" || record.Branch != "koonwen/ci/dune-engine" {
		t.Fatalf("record = %+v, want it keyed by repo and head branch", record)
	}
	if record.PR.Number != 37238 || record.PR.Title != "ci: dune engine for CI" {
		t.Fatalf("pr = %+v, want the number and title carried through", record.PR)
	}
}

// A number that is not a pull request comes back as a null, which must be an error
// and not an empty record keyed by ""/"" — that shape attaches itself to every
// workspace whose remote could not be parsed.
func TestPRByNumberRejectsAMissingPullRequest(t *testing.T) {
	key := strings.Join(prByNumberArgv("o", "r", 1), " ")
	runner := newFakeEnvRunner(t, map[string]string{key: `{"data":{"repository":{"pullRequest":null}}}`})
	if _, err := PRByNumber(runner, "", "o", "r", 1); err == nil {
		t.Fatal("PRByNumber() error = nil, want a missing pull request reported")
	}
}
