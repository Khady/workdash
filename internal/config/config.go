package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type LocalRepoConfig struct {
	Root      string
	Label     string
	GHAccount string
}

type RemoteRepoConfig struct {
	Root      string
	Label     string
	GHAccount string
}

type LocalHostConfig struct {
	Label       string
	TmuxEnabled bool
	Repos       []LocalRepoConfig
}

type RemoteHostConfig struct {
	Label       string
	SSHTarget   string
	TmuxEnabled bool
	Repos       []RemoteRepoConfig
}

type DashboardConfig struct {
	LocalHost               LocalHostConfig
	RemoteHosts             []RemoteHostConfig
	TerminalLauncher        string
	TerminalLauncherDefault bool
	ConfigEditor            string
	Commands                []CommandConfig
}

type CommandConfig struct {
	ID                string
	Shortcut          rune
	Label             string
	Detail            string
	Command           string
	Cwd               string
	Run               string
	Scope             string
	Contexts          []string
	Requires          []string
	Remote            bool
	RemoteInteractive bool
	Relaunch          string
}

type LoadResult struct {
	Config   DashboardConfig
	Warnings []string
}

type rawConfig struct {
	Tmux                    *bool           `toml:"tmux"`
	TerminalLauncher        *string         `toml:"terminal_launcher"`
	TerminalLauncherDefault *bool           `toml:"terminal_launcher_default"`
	ConfigEditor            *string         `toml:"config_editor"`
	Repos                   []rawRepo       `toml:"repos"`
	Hosts                   []rawRemoteHost `toml:"hosts"`
	Commands                []rawCommand    `toml:"commands"`
}

type rawRepo struct {
	Root      *string `toml:"root"`
	Label     *string `toml:"label"`
	GHAccount *string `toml:"gh_account"`
}

type rawRemoteHost struct {
	Label     string    `toml:"label"`
	SSHTarget string    `toml:"ssh_target"`
	Tmux      *bool     `toml:"tmux"`
	Repos     []rawRepo `toml:"repos"`
}

type rawCommand struct {
	ID                *string  `toml:"id"`
	Shortcut          *string  `toml:"shortcut"`
	Label             *string  `toml:"label"`
	Detail            *string  `toml:"detail"`
	Command           *string  `toml:"command"`
	Cwd               *string  `toml:"cwd"`
	Run               *string  `toml:"run"`
	Scope             *string  `toml:"scope"`
	Contexts          []string `toml:"contexts"`
	Requires          []string `toml:"requires"`
	Remote            *bool    `toml:"remote"`
	RemoteInteractive *bool    `toml:"remote_interactive"`
	Relaunch          *string  `toml:"relaunch"`
}

func DefaultConfigPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "workdash", "config.toml")
}

func Load(path string) (LoadResult, error) {
	if path == "" {
		path = DefaultConfigPath()
	}
	cfg := defaultDashboardConfig()
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return LoadResult{Config: cfg, Warnings: []string{"Config file not found: " + path}}, nil
	}
	if err != nil {
		return LoadResult{}, err
	}

	var raw rawConfig
	if err := toml.Unmarshal(content, &raw); err != nil {
		return LoadResult{}, fmt.Errorf("Invalid config file %s: %w", path, err)
	}

	if raw.Tmux != nil {
		cfg.LocalHost.TmuxEnabled = *raw.Tmux
	}
	if raw.TerminalLauncher != nil {
		if strings.TrimSpace(*raw.TerminalLauncher) == "" {
			return LoadResult{}, fmt.Errorf("`terminal_launcher` must be a non-empty string")
		}
		cfg.TerminalLauncher = strings.TrimSpace(*raw.TerminalLauncher)
		if !strings.Contains(cfg.TerminalLauncher, "{command}") {
			return LoadResult{}, fmt.Errorf("`terminal_launcher` must include a {command} placeholder")
		}
	}
	if raw.TerminalLauncherDefault != nil {
		cfg.TerminalLauncherDefault = *raw.TerminalLauncherDefault
	}
	if raw.ConfigEditor != nil {
		if strings.TrimSpace(*raw.ConfigEditor) == "" {
			return LoadResult{}, fmt.Errorf("`config_editor` must be a non-empty string")
		}
		cfg.ConfigEditor = strings.TrimSpace(*raw.ConfigEditor)
	}
	if raw.Commands != nil {
		cfg.Commands = nil
		for i, command := range raw.Commands {
			parsed, err := parseCommand(command, fmt.Sprintf("commands[%d]", i))
			if err != nil {
				return LoadResult{}, err
			}
			cfg.Commands = append(cfg.Commands, parsed)
		}
	}

	warnings := []string{}
	for i, repo := range raw.Repos {
		parsed, err := parseLocalRepo(repo, fmt.Sprintf("repos[%d]", i))
		if err != nil {
			return LoadResult{}, err
		}
		if _, err := os.Stat(parsed.Root); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				warnings = append(warnings, "Skipping missing repo root: "+parsed.Root)
				continue
			}
			return LoadResult{}, err
		}
		cfg.LocalHost.Repos = append(cfg.LocalHost.Repos, parsed)
	}

	seen := map[string]bool{"local": true}
	for i, host := range raw.Hosts {
		parsed, err := parseRemoteHost(host, i)
		if err != nil {
			return LoadResult{}, err
		}
		key := strings.ToLower(parsed.Label)
		if seen[key] {
			return LoadResult{}, fmt.Errorf("`hosts[%d].label` must be unique and cannot be `local`", i)
		}
		seen[key] = true
		cfg.RemoteHosts = append(cfg.RemoteHosts, parsed)
	}

	return LoadResult{Config: cfg, Warnings: warnings}, nil
}

func defaultDashboardConfig() DashboardConfig {
	return DashboardConfig{
		LocalHost: LocalHostConfig{
			Label:       "local",
			TmuxEnabled: true,
		},
		TerminalLauncherDefault: true,
	}
}

func parseLocalRepo(repo rawRepo, location string) (LocalRepoConfig, error) {
	root, label, ghAccount, err := parseRepoFields(repo, location)
	if err != nil {
		return LocalRepoConfig{}, err
	}
	root = expandHome(root)
	return LocalRepoConfig{Root: root, Label: label, GHAccount: ghAccount}, nil
}

func parseRemoteRepo(repo rawRepo, location string) (RemoteRepoConfig, error) {
	root, label, ghAccount, err := parseRepoFields(repo, location)
	if err != nil {
		return RemoteRepoConfig{}, err
	}
	return RemoteRepoConfig{Root: root, Label: label, GHAccount: ghAccount}, nil
}

func parseCommand(command rawCommand, location string) (CommandConfig, error) {
	id, err := requiredString(command.ID, location+".id")
	if err != nil {
		return CommandConfig{}, err
	}
	label, err := requiredString(command.Label, location+".label")
	if err != nil {
		return CommandConfig{}, err
	}
	shellCommand, err := requiredString(command.Command, location+".command")
	if err != nil {
		return CommandConfig{}, err
	}
	run := optionalString(command.Run, "shell")
	if !oneOf(run, "shell", "inline", "background", "terminal") {
		return CommandConfig{}, fmt.Errorf("`%s.run` must be one of shell, terminal, inline, or background", location)
	}
	scope := optionalString(command.Scope, "both")
	if !oneOf(scope, "both", "local", "remote") {
		return CommandConfig{}, fmt.Errorf("`%s.scope` must be one of both, local, or remote", location)
	}
	relaunch := optionalString(command.Relaunch, "never")
	if !oneOf(relaunch, "never", "always") {
		return CommandConfig{}, fmt.Errorf("`%s.relaunch` must be one of never or always", location)
	}
	shortcut, err := parseShortcut(command.Shortcut, location+".shortcut")
	if err != nil {
		return CommandConfig{}, err
	}
	contexts, err := parseStringList(command.Contexts, location+".contexts", validContexts())
	if err != nil {
		return CommandConfig{}, err
	}
	requires, err := parseStringList(command.Requires, location+".requires", nil)
	if err != nil {
		return CommandConfig{}, err
	}
	return CommandConfig{
		ID: id, Shortcut: shortcut, Label: label, Detail: optionalString(command.Detail, ""),
		Command: shellCommand, Cwd: optionalString(command.Cwd, ""), Run: run, Scope: scope,
		Contexts: contexts, Requires: requires, Remote: optionalBool(command.Remote, false),
		RemoteInteractive: optionalBool(command.RemoteInteractive, false), Relaunch: relaunch,
	}, nil
}

func requiredString(value *string, name string) (string, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return "", fmt.Errorf("`%s` must be a non-empty string", name)
	}
	return strings.TrimSpace(*value), nil
}

func optionalString(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return strings.TrimSpace(*value)
}

func optionalBool(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func parseShortcut(value *string, name string) (rune, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return 0, nil
	}
	runes := []rune(strings.TrimSpace(*value))
	if len(runes) != 1 {
		return 0, fmt.Errorf("`%s` must be a single character when set", name)
	}
	return runes[0], nil
}

func parseStringList(values []string, name string, allowed map[string]bool) ([]string, error) {
	parsed := []string{}
	for i, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return nil, fmt.Errorf("`%s[%d]` must be a non-empty string", name, i)
		}
		if allowed != nil && !allowed[trimmed] {
			return nil, fmt.Errorf("`%s[%d]` must be one of pr, worktree, branch, or tmux", name, i)
		}
		parsed = append(parsed, trimmed)
	}
	return parsed, nil
}

func validContexts() map[string]bool {
	return map[string]bool{"pr": true, "worktree": true, "branch": true, "tmux": true}
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func parseRepoFields(repo rawRepo, location string) (string, string, string, error) {
	if repo.Root == nil {
		return "", "", "", fmt.Errorf("`%s.root` must be a non-empty string", location)
	}
	root := strings.TrimSpace(*repo.Root)
	if root == "" {
		return "", "", "", fmt.Errorf("`%s.root` must be a non-empty string", location)
	}
	label := ""
	if repo.Label != nil {
		label = strings.TrimSpace(*repo.Label)
	}
	if label == "" {
		label = filepath.Base(root)
	}
	ghAccount := ""
	if repo.GHAccount != nil {
		ghAccount = strings.TrimSpace(*repo.GHAccount)
		if ghAccount == "" {
			return "", "", "", fmt.Errorf("`%s.gh_account` must be a non-empty string", location)
		}
	}
	return root, label, ghAccount, nil
}

func parseRemoteHost(host rawRemoteHost, index int) (RemoteHostConfig, error) {
	label := strings.TrimSpace(host.Label)
	if label == "" {
		return RemoteHostConfig{}, fmt.Errorf("`hosts[%d].label` must be a non-empty string", index)
	}
	sshTarget := strings.TrimSpace(host.SSHTarget)
	if sshTarget == "" {
		return RemoteHostConfig{}, fmt.Errorf("`hosts[%d].ssh_target` must be a non-empty string", index)
	}
	if strings.HasPrefix(sshTarget, "-") || strings.ContainsAny(sshTarget, " \t\r\n") {
		return RemoteHostConfig{}, fmt.Errorf("`hosts[%d].ssh_target` must be a single SSH destination argument", index)
	}
	if host.Tmux == nil {
		return RemoteHostConfig{}, fmt.Errorf("`hosts[%d].tmux` must be a boolean", index)
	}
	parsed := RemoteHostConfig{Label: label, SSHTarget: sshTarget, TmuxEnabled: *host.Tmux}
	for i, repo := range host.Repos {
		r, err := parseRemoteRepo(repo, fmt.Sprintf("hosts[%d].repos[%d]", index, i))
		if err != nil {
			return RemoteHostConfig{}, err
		}
		parsed.Repos = append(parsed.Repos, r)
	}
	if !parsed.TmuxEnabled && len(parsed.Repos) == 0 {
		return RemoteHostConfig{}, fmt.Errorf("`hosts[%d]` must enable tmux or configure at least one repo", index)
	}
	return parsed, nil
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
