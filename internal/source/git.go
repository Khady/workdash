package source

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Khady/workdash/internal/actions"
	"github.com/Khady/workdash/internal/model"
)

type GitRepoConfig struct {
	Root      string
	Label     string
	GHAccount string
	Remote    bool
}

var WorktreeCommand = []string{"worktree", "list", "--porcelain"}
var BranchCommand = []string{"for-each-ref", "--sort=-committerdate", "--format=%(refname:short)\t%(committerdate:iso8601-strict)\t%(upstream:short)\t%(upstream:track)", "refs/heads/"}
var RemoteURLCommand = []string{"remote", "get-url", "origin"}

var trackAheadPattern = regexp.MustCompile(`ahead (?P<count>\d+)`)
var prCacheTTL = 60 * time.Second
var ghRequestLimit = 4
var authoredPRSearchQuery = `
query($searchQuery: String!, $after: String) {
  search(type: ISSUE, query: $searchQuery, first: 100, after: $after) {
    nodes {
      ... on PullRequest {
        number
        title
        url
        isDraft
        state
        updatedAt
        headRefName
        repository {
          name
          nameWithOwner
        }
      }
    }
    pageInfo {
      hasNextPage
      endCursor
    }
  }
}`
var prCacheMu sync.Mutex
var prCache = map[string]prCacheEntry{}
var accountPRCache = map[string]accountPRCacheEntry{}
var ghTokenCache = map[string]string{}
var ghRequestSem = make(chan struct{}, ghRequestLimit)

type prCacheEntry struct {
	At time.Time
	PR map[string]PRItem
}

type accountPRCacheEntry struct {
	At      time.Time
	Records []SearchPRRecord
}

type PRItem struct {
	Number      int    `json:"number"`
	URL         string `json:"url"`
	IsDraft     bool   `json:"isDraft"`
	HeadRefName string `json:"headRefName"`
	State       string `json:"state"`
	MergedAt    string `json:"mergedAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type SearchPRItem struct {
	Number     int            `json:"number"`
	Title      string         `json:"title"`
	URL        string         `json:"url"`
	IsDraft    bool           `json:"isDraft"`
	State      string         `json:"state"`
	UpdatedAt  string         `json:"updatedAt"`
	Repository SearchRepoItem `json:"repository"`
}

type GraphQLSearchResponse struct {
	Data struct {
		Search struct {
			Nodes    []GraphQLSearchPRNode `json:"nodes"`
			PageInfo struct {
				HasNextPage bool   `json:"hasNextPage"`
				EndCursor   string `json:"endCursor"`
			} `json:"pageInfo"`
		} `json:"search"`
	} `json:"data"`
}

type GraphQLSearchPRNode struct {
	Number      int            `json:"number"`
	Title       string         `json:"title"`
	URL         string         `json:"url"`
	IsDraft     bool           `json:"isDraft"`
	State       string         `json:"state"`
	UpdatedAt   string         `json:"updatedAt"`
	HeadRefName string         `json:"headRefName"`
	Repository  SearchRepoItem `json:"repository"`
}

type SearchRepoItem struct {
	Name          string `json:"name"`
	NameWithOwner string `json:"nameWithOwner"`
}

type SearchPRRecord struct {
	Number       int
	Title        string
	URL          string
	IsDraft      bool
	Status       model.PRStatus
	UpdatedAt    *time.Time
	RepoLabel    string
	RepoFullName string
	HeadRefName  string
	HostLabel    string
	SSHTarget    string
}

func ParsePRListing(text string) (map[string]PRItem, error) {
	if strings.TrimSpace(text) == "" {
		return map[string]PRItem{}, nil
	}
	var items []PRItem
	if err := json.Unmarshal([]byte(text), &items); err != nil {
		return nil, err
	}
	byBranch := map[string]PRItem{}
	for _, item := range items {
		if item.HeadRefName == "" {
			continue
		}
		current, ok := byBranch[item.HeadRefName]
		if !ok || prSortKey(item) > prSortKey(current) {
			byBranch[item.HeadRefName] = item
		}
	}
	return byBranch, nil
}

func ParseSearchPRListing(text string, hostLabel, sshTarget string) ([]SearchPRRecord, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	var items []SearchPRItem
	if err := json.Unmarshal([]byte(text), &items); err != nil {
		return nil, err
	}
	records := make([]SearchPRRecord, 0, len(items))
	for _, item := range items {
		if item.URL == "" {
			continue
		}
		records = append(records, SearchPRRecord{
			Number:       item.Number,
			Title:        item.Title,
			URL:          item.URL,
			IsDraft:      item.IsDraft,
			Status:       coercePRStatus(PRItem{State: normalizePRState(item.State)}),
			UpdatedAt:    parseRFC3339(item.UpdatedAt),
			RepoLabel:    firstNonEmpty(item.Repository.NameWithOwner, item.Repository.Name),
			RepoFullName: item.Repository.NameWithOwner,
			HostLabel:    firstNonEmpty(hostLabel, "local"),
			SSHTarget:    sshTarget,
		})
	}
	return records, nil
}

func ParseGraphQLSearchPRListing(text string, hostLabel, sshTarget string) ([]SearchPRRecord, bool, string, error) {
	if strings.TrimSpace(text) == "" {
		return nil, false, "", nil
	}
	var response GraphQLSearchResponse
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		return nil, false, "", err
	}
	records := make([]SearchPRRecord, 0, len(response.Data.Search.Nodes))
	for _, item := range response.Data.Search.Nodes {
		if item.URL == "" {
			continue
		}
		records = append(records, SearchPRRecord{
			Number:       item.Number,
			Title:        item.Title,
			URL:          item.URL,
			IsDraft:      item.IsDraft,
			Status:       coercePRStatus(PRItem{State: normalizePRState(item.State)}),
			UpdatedAt:    parseRFC3339(item.UpdatedAt),
			RepoLabel:    firstNonEmpty(item.Repository.NameWithOwner, item.Repository.Name),
			RepoFullName: item.Repository.NameWithOwner,
			HeadRefName:  item.HeadRefName,
			HostLabel:    firstNonEmpty(hostLabel, "local"),
			SSHTarget:    sshTarget,
		})
	}
	return records, response.Data.Search.PageInfo.HasNextPage, response.Data.Search.PageInfo.EndCursor, nil
}

func ParseWorktreePorcelain(text string, repo GitRepoConfig, hostLabel, sshTarget string) []model.WorktreeRecord {
	if hostLabel == "" {
		hostLabel = "local"
	}
	blocks := strings.Split(text, "\n\n")
	records := []model.WorktreeRecord{}
	for _, block := range blocks {
		if strings.TrimSpace(block) == "" {
			continue
		}
		values := map[string]string{}
		for _, line := range strings.Split(block, "\n") {
			key, value, _ := strings.Cut(line, " ")
			values[key] = strings.TrimSpace(value)
		}
		path := values["worktree"]
		if path == "" {
			continue
		}
		branch := values["branch"]
		branch = strings.TrimPrefix(branch, "refs/heads/")
		records = append(records, model.WorktreeRecord{
			RepoRoot:  repo.Root,
			RepoLabel: repo.Label,
			Path:      path,
			Branch:    branch,
			Head:      values["HEAD"],
			IsMain:    path == repo.Root,
			HostLabel: hostLabel,
			SSHTarget: sshTarget,
		})
	}
	return records
}

func ParseBranchListing(text string, repo GitRepoConfig, prByBranch map[string]PRItem, hostLabel, sshTarget string) []model.BranchRecord {
	if hostLabel == "" {
		hostLabel = "local"
	}
	if prByBranch == nil {
		prByBranch = map[string]PRItem{}
	}
	records := []model.BranchRecord{}
	for rank, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		var committedAt *time.Time
		if parts[1] != "" {
			if parsed, err := time.Parse(time.RFC3339, parts[1]); err == nil {
				committedAt = &parsed
			}
		}
		upstream := ""
		if len(parts) > 2 {
			upstream = strings.TrimSpace(parts[2])
		}
		track := ""
		if len(parts) > 3 {
			track = strings.TrimSpace(parts[3])
		}
		pr := prByBranch[name]
		records = append(records, model.BranchRecord{
			RepoRoot:    repo.Root,
			RepoLabel:   repo.Label,
			Name:        name,
			CommittedAt: committedAt,
			RecencyRank: max(0, 500-rank),
			PRNumber:    pr.Number,
			PRURL:       pr.URL,
			PRIsDraft:   pr.IsDraft,
			PRStatus:    coercePRStatus(pr),
			Upstream:    upstream,
			AheadCount:  parseAheadCount(track),
			HostLabel:   hostLabel,
			SSHTarget:   sshTarget,
		})
	}
	return records
}

func CollectRepoItems(hostLabel, sshTarget string, runner GitRunner, repo GitRepoConfig, includePRs bool) ([]model.WorkItem, []string) {
	if !repo.Remote {
		if _, err := os.Stat(repo.Root); err != nil {
			return nil, []string{"Skipping missing repo root: " + repo.Root}
		}
	}
	type gitResult struct {
		kind string
		text string
		prs  map[string]PRItem
		err  error
	}
	resultCount := 3
	if includePRs {
		resultCount++
	}
	ch := make(chan gitResult, resultCount)
	go func() {
		text, err := runner.RunGit(repo.Root, WorktreeCommand)
		ch <- gitResult{kind: "worktree", text: text, err: err}
	}()
	go func() {
		text, err := runner.RunGit(repo.Root, BranchCommand)
		ch <- gitResult{kind: "branch", text: text, err: err}
	}()
	go func() {
		text, err := runner.RunGit(repo.Root, RemoteURLCommand)
		ch <- gitResult{kind: "remote-url", text: text, err: err}
	}()
	if includePRs {
		go func() {
			prs, err := FetchPRByBranch(repo.Root, repo.Remote, repo.GHAccountOrMe(), repo.GHAccount, sshTarget, runner)
			ch <- gitResult{kind: "pr", prs: prs, err: err}
		}()
	}
	worktreeText := ""
	branchText := ""
	repoFullName := ""
	prByBranch := map[string]PRItem{}
	var worktreeErr error
	var branchErr error
	for i := 0; i < resultCount; i++ {
		res := <-ch
		switch res.kind {
		case "worktree":
			worktreeText = res.text
			worktreeErr = res.err
		case "branch":
			branchText = res.text
			branchErr = res.err
		case "remote-url":
			if res.err == nil {
				repoFullName = parseGitHubRepoFullName(res.text)
			}
		case "pr":
			if res.err == nil {
				prByBranch = res.prs
			}
		}
	}
	if worktreeErr != nil {
		if errors.Is(worktreeErr, exec.ErrNotFound) {
			return nil, []string{"git is not installed"}
		}
		return nil, []string{fmt.Sprintf("%s/%s: %s", hostLabel, repo.Label, worktreeErr)}
	}
	if branchErr != nil {
		if errors.Is(branchErr, exec.ErrNotFound) {
			return nil, []string{"git is not installed"}
		}
		return nil, []string{fmt.Sprintf("%s/%s: %s", hostLabel, repo.Label, branchErr)}
	}
	branches := ParseBranchListing(branchText, repo, prByBranch, hostLabel, sshTarget)
	for i := range branches {
		branches[i].RepoFullName = repoFullName
	}
	worktrees := attachBranchMetadata(ParseWorktreePorcelain(worktreeText, repo, hostLabel, sshTarget), branches)
	for i := range worktrees {
		worktrees[i].RepoFullName = repoFullName
	}
	branches = linkActiveWorktrees(branches, worktrees)
	items := make([]model.WorkItem, 0, len(worktrees)+len(branches))
	for _, wt := range worktrees {
		items = append(items, worktreeToItem(wt))
	}
	for _, br := range branches {
		items = append(items, branchToItem(br))
	}
	return items, nil
}

func CollectSearchPRItems(hostLabel, sshTarget string, runner GitRunner, ghAccounts []string) ([]model.WorkItem, []string) {
	accounts := uniqueStrings(ghAccounts)
	if len(accounts) == 0 {
		accounts = []string{""}
	}

	type result struct {
		account string
		records []SearchPRRecord
		err     error
	}
	ch := make(chan result, len(accounts))
	for _, account := range accounts {
		go func(account string) {
			records, err := FetchAccountPRs(account, hostLabel, sshTarget, runner)
			ch <- result{account: account, records: records, err: err}
		}(account)
	}

	seen := map[string]bool{}
	items := []model.WorkItem{}
	warnings := []string{}
	for range accounts {
		res := <-ch
		if res.err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: gh PR fetch failed: %v", firstNonEmpty(hostLabel, "local"), res.err))
			continue
		}
		for _, record := range res.records {
			if seen[record.URL] {
				continue
			}
			seen[record.URL] = true
			items = append(items, searchPRToItem(record))
		}
	}
	return items, warnings
}

func EnrichPRItems(items []model.WorkItem) []model.WorkItem {
	linkedByURL := map[string]model.WorkItem{}
	prsByRepoBranch := map[string]model.WorkItem{}
	for _, item := range items {
		if item.Kind == model.KindPR {
			if item.RepoFullName != "" && item.Branch != "" {
				prsByRepoBranch[item.RepoFullName+"|"+item.Branch] = item
			}
			continue
		}
		if item.PRURL != "" {
			current, ok := linkedByURL[item.PRURL]
			if !ok || preferPRLinkSource(item, current) {
				linkedByURL[item.PRURL] = item
			}
		}
	}
	out := make([]model.WorkItem, len(items))
	for i, item := range items {
		if item.Kind == model.KindPR {
			if item.PRURL != "" {
				if linked, ok := linkedByURL[item.PRURL]; ok {
					item.RepoRoot = linked.RepoRoot
					item.RepoLabel = firstNonEmpty(linked.RepoLabel, item.RepoLabel)
					item.RepoFullName = firstNonEmpty(linked.RepoFullName, item.RepoFullName)
					item.Path = linked.Path
					item.Branch = linked.Branch
					item.Upstream = linked.Upstream
					item.AheadCount = linked.AheadCount
				}
			}
			out[i] = item
			continue
		}
		if item.PRURL == "" && item.RepoFullName != "" && item.Branch != "" {
			if pr, ok := prsByRepoBranch[item.RepoFullName+"|"+item.Branch]; ok {
				item.PRNumber = pr.PRNumber
				item.PRURL = pr.PRURL
				item.PRIsDraft = pr.PRIsDraft
				item.PRStatus = pr.PRStatus
			}
		}
		out[i] = item
	}
	return out
}

func FetchPRByBranch(repoRoot string, remote bool, author, ghAccount, sshTarget string, runner GitRunner) (map[string]PRItem, error) {
	key := fmt.Sprintf("%t|%s|%s|%s", remote, repoRoot, sshTarget, author)
	now := time.Now()
	prCacheMu.Lock()
	if cached, ok := prCache[key]; ok && now.Sub(cached.At) < prCacheTTL {
		prCacheMu.Unlock()
		return cached.PR, nil
	}
	prCacheMu.Unlock()
	release := acquireGHRequestSlot()
	defer release()
	token, err := resolveGHToken(ghAccount, sshTarget, runner)
	if err != nil {
		return nil, err
	}
	text, err := runner.RunGH(repoRoot, buildPRArgs(author), token)
	if err != nil {
		return nil, err
	}
	prs, err := ParsePRListing(text)
	if err != nil {
		return nil, err
	}
	prCacheMu.Lock()
	prCache[key] = prCacheEntry{At: now, PR: prs}
	prCacheMu.Unlock()
	return prs, nil
}

func FetchAccountPRs(ghAccount, hostLabel, sshTarget string, runner GitRunner) ([]SearchPRRecord, error) {
	key := sshTarget + "|" + ghAccount
	now := time.Now()
	prCacheMu.Lock()
	if cached, ok := accountPRCache[key]; ok && now.Sub(cached.At) < prCacheTTL {
		prCacheMu.Unlock()
		return cached.Records, nil
	}
	prCacheMu.Unlock()
	release := acquireGHRequestSlot()
	defer release()
	token, err := resolveGHToken(ghAccount, sshTarget, runner)
	if err != nil {
		return nil, err
	}
	records := []SearchPRRecord{}
	cursor := ""
	for len(records) < 200 {
		text, err := runner.RunGH("", buildAccountPRArgs(cursor), token)
		if err != nil {
			return nil, err
		}
		page, hasNextPage, endCursor, err := ParseGraphQLSearchPRListing(text, hostLabel, sshTarget)
		if err != nil {
			return nil, err
		}
		records = append(records, page...)
		if !hasNextPage || endCursor == "" {
			break
		}
		cursor = endCursor
	}
	if len(records) > 200 {
		records = records[:200]
	}
	prCacheMu.Lock()
	accountPRCache[key] = accountPRCacheEntry{At: now, Records: records}
	prCacheMu.Unlock()
	return records, nil
}

func acquireGHRequestSlot() func() {
	ghRequestSem <- struct{}{}
	return func() {
		<-ghRequestSem
	}
}

func buildPRArgs(author string) []string {
	return []string{"pr", "list", "--author", author, "--state", "all", "--json", "number,url,isDraft,headRefName,state,mergedAt,updatedAt", "--limit", "200"}
}

func buildAccountPRArgs(after string) []string {
	args := []string{
		"api", "graphql",
		"-f", "query=" + authoredPRSearchQuery,
		"-f", "searchQuery=is:pr author:@me sort:updated-desc",
	}
	if after != "" {
		args = append(args, "-f", "after="+after)
	}
	return args
}

func resolveGHToken(account, sshTarget string, runner GitRunner) (string, error) {
	if account == "" {
		return "", nil
	}
	key := sshTarget + "|" + account
	prCacheMu.Lock()
	if token, ok := ghTokenCache[key]; ok {
		prCacheMu.Unlock()
		return token, nil
	}
	prCacheMu.Unlock()
	token, err := runner.RunGHAuth([]string{"token", "--user", account})
	if err != nil {
		return "", err
	}
	token = strings.TrimSpace(token)
	prCacheMu.Lock()
	ghTokenCache[key] = token
	prCacheMu.Unlock()
	return token, nil
}

func linkActiveWorktrees(branches []model.BranchRecord, worktrees []model.WorktreeRecord) []model.BranchRecord {
	active := map[string]string{}
	for _, wt := range worktrees {
		if wt.Branch != "" {
			active[wt.Branch] = wt.Path
		}
	}
	out := make([]model.BranchRecord, len(branches))
	copy(out, branches)
	for i := range out {
		out[i].ActivePath = active[out[i].Name]
	}
	return out
}

func attachBranchMetadata(worktrees []model.WorktreeRecord, branches []model.BranchRecord) []model.WorktreeRecord {
	byName := map[string]model.BranchRecord{}
	for _, br := range branches {
		byName[br.Name] = br
	}
	out := make([]model.WorktreeRecord, len(worktrees))
	copy(out, worktrees)
	for i := range out {
		br, ok := byName[out[i].Branch]
		if !ok {
			continue
		}
		out[i].LastActivityAt = br.CommittedAt
		out[i].PRNumber = br.PRNumber
		out[i].PRURL = br.PRURL
		out[i].PRIsDraft = br.PRIsDraft
		out[i].PRStatus = br.PRStatus
		out[i].Upstream = br.Upstream
		out[i].AheadCount = br.AheadCount
	}
	return out
}

func worktreeToItem(wt model.WorktreeRecord) model.WorkItem {
	branch := wt.Branch
	if branch == "" {
		branch = "detached HEAD"
	}
	var action model.ShellAction = actions.CdAction{Path: wt.Path}
	if wt.SSHTarget != "" {
		action = actions.RemoteShellAction{Path: wt.Path, SSHTarget: wt.SSHTarget}
	}
	return model.WorkItem{
		Kind:           model.KindWorktree,
		Title:          baseName(wt.Path),
		Subtitle:       fmt.Sprintf("%s • %s • %s • %s", wt.HostLabel, wt.RepoLabel, branch, wt.Path),
		RepoLabel:      wt.RepoLabel,
		RepoRoot:       wt.RepoRoot,
		RepoFullName:   wt.RepoFullName,
		Path:           wt.Path,
		Branch:         wt.Branch,
		SearchText:     strings.Join([]string{wt.HostLabel, baseName(wt.Path), wt.Path, wt.RepoLabel, wt.Branch}, " "),
		ScoreHint:      map[bool]int{true: 250, false: 150}[wt.IsMain],
		Action:         action,
		PRNumber:       wt.PRNumber,
		PRURL:          wt.PRURL,
		PRIsDraft:      wt.PRIsDraft,
		PRStatus:       wt.PRStatus,
		LastActivityAt: wt.LastActivityAt,
		Upstream:       wt.Upstream,
		AheadCount:     wt.AheadCount,
		IsPrimary:      wt.IsMain,
		HostLabel:      wt.HostLabel,
		SSHTarget:      wt.SSHTarget,
	}
}

func branchToItem(br model.BranchRecord) model.WorkItem {
	subtitle := fmt.Sprintf("%s • %s", br.HostLabel, br.RepoLabel)
	var action model.ShellAction = actions.CheckoutAction{RepoRoot: br.RepoRoot, Branch: br.Name, SSHTarget: br.SSHTarget}
	if br.ActivePath != "" {
		subtitle = fmt.Sprintf("%s • %s • %s", br.HostLabel, br.RepoLabel, br.ActivePath)
		action = actions.CdAction{Path: br.ActivePath}
		if br.SSHTarget != "" {
			action = actions.RemoteShellAction{Path: br.ActivePath, SSHTarget: br.SSHTarget}
		}
	}
	return model.WorkItem{
		Kind:           model.KindBranch,
		Title:          br.Name,
		Subtitle:       subtitle,
		RepoLabel:      br.RepoLabel,
		RepoRoot:       br.RepoRoot,
		RepoFullName:   br.RepoFullName,
		Path:           br.ActivePath,
		Branch:         br.Name,
		SearchText:     strings.Join([]string{br.HostLabel, br.Name, br.RepoLabel, br.RepoRoot, br.ActivePath}, " "),
		ScoreHint:      br.RecencyRank,
		Action:         action,
		PRNumber:       br.PRNumber,
		PRURL:          br.PRURL,
		PRIsDraft:      br.PRIsDraft,
		PRStatus:       br.PRStatus,
		LastActivityAt: br.CommittedAt,
		Upstream:       br.Upstream,
		AheadCount:     br.AheadCount,
		HostLabel:      br.HostLabel,
		SSHTarget:      br.SSHTarget,
	}
}

func searchPRToItem(pr SearchPRRecord) model.WorkItem {
	return model.WorkItem{
		Kind:           model.KindPR,
		Title:          pr.Title,
		Subtitle:       fmt.Sprintf("%s • %s • #%d", pr.HostLabel, pr.RepoLabel, pr.Number),
		RepoLabel:      firstNonEmpty(pr.RepoLabel, pr.RepoFullName),
		RepoFullName:   pr.RepoFullName,
		Branch:         pr.HeadRefName,
		SearchText:     strings.Join([]string{pr.HostLabel, "pr", strconv.Itoa(pr.Number), pr.Title, pr.RepoLabel, pr.RepoFullName, pr.URL}, " "),
		ScoreHint:      400,
		Action:         actions.OpenInBrowserAction{URL: pr.URL},
		PRNumber:       pr.Number,
		PRURL:          pr.URL,
		PRIsDraft:      pr.IsDraft,
		PRStatus:       pr.Status,
		LastActivityAt: pr.UpdatedAt,
		HostLabel:      pr.HostLabel,
		SSHTarget:      pr.SSHTarget,
	}
}

func (r GitRepoConfig) GHAccountOrMe() string {
	if r.GHAccount != "" {
		return r.GHAccount
	}
	return "@me"
}

func coercePRStatus(pr PRItem) model.PRStatus {
	switch {
	case pr.MergedAt != "" || pr.State == "MERGED":
		return model.PRMerged
	case pr.State == "CLOSED":
		return model.PRClosed
	case pr.State == "OPEN":
		return model.PROpen
	default:
		return ""
	}
}

func normalizePRState(state string) string {
	return strings.ToUpper(strings.TrimSpace(state))
}

func parseAheadCount(track string) *int {
	if track == "" {
		return nil
	}
	if m := trackAheadPattern.FindStringSubmatch(track); m != nil {
		value, _ := strconv.Atoi(m[1])
		return &value
	}
	if strings.Contains(track, "behind") || strings.Contains(track, "up to date") {
		zero := 0
		return &zero
	}
	return nil
}

func prSortKey(pr PRItem) string {
	return fmt.Sprintf("%010d|%s", pr.Number, pr.UpdatedAt)
}

func baseName(path string) string {
	path = strings.TrimRight(path, "/")
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func parseRFC3339(value string) *time.Time {
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	return &parsed
}

func parseGitHubRepoFullName(rawURL string) string {
	remoteURL := strings.TrimSpace(rawURL)
	if remoteURL == "" {
		return ""
	}
	if strings.HasPrefix(remoteURL, "git@") {
		if idx := strings.Index(remoteURL, ":"); idx >= 0 && idx+1 < len(remoteURL) {
			return trimDotGit(remoteURL[idx+1:])
		}
		return ""
	}
	parsed, err := url.Parse(remoteURL)
	if err != nil {
		return ""
	}
	path := strings.TrimPrefix(parsed.Path, "/")
	if parsed.Scheme == "ssh" {
		path = strings.TrimPrefix(path, "/")
	}
	return trimDotGit(path)
}

func trimDotGit(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, ".git")
	if strings.Count(path, "/") < 1 {
		return ""
	}
	return path
}

func preferPRLinkSource(candidate, current model.WorkItem) bool {
	if current.Kind != model.KindWorktree && candidate.Kind == model.KindWorktree {
		return true
	}
	if current.Path == "" && candidate.Path != "" {
		return true
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func SortItems(items []model.WorkItem) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].Key() < items[j].Key() })
}
