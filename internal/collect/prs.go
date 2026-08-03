package collect

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Khady/workdash/internal/workspace"
)

// prFields are the fields a PRRecord carries, shared by the search and the
// single-pull-request lookup so the two cannot drift into parsing different
// things.
const prFields = `number url title isDraft state headRefName repository { nameWithOwner } ` +
	`headRepository { nameWithOwner }`

// prsQuery asks for exactly those fields. The search type has to be ISSUE because
// GitHub's search index has no PR-only type; the inline fragment is what narrows
// the result, and it is also why plain issues come back as empty objects that
// parseSearch has to drop.
//
// pageInfo and $endCursor are what let gh follow the pages itself.
const prsQuery = `query($q: String!, $endCursor: String) ` +
	`{ search(query: $q, type: ISSUE, first: 100, after: $endCursor) ` +
	`{ pageInfo { hasNextPage endCursor } nodes { ... on PullRequest { ` + prFields + ` } } } }`

// The two searches have deliberately different contracts, and the difference is
// the point.
//
// openSearch is meant to be complete: every open pull request the account is
// involved in, every page of it. That is the set worth being sure about — a review
// request you cannot see is a review you will not do — and it is bounded, 227 on
// the account this was measured against.
//
// historySearch is explicitly a window, one page of your own most recently updated
// pull requests. It exists for the merged and closed ones, which are what tell you
// a worktree is finished with. It cannot be complete and does not pretend to be:
// `is:pr involves:@me` across every state is 7580 results, and GitHub's search caps
// at 1000 however you page it.
//
// Both are passed as GraphQL variables rather than interpolated into the query, so
// nothing in them can be read as GraphQL syntax.
const (
	openSearch    = "is:pr is:open involves:@me"
	historySearch = "is:pr author:@me sort:updated-desc"
)

// searchArgv is a function, not a var, because the argv is handed to a Runner that
// may hold on to the slice; sharing one backing array between calls is a bug
// waiting for a second collector.
func searchArgv(search string, paginate bool) []string {
	argv := []string{"gh", "api", "graphql"}
	if paginate {
		// gh follows the cursor itself when the query exposes pageInfo and accepts an
		// $endCursor variable, writing one JSON document per page. Threading a cursor
		// through the Runner here would buy nothing.
		argv = append(argv, "--paginate")
	}
	return append(argv, "-f", "query="+prsQuery, "-f", "q="+search)
}

// PRs lists the pull requests one GitHub account cares about: every open one it is
// involved in, plus a recent window of the ones it wrote, merged and closed
// included.
//
// It is two calls per account, always local: the pull requests of an account are
// the same facts no matter which machine asks, so running this per host would
// multiply the only network call in the system for identical answers.
//
// Every failure is returned. GitHub is the one source that can be down, rate
// limited or unauthorised, and the caller's job is to degrade to a snapshot
// without PR facets and say so — silently returning no pull requests would read
// as "you have none", which is the same shape as the healthy empty answer.
func PRs(runner EnvRunner, account string) ([]workspace.PRRecord, error) {
	env, err := tokenEnv(runner, account)
	if err != nil {
		return nil, err
	}
	records := []workspace.PRRecord{}
	for _, search := range []struct {
		query    string
		paginate bool
	}{{openSearch, true}, {historySearch, false}} {
		out, err := runner.RunEnv("", env, searchArgv(search.query, search.paginate))
		if err != nil {
			return nil, fmt.Errorf("gh api graphql: %w", err)
		}
		page, err := parseSearch(out)
		if err != nil {
			return nil, err
		}
		records = append(records, page...)
	}
	// The two searches overlap on your own open pull requests, and the same one
	// twice would attach twice and be counted twice.
	return dedupe(records), nil
}

// prByNumberQuery reaches one pull request directly. No search covers a pull
// request you have no relationship with, which is exactly the case a browser has
// to answer: a page you navigated to.
const prByNumberQuery = `query($owner: String!, $name: String!, $number: Int!) ` +
	`{ repository(owner: $owner, name: $name) { pullRequest(number: $number) { ` + prFields + ` } } }`

func prByNumberArgv(owner, name string, number int) []string {
	return []string{"gh", "api", "graphql", "-f", "query=" + prByNumberQuery,
		"-f", "owner=" + owner, "-f", "name=" + name,
		// -F, not -f: the number is an Int! in the schema and a quoted string is
		// rejected by GraphQL's own type check.
		"-F", "number=" + strconv.Itoa(number)}
}

// PRByNumber fetches one pull request. It is the on-demand half of the PR facet:
// the cached searches cover what is yours and what involves you, and this covers
// everything else, at the cost of one call made only when something asks about a
// pull request the searches never saw.
func PRByNumber(runner EnvRunner, account, owner, name string, number int) (workspace.PRRecord, error) {
	env, err := tokenEnv(runner, account)
	if err != nil {
		return workspace.PRRecord{}, err
	}
	out, err := runner.RunEnv("", env, prByNumberArgv(owner, name, number))
	if err != nil {
		return workspace.PRRecord{}, fmt.Errorf("gh api graphql: %w", err)
	}
	var response struct {
		Data struct {
			Repository struct {
				PullRequest *prNode `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &response); err != nil {
		return workspace.PRRecord{}, fmt.Errorf("gh api graphql: unexpected output: %w", err)
	}
	node := response.Data.Repository.PullRequest
	// A number that is not a pull request comes back as a null. Reported rather
	// than returned empty: a record keyed by ""/"" attaches itself to every
	// workspace whose remote could not be parsed.
	if node == nil || node.HeadRefName == "" || node.Repository.NameWithOwner == "" {
		return workspace.PRRecord{}, fmt.Errorf("%s/%s#%d is not a pull request", owner, name, number)
	}
	return recordOf(*node), nil
}

// tokenEnv looks up the token for an account, or returns nothing for the ambient
// login.
//
// The token travels in the environment because /proc/<pid>/cmdline is
// world-readable on Linux while /proc/<pid>/environ is not: passing it as
// `-H "Authorization: ..."` would publish a live GitHub credential to every local
// user for the lifetime of the call.
func tokenEnv(runner EnvRunner, account string) ([]string, error) {
	// With no account there is nothing to look up, and gh's own login is the right
	// credential — that is the single-account case, which is most people.
	if account == "" {
		return nil, nil
	}
	token, err := runner.Run("", []string{"gh", "auth", "token", "--user", account})
	if err != nil {
		return nil, fmt.Errorf("gh auth token for %s: %w", account, err)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("gh auth token for %s: no token", account)
	}
	return []string{"GH_TOKEN=" + token}, nil
}

// dedupe keeps the first record for each pull request. A pull request is
// identified by its repository and number, not by its branch: a branch can carry
// several over time.
func dedupe(records []workspace.PRRecord) []workspace.PRRecord {
	seen := map[string]bool{}
	out := make([]workspace.PRRecord, 0, len(records))
	for _, record := range records {
		key := fmt.Sprintf("%s#%d", record.FullName, record.PR.Number)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, record)
	}
	return out
}

// prNode mirrors only the parts of the payload that matter. It is private because
// the shape is gh's, not workdash's: everything downstream sees
// workspace.PRRecord.
type prNode struct {
	Number      int    `json:"number"`
	URL         string `json:"url"`
	IsDraft     bool   `json:"isDraft"`
	State       string `json:"state"`
	Title       string `json:"title"`
	HeadRefName string `json:"headRefName"`
	Repository  struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	// HeadRepository is null when the fork the branch lived on is gone, so it is a
	// pointer rather than a value.
	HeadRepository *struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"headRepository"`
}

type searchResponse struct {
	Data struct {
		Search struct {
			Nodes []prNode `json:"nodes"`
		} `json:"search"`
	} `json:"data"`
}

// parseSearch drops nodes that are missing either half of the key.
//
// The search returns issues too, and an issue matches no field of the
// PullRequest fragment, so it arrives as an empty object. Kept, it would become a
// record keyed by ""/"" and attach itself to every workspace whose remote could
// not be parsed — a pull request appearing on an unrelated branch.
// It also reads a whole paginated stream. `gh --paginate` writes one JSON document
// per page, concatenated, so a single Unmarshal would silently return only the
// first page — the same truncation pagination was added to remove, but harder to
// notice because nothing errors.
func parseSearch(out string) ([]workspace.PRRecord, error) {
	decoder := json.NewDecoder(strings.NewReader(out))
	records := []workspace.PRRecord{}
	pages := 0
	for {
		var response searchResponse
		err := decoder.Decode(&response)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("gh api graphql: unexpected output: %w", err)
		}
		pages++
		for _, node := range response.Data.Search.Nodes {
			if node.HeadRefName == "" || node.Repository.NameWithOwner == "" {
				continue
			}
			records = append(records, recordOf(node))
		}
	}
	// No documents at all is not "you have no pull requests": gh exiting 0 with an
	// empty stdout means something we cannot interpret happened, and caching an
	// empty answer would pin it for the whole TTL.
	if pages == 0 {
		return nil, fmt.Errorf("gh api graphql: empty response")
	}
	return records, nil
}

func recordOf(node prNode) workspace.PRRecord {
	return workspace.PRRecord{
		FullName:     node.Repository.NameWithOwner,
		HeadFullName: headFullName(node.HeadRepository),
		Branch:       node.HeadRefName,
		PR: workspace.PR{
			Number: node.Number,
			Title:  node.Title,
			URL:    node.URL,
			// The state is passed through verbatim. Interpreting it here would put
			// GitHub's vocabulary in two places, and the renderer already has to
			// decide how OPEN, MERGED and CLOSED look.
			State: node.State,
			Draft: node.IsDraft,
		},
	}
}

// headFullName reads the fork a pull request's branch lives on, tolerating the
// null GitHub returns once that fork is deleted.
func headFullName(head *struct {
	NameWithOwner string `json:"nameWithOwner"`
}) string {
	if head == nil {
		return ""
	}
	return head.NameWithOwner
}
