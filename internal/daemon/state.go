package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Khady/workdash/internal/actions"
	"github.com/Khady/workdash/internal/config"
	"github.com/Khady/workdash/internal/loader"
	"github.com/Khady/workdash/internal/model"
	"github.com/Khady/workdash/internal/source"
)

type actionCapability struct {
	action       model.ShellAction
	confirmation *actions.ConfirmationPrompt
	createdAt    time.Time
}

type State struct {
	configPath string

	mu          sync.Mutex
	items       []model.WorkItem
	warnings    []string
	cfg         config.DashboardConfig
	loadedAt    time.Time
	refreshing  bool
	refreshDone chan struct{}
	refreshErr  error
	extraRepos  map[string]bool
	tokens      map[string]actionCapability
}

func NewState(configPath string) *State {
	return &State{
		configPath: configPath,
		extraRepos: map[string]bool{},
		tokens:     map[string]actionCapability{},
	}
}

func (s *State) Start() {
	go func() { _ = s.Refresh(context.Background()) }()
}

func (s *State) Refresh(ctx context.Context) error {
	s.mu.Lock()
	if s.refreshing {
		done := s.refreshDone
		s.mu.Unlock()
		select {
		case <-done:
			s.mu.Lock()
			err := s.refreshErr
			s.mu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.refreshing = true
	s.refreshDone = make(chan struct{})
	done := s.refreshDone
	extras := make([]string, 0, len(s.extraRepos))
	for root := range s.extraRepos {
		extras = append(extras, root)
	}
	s.mu.Unlock()

	items, warnings, cfg, err := s.load(extras)

	s.mu.Lock()
	if err == nil {
		s.items = items
		s.warnings = warnings
		s.cfg = cfg
		s.loadedAt = time.Now()
	}
	s.refreshErr = err
	s.refreshing = false
	close(done)
	s.mu.Unlock()
	return err
}

func (s *State) load(extras []string) ([]model.WorkItem, []string, config.DashboardConfig, error) {
	cfgResult, err := config.Load(s.configPath)
	if err != nil {
		return nil, nil, config.DashboardConfig{}, err
	}
	var finalItems []model.WorkItem
	var finalWarnings []string
	err = loader.StreamItems(s.configPath, "/", func(items []model.WorkItem, warnings []string, done bool) {
		if done {
			finalItems = items
			finalWarnings = warnings
		}
	})
	if err != nil {
		return nil, nil, config.DashboardConfig{}, err
	}
	for _, root := range extras {
		extra, warnings := collectExtraRepo(root)
		finalItems = replaceRepoItems(finalItems, root, extra)
		finalWarnings = append(finalWarnings, warnings...)
	}
	finalItems = source.EnrichTmuxItems(source.EnrichPRItems(finalItems))
	return finalItems, uniqueStrings(finalWarnings), cfgResult.Config, nil
}

func (s *State) Snapshot(ctx context.Context) (Response, error) {
	if err := s.waitReady(ctx); err != nil {
		return Response{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return Response{
		OK:         true,
		Status:     "ready",
		LoadedAt:   s.loadedAt,
		Refreshing: s.refreshing,
		Items:      append([]model.WorkItem{}, s.items...),
		Warnings:   append([]string{}, s.warnings...),
	}, nil
}

func (s *State) EnsureRepo(ctx context.Context, cwd string) (Response, error) {
	if err := s.waitReady(ctx); err != nil {
		return Response{}, err
	}
	root := loader.FindGitRoot(cwd)
	if root == "" || s.isConfiguredRepo(root) {
		return s.Snapshot(ctx)
	}

	s.mu.Lock()
	if s.extraRepos[root] {
		s.mu.Unlock()
		return s.Snapshot(ctx)
	}
	s.extraRepos[root] = true
	s.mu.Unlock()

	items, warnings := collectExtraRepo(root)
	s.mu.Lock()
	s.items = replaceRepoItems(s.items, root, items)
	s.items = source.EnrichTmuxItems(source.EnrichPRItems(s.items))
	s.warnings = uniqueStrings(append(s.warnings, warnings...))
	s.mu.Unlock()
	return s.Snapshot(ctx)
}

func (s *State) isConfiguredRepo(root string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, repo := range s.cfg.LocalHost.Repos {
		if repo.Root == root {
			return true
		}
	}
	return false
}

func (s *State) waitReady(ctx context.Context) error {
	s.mu.Lock()
	if !s.loadedAt.IsZero() {
		s.mu.Unlock()
		return nil
	}
	done := s.refreshDone
	err := s.refreshErr
	s.mu.Unlock()
	if done == nil {
		if err != nil {
			return err
		}
		return fmt.Errorf("daemon has not started loading")
	}
	select {
	case <-done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.refreshErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *State) ActionsForPR(ctx context.Context, rawURL string) (Response, error) {
	return s.browserActionsForPR(ctx, rawURL, true)
}

func (s *State) SummaryForPR(ctx context.Context, rawURL string) (Response, error) {
	response, err := s.browserActionsForPR(ctx, rawURL, false)
	if err != nil {
		return Response{}, err
	}
	for _, group := range response.Groups {
		for _, action := range group.Actions {
			if action.Enabled {
				response.ActionCount++
			}
		}
	}
	response.Groups = nil
	return response, nil
}

func (s *State) browserActionsForPR(ctx context.Context, rawURL string, issueTokens bool) (Response, error) {
	if err := s.waitReady(ctx); err != nil {
		return Response{}, err
	}
	canonical, err := CanonicalPRURL(rawURL)
	if err != nil {
		return Response{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for token, capability := range s.tokens {
		if !capability.createdAt.IsZero() && time.Since(capability.createdAt) > 15*time.Minute {
			delete(s.tokens, token)
		}
	}
	groups := []ActionGroup{}
	groupByLocation := map[string]int{}
	seenActions := map[string]bool{}
	items := append([]model.WorkItem{}, s.items...)
	sort.SliceStable(items, func(i, j int) bool {
		left, right := browserItemPriority(items[i]), browserItemPriority(items[j])
		if left != right {
			return left < right
		}
		return items[i].Key() < items[j].Key()
	})
	for _, item := range items {
		itemURL, err := CanonicalPRURL(item.PRURL)
		if err != nil || itemURL != canonical {
			continue
		}
		options := actions.ActionsForItem(item, s.cfg.Commands, s.cfg.TerminalLauncher != "", s.cfg.TerminalLauncherDefault)
		locationKey, group := browserActionLocation(item)
		groupIndex, exists := groupByLocation[locationKey]
		if !exists {
			groupIndex = len(groups)
			groupByLocation[locationKey] = groupIndex
			groups = append(groups, group)
		}
		for _, option := range options {
			projected, capability := s.projectAction(option)
			key := browserActionIdentity(projected, capability)
			if seenActions[key] {
				continue
			}
			seenActions[key] = true
			if projected.Enabled && issueTokens {
				projected.Token = newToken()
				capability.createdAt = time.Now()
				s.tokens[projected.Token] = capability
			}
			groups[groupIndex].Actions = append(groups[groupIndex].Actions, projected)
		}
	}
	groups = compactActionGroups(groups)
	if len(groups) == 0 {
		return Response{}, fmt.Errorf("no Workdash items match this pull request")
	}
	return Response{
		OK: true, Status: "ready", LoadedAt: s.loadedAt, Refreshing: s.refreshing,
		Warnings: append([]string{}, s.warnings...), Groups: groups,
	}, nil
}

func browserItemPriority(item model.WorkItem) int {
	switch item.Kind {
	case model.KindWorktree:
		return 0
	case model.KindPR:
		return 1
	case model.KindBranch:
		return 2
	case model.KindTmux:
		return 3
	default:
		return 4
	}
}

func browserActionLocation(item model.WorkItem) (string, ActionGroup) {
	group := ActionGroup{
		ItemKey: item.Key(), Kind: item.Kind, Title: item.Title, Subtitle: item.Subtitle,
		Host: item.HostLabel, Path: item.Path,
	}
	hostKey := item.HostLabel + "\x00" + item.SSHTarget
	if item.Path != "" {
		group.Kind = model.KindWorktree
		group.Title = filepath.Base(item.Path)
		group.Subtitle = item.Path
		return "path\x00" + hostKey + "\x00" + item.Path, group
	}
	if item.RepoRoot != "" && item.Branch != "" {
		group.Kind = model.KindBranch
		group.Title = item.Branch
		group.Subtitle = item.RepoRoot
		return "branch\x00" + hostKey + "\x00" + item.RepoRoot + "\x00" + item.Branch, group
	}
	if item.Kind == model.KindTmux && item.Session != "" {
		return "tmux\x00" + hostKey + "\x00" + item.Session, group
	}
	return "item\x00" + item.Key(), group
}

func browserActionIdentity(projected BrowserAction, capability actionCapability) string {
	if capability.action == nil {
		return "unavailable\x00" + projected.Label + "\x00" + projected.Detail + "\x00" + projected.UnavailableReason
	}
	mode := "terminal"
	if inline, ok := actions.AsInlineAction(capability.action); ok {
		mode = "inline"
		if configured, ok := inline.(actions.ConfiguredInlineAction); ok {
			mode += ":" + configured.Run
		}
	}
	confirmation := projected.ConfirmationTitle + "\x00" + projected.ConfirmationDetail + "\x00" + projected.ConfirmLabel
	return mode + "\x00" + capability.action.ToShell() + "\x00" + confirmation
}

func compactActionGroups(groups []ActionGroup) []ActionGroup {
	out := groups[:0]
	for _, group := range groups {
		if len(group.Actions) > 0 {
			out = append(out, group)
		}
	}
	return out
}

func (s *State) projectAction(option actions.ItemActionOption) (BrowserAction, actionCapability) {
	out := BrowserAction{Label: option.Label, Detail: option.Detail, Enabled: true}
	capability := actionCapability{confirmation: option.Confirmation}
	action, ok := option.Action.(model.ShellAction)
	if !ok {
		out.Enabled = false
		out.UnavailableReason = "unsupported action"
		return out, capability
	}
	if _, ok := actions.AsInlineAction(action); ok {
		capability.action = action
	} else if actions.IsTerminalLaunchAction(action) {
		if s.cfg.TerminalLauncher == "" {
			out.Enabled = false
			out.UnavailableReason = "terminal_launcher is not configured"
		} else {
			capability.action = action
		}
	} else if actions.TerminalLaunchCommand(action) != "" {
		if s.cfg.TerminalLauncher == "" {
			out.Enabled = false
			out.UnavailableReason = "terminal_launcher is not configured"
		} else {
			capability.action = actions.TerminalLaunchAction{Action: action}
		}
	} else {
		out.Enabled = false
		out.UnavailableReason = "action requires a shell"
	}
	if option.Confirmation != nil {
		out.ConfirmationTitle = option.Confirmation.Title
		out.ConfirmationDetail = option.Confirmation.Detail
		out.ConfirmLabel = option.Confirmation.ConfirmLabel
	}
	return out, capability
}

func (s *State) Execute(token string, confirmed bool) (Response, error) {
	s.mu.Lock()
	capability, ok := s.tokens[token]
	if ok && capability.confirmation != nil && !confirmed {
		s.mu.Unlock()
		return Response{}, fmt.Errorf("confirmation required")
	}
	if ok {
		delete(s.tokens, token)
	}
	launcher := s.cfg.TerminalLauncher
	s.mu.Unlock()
	if !ok {
		return Response{}, fmt.Errorf("unknown, stale, or already-used action token")
	}
	if inline, ok := actions.AsInlineAction(capability.action); ok {
		if err := actions.ExecuteInlineAction(inline); err != nil {
			return Response{}, err
		}
	} else {
		if err := actions.ExecuteTerminalLaunchActionAsync(capability.action, launcher); err != nil {
			return Response{}, err
		}
	}
	return Response{OK: true, Status: "completed"}, nil
}

func CanonicalPRURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") ||
		parsed.User != nil || parsed.Port() != "" {
		return "", fmt.Errorf("current page is not a github.com pull request")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 4 || parts[0] == "" || parts[1] == "" || parts[2] != "pull" || parts[3] == "" {
		return "", fmt.Errorf("current page is not a github.com pull request")
	}
	number, err := strconv.Atoi(parts[3])
	if err != nil || number <= 0 {
		return "", fmt.Errorf("current page is not a github.com pull request")
	}
	return "https://github.com/" + strings.ToLower(parts[0]) + "/" + strings.ToLower(parts[1]) + "/pull/" + strconv.Itoa(number), nil
}

func collectExtraRepo(root string) ([]model.WorkItem, []string) {
	return source.CollectRepoItems(
		"local", "", source.LocalGitRunner{},
		source.GitRepoConfig{Root: root, Label: filepath.Base(root)},
		false,
	)
}

func replaceRepoItems(current []model.WorkItem, root string, replacement []model.WorkItem) []model.WorkItem {
	out := make([]model.WorkItem, 0, len(current)+len(replacement))
	for _, item := range current {
		if item.HostLabel == "local" && item.RepoRoot == root &&
			(item.Kind == model.KindWorktree || item.Kind == model.KindBranch) {
			continue
		}
		out = append(out, item)
	}
	return append(out, replacement...)
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func newToken() string {
	var data [24]byte
	if _, err := rand.Read(data[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(data[:])
}
