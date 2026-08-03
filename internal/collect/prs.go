package collect

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Khady/workdash/internal/workspace"
)

// prsQuery asks for exactly the fields a PRRecord carries. The search type has to
// be ISSUE because GitHub's search index has no PR-only type; the inline fragment
// is what narrows the result, and it is also why plain issues come back as empty
// objects that parseSearch has to drop.
const prsQuery = `query($q: String!) { search(query: $q, type: ISSUE, first: 100) ` +
	`{ nodes { ... on PullRequest { number url title isDraft state headRefName repository { nameWithOwner } ` +
	`headRepository { nameWithOwner } } } } }`

// prsSearch is passed as a GraphQL variable rather than interpolated into the
// query, so nothing in it can be read as GraphQL syntax.
const prsSearch = "is:pr author:@me sort:updated-desc"

// prsArgv is a function, not a var, because the argv is handed to a Runner that
// may hold on to the slice; sharing one backing array between calls is a bug
// waiting for a second collector.
func prsArgv() []string {
	return []string{"gh", "api", "graphql", "-f", "query=" + prsQuery, "-f", "q=" + prsSearch}
}

// PRs lists the pull requests one GitHub account authored.
//
// It is one call per account, always local: the pull requests of an account are
// the same facts no matter which machine asks, so running this per host would
// multiply the only network call in the system for identical answers.
//
// Every failure is returned. GitHub is the one source that can be down, rate
// limited or unauthorised, and the caller's job is to degrade to a snapshot
// without PR facets and say so — silently returning no pull requests would read
// as "you have none", which is the same shape as the healthy empty answer.
func PRs(runner EnvRunner, account string) ([]workspace.PRRecord, error) {
	env := []string(nil)
	if account != "" {
		// The token travels in the environment because /proc/<pid>/cmdline is
		// world-readable on Linux while /proc/<pid>/environ is not: passing it as
		// `-H "Authorization: ..."` would publish a live GitHub credential to
		// every local user for the lifetime of the call.
		token, err := runner.Run("", []string{"gh", "auth", "token", "--user", account})
		if err != nil {
			return nil, fmt.Errorf("gh auth token for %s: %w", account, err)
		}
		token = strings.TrimSpace(token)
		if token == "" {
			return nil, fmt.Errorf("gh auth token for %s: no token", account)
		}
		env = []string{"GH_TOKEN=" + token}
	}
	// With no account there is nothing to look up, and gh's own login is the
	// right credential — that is the single-account case, which is most people.
	out, err := runner.RunEnv("", env, prsArgv())
	if err != nil {
		return nil, fmt.Errorf("gh api graphql: %w", err)
	}
	return parseSearch(out)
}

// searchResponse mirrors only the parts of the payload that matter. It is
// private because the shape is gh's, not workdash's: everything downstream sees
// workspace.PRRecord.
type searchResponse struct {
	Data struct {
		Search struct {
			Nodes []struct {
				Number      int    `json:"number"`
				URL         string `json:"url"`
				IsDraft     bool   `json:"isDraft"`
				State       string `json:"state"`
				Title       string `json:"title"`
				HeadRefName string `json:"headRefName"`
				Repository  struct {
					NameWithOwner string `json:"nameWithOwner"`
				} `json:"repository"`
				// HeadRepository is null when the fork the branch lived on is
				// gone, so it is a pointer rather than a value.
				HeadRepository *struct {
					NameWithOwner string `json:"nameWithOwner"`
				} `json:"headRepository"`
			} `json:"nodes"`
		} `json:"search"`
	} `json:"data"`
}

// parseSearch drops nodes that are missing either half of the key.
//
// The search returns issues too, and an issue matches no field of the
// PullRequest fragment, so it arrives as an empty object. Kept, it would become a
// record keyed by ""/"" and attach itself to every workspace whose remote could
// not be parsed — a pull request appearing on an unrelated branch.
func parseSearch(out string) ([]workspace.PRRecord, error) {
	var response searchResponse
	if err := json.Unmarshal([]byte(out), &response); err != nil {
		return nil, fmt.Errorf("gh api graphql: unexpected output: %w", err)
	}
	records := []workspace.PRRecord{}
	for _, node := range response.Data.Search.Nodes {
		if node.HeadRefName == "" || node.Repository.NameWithOwner == "" {
			continue
		}
		records = append(records, workspace.PRRecord{
			FullName:     node.Repository.NameWithOwner,
			HeadFullName: headFullName(node.HeadRepository),
			Branch:       node.HeadRefName,
			PR: workspace.PR{
				Number: node.Number,
				Title:  node.Title,
				URL:    node.URL,
				// The state is passed through verbatim. Interpreting it here would
				// put GitHub's vocabulary in two places, and the renderer already
				// has to decide how OPEN, MERGED and CLOSED look.
				State: node.State,
				Draft: node.IsDraft,
			},
		})
	}
	return records, nil
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
