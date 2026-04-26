package loader

import (
	"github.com/Khady/workdash/internal/config"
	"github.com/Khady/workdash/internal/model"
	"github.com/Khady/workdash/internal/source"
)

type SnapshotEmitter func(items []model.WorkItem, warnings []string, done bool)

func LoadItems(configPath, cwd string, includePRs bool) ([]model.WorkItem, []string, error) {
	result, err := config.Load(configPath)
	if err != nil {
		return nil, nil, err
	}
	hosts := BuildHostContexts(result.Config, cwd)
	searchAccounts := searchAccountsByHost(hosts)
	type res struct {
		items    []model.WorkItem
		warnings []string
	}
	ch := make(chan res, len(hosts))
	for _, host := range hosts {
		go func(host HostContext) {
			loaded := LoadHostItems(host, includePRs, searchAccounts[hostSearchKey(host)])
			ch <- res{items: loaded.Items, warnings: loaded.Warnings}
		}(host)
	}
	items := []model.WorkItem{}
	warnings := append([]string{}, result.Warnings...)
	for range hosts {
		loaded := <-ch
		items = append(items, loaded.items...)
		warnings = append(warnings, loaded.warnings...)
	}
	return source.EnrichTmuxItems(source.EnrichPRItems(items)), unique(warnings), nil
}

func LoadHostItems(host HostContext, includePRs bool, searchAccounts []string) HostLoadResult {
	type taskResult struct {
		items    []model.WorkItem
		warnings []string
	}
	total := len(host.Repos)
	if host.TmuxEnabled {
		total++
	}
	if searchAccounts != nil {
		total++
	}
	ch := make(chan taskResult, total)
	for _, repo := range host.Repos {
		go func(repo RepoConfig) {
			repoItems, repoWarnings := CollectRepoItems(host, repo, includePRs)
			ch <- taskResult{items: repoItems, warnings: repoWarnings}
		}(repo)
	}
	if host.TmuxEnabled {
		go func() {
			tmuxItems, tmuxWarnings := source.CollectTmuxItemsForHost(host.Label, host.SSHTarget, host.Tmux, host.InsideTmux)
			ch <- taskResult{items: tmuxItems, warnings: tmuxWarnings}
		}()
	}
	if searchAccounts != nil {
		go func() {
			prItems, prWarnings := source.CollectSearchPRItems(host.Label, host.SSHTarget, host.Git, searchAccounts)
			ch <- taskResult{items: prItems, warnings: prWarnings}
		}()
	}
	items := []model.WorkItem{}
	warnings := []string{}
	for i := 0; i < total; i++ {
		loaded := <-ch
		items = append(items, loaded.items...)
		warnings = append(warnings, loaded.warnings...)
	}
	return HostLoadResult{Items: source.EnrichTmuxItems(source.EnrichPRItems(items)), Warnings: warnings}
}

func CollectRepoItems(host HostContext, repo RepoConfig, includePRs bool) ([]model.WorkItem, []string) {
	return source.CollectRepoItems(
		host.Label,
		host.SSHTarget,
		host.Git,
		source.GitRepoConfig{Root: repo.Root, Label: repo.Label, GHAccount: repo.GHAccount, Remote: repo.Remote},
		includePRs,
	)
}

func StreamItems(configPath, cwd string, emit SnapshotEmitter) error {
	result, err := config.Load(configPath)
	if err != nil {
		return err
	}
	hosts := BuildHostContexts(result.Config, cwd)
	searchAccounts := searchAccountsByHost(hosts)
	itemsByKey := map[string]model.WorkItem{}
	warnings := append([]string{}, result.Warnings...)
	emit(nil, unique(warnings), false)

	type loadResult struct {
		host  HostContext
		repo  *RepoConfig
		items []model.WorkItem
		warns []string
	}
	total := 0
	ch := make(chan loadResult, 32)
	for _, host := range hosts {
		for _, repo := range host.Repos {
			total++
			go func(host HostContext, repo RepoConfig) {
				items, warns := CollectRepoItems(host, repo, false)
				ch <- loadResult{host: host, repo: &repo, items: items, warns: warns}
			}(host, repo)
		}
		if host.TmuxEnabled {
			total++
			go func(host HostContext) {
				items, warns := source.CollectTmuxItemsForHost(host.Label, host.SSHTarget, host.Tmux, host.InsideTmux)
				ch <- loadResult{host: host, items: items, warns: warns}
			}(host)
		}
		if accounts := searchAccounts[hostSearchKey(host)]; accounts != nil {
			total++
			go func(host HostContext, accounts []string) {
				items, warns := source.CollectSearchPRItems(host.Label, host.SSHTarget, host.Git, accounts)
				ch <- loadResult{host: host, items: items, warns: warns}
			}(host, accounts)
		}
	}
	for i := 0; i < total; i++ {
		res := <-ch
		warnings = append(warnings, res.warns...)
		for _, item := range res.items {
			itemsByKey[item.Key()] = item
		}
		emit(source.EnrichTmuxItems(source.EnrichPRItems(values(itemsByKey))), unique(warnings), false)
	}
	emit(source.EnrichTmuxItems(source.EnrichPRItems(values(itemsByKey))), unique(warnings), true)
	return nil
}

func values(m map[string]model.WorkItem) []model.WorkItem {
	out := make([]model.WorkItem, 0, len(m))
	for _, item := range m {
		out = append(out, item)
	}
	return out
}

func unique(values []string) []string {
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

func ghAccountsForHost(host HostContext) []string {
	accounts := make([]string, 0, len(host.Repos))
	for _, repo := range host.Repos {
		accounts = append(accounts, repo.GHAccount)
	}
	return accounts
}

func searchAccountsByHost(hosts []HostContext) map[string][]string {
	out := map[string][]string{}
	claimed := map[string]bool{}
	for _, host := range hosts {
		key := hostSearchKey(host)
		accounts := uniqueNonEmpty(ghAccountsForHost(host))
		if len(accounts) == 0 {
			out[key] = []string{""}
			continue
		}
		selected := make([]string, 0, len(accounts))
		for _, account := range accounts {
			if claimed[account] {
				continue
			}
			claimed[account] = true
			selected = append(selected, account)
		}
		if len(selected) == 0 {
			out[key] = nil
			continue
		}
		out[key] = selected
	}
	return out
}

func hostSearchKey(host HostContext) string {
	return host.Label + "|" + host.SSHTarget
}

func uniqueNonEmpty(values []string) []string {
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

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
