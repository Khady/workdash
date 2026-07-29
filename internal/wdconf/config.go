package wdconf

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/Khady/workdash/internal/workspace"
)

const (
	WherePane    = "pane"
	WhereCapture = "capture"
	// WhereAttach connects to a session that already exists. It takes no argv.
	WhereAttach = "attach"
	// RunOnLocal runs the command on this machine even when the workspace lives on
	// another one. Opening a pull request in a browser is about the workspace but
	// belongs on the laptop, which `hosts` cannot express: `hosts` says which
	// workspaces an action applies to, not where its command runs.
	RunOnLocal = "local"
)

type Repo struct {
	Root  string `toml:"root"`
	Label string `toml:"label"`
	// GHAccount names the authenticated `gh` account whose token can see this
	// repo's pull requests, for the machines juggling a personal and a work
	// login. Empty means whatever account the local `gh` defaults to.
	GHAccount string `toml:"gh_account"`
}

type Action struct {
	ID    string   `toml:"id"`
	Label string   `toml:"label"`
	Needs []string `toml:"needs"`
	// Hosts limits an action to named hosts. Empty means every host. A GUI editor
	// belongs on "local" only, because it would otherwise be offered for a remote
	// workspace and open a path that does not exist on this machine.
	Hosts []string `toml:"hosts"`
	Where string   `toml:"where"`
	Argv  []string `toml:"argv"`
	Cwd   string   `toml:"cwd"`
	Shell bool     `toml:"shell"`
	// RunOn is empty for "the workspace's host", or RunOnLocal for this machine.
	RunOn string `toml:"run_on"`
	// AttachArgv overrides the host's way of reaching a remote pane, without the
	// ssh target. Set it to mosh for editors; leave it empty for anything that
	// needs agent or port forwarding.
	AttachArgv []string `toml:"attach_argv"`
}

// Host is a machine reachable over ssh whose repos are collected alongside the
// local ones. The local machine is not spelled out in the config; it is implied,
// which is why `local` is not available as a host label.
type Host struct {
	Label     string `toml:"label"`
	SSHTarget string `toml:"ssh_target"`
	// Tmux is a pointer so an omitted key is distinguishable from an explicit
	// false; TmuxEnabled is the resolved value consumers read.
	Tmux        *bool `toml:"tmux"`
	TmuxEnabled bool  `toml:"-"`
	// AttachArgv reaches the host interactively, with SSHTarget appended. It is
	// argv rather than a template so `mosh -p 25300:25399` needs no quoting
	// rules, and it defaults to plain ssh with a tty.
	AttachArgv []string `toml:"attach_argv"`
	Repos      []Repo   `toml:"repos"`
}

// Default pull request cache windows. The dashboard is opened reflexively and
// refreshed deliberately only a few times a day, so the blocking limit is a day:
// rendering from a cache that is hours old beats making every launch wait on the
// network. The soft limit is short enough that a session that stays open keeps
// catching up in the background.
const (
	DefaultPRCacheTTL     = 24 * time.Hour
	DefaultPRRefreshAfter = 15 * time.Minute
)

type Config struct {
	Repos   []Repo   `toml:"repos"`
	Hosts   []Host   `toml:"hosts"`
	Actions []Action `toml:"actions"`
	// PRCacheTTL is the hard limit: pull requests older than this are refetched
	// with the caller waiting. PRRefreshAfter is the soft one: past it the cache
	// still renders and the refetch happens in the background. Both are parsed
	// from the raw strings below so callers get a duration, not a string.
	PRCacheTTL        time.Duration `toml:"-"`
	PRRefreshAfter    time.Duration `toml:"-"`
	RawPRCacheTTL     string        `toml:"pr_cache_ttl"`
	RawPRRefreshAfter string        `toml:"pr_refresh_after"`
}

// GHAccounts lists the distinct `gh_account` values configured anywhere, local
// repos first, in first-seen order. Pull requests are fetched once per account
// rather than once per repo, so this is the fetch plan: repos leaving the key
// empty are covered by whatever account `gh` defaults to and contribute no
// entry.
func (c Config) GHAccounts() []string {
	var accounts []string
	seen := map[string]bool{}
	collect := func(repos []Repo) {
		for _, repo := range repos {
			if repo.GHAccount == "" || seen[repo.GHAccount] {
				continue
			}
			seen[repo.GHAccount] = true
			accounts = append(accounts, repo.GHAccount)
		}
	}
	collect(c.Repos)
	for _, host := range c.Hosts {
		collect(host.Repos)
	}
	return accounts
}

// LocalHost is the label the local machine collects under. It is reserved, so a
// configured host cannot claim it and shadow the local workspaces.
const LocalHost = "local"

func DefaultPath() string {
	if path := os.Getenv("WD_CONFIG"); path != "" {
		return path
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(base, "workdash", "wd.toml")
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if err := resolvePRCache(&cfg); err != nil {
		return Config{}, err
	}
	if err := normalizeRepos(cfg.Repos, "", true); err != nil {
		return Config{}, err
	}
	if err := normalizeHosts(cfg.Hosts); err != nil {
		return Config{}, err
	}
	for i := range cfg.Actions {
		action := &cfg.Actions[i]
		if strings.TrimSpace(action.ID) == "" {
			return Config{}, fmt.Errorf("action %d: `id` is required", i)
		}
		if len(action.Argv) == 0 && action.Where != WhereAttach {
			return Config{}, fmt.Errorf("action %q: `argv` must be non-empty", action.ID)
		}
		if action.Where == WhereAttach && len(action.Argv) > 0 {
			return Config{}, fmt.Errorf("action %q: `attach` takes no `argv`: it connects to an existing session", action.ID)
		}
		if action.RunOn != "" && action.RunOn != RunOnLocal {
			return Config{}, fmt.Errorf("action %q: `run_on` must be %q or omitted", action.ID, RunOnLocal)
		}
		if action.Where == "" {
			action.Where = WherePane
		}
		switch action.Where {
		case WherePane, WhereCapture:
		case WhereAttach:
			// Attaching without a session is meaningless, so the facet is implied
			// rather than something every attach action has to remember.
			if !slices.Contains(action.Needs, workspace.FacetSession) {
				action.Needs = append(action.Needs, workspace.FacetSession)
			}
		default:
			return Config{}, fmt.Errorf("action %q: `where` must be %q, %q or %q",
				action.ID, WherePane, WhereCapture, WhereAttach)
		}
		for _, need := range action.Needs {
			if err := checkFacet(action.ID, need); err != nil {
				return Config{}, err
			}
		}
	}
	return cfg, nil
}

// resolvePRCache turns the raw duration strings into the durations callers read,
// filling in the defaults for the keys the config leaves out.
func resolvePRCache(cfg *Config) error {
	ttl, err := parseDuration("pr_cache_ttl", cfg.RawPRCacheTTL, DefaultPRCacheTTL)
	if err != nil {
		return err
	}
	refresh, err := parseDuration("pr_refresh_after", cfg.RawPRRefreshAfter, DefaultPRRefreshAfter)
	if err != nil {
		return err
	}
	// The soft limit has to be reachable before the hard one, or the background
	// refresh never runs: the cache would already be past its TTL by the time the
	// refresh window opened, and every read would block on the network instead.
	if refresh > ttl {
		return fmt.Errorf("`pr_refresh_after` (%s) must not exceed `pr_cache_ttl` (%s): the background refresh would never run before the blocking refetch", refresh, ttl)
	}
	cfg.PRCacheTTL = ttl
	cfg.PRRefreshAfter = refresh
	return nil
}

// parseDuration reads one optional duration key, returning fallback when it is
// omitted.
func parseDuration(key, raw string, fallback time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("`%s` %q is not a duration: use a form like %q or %q", key, raw, "24h", "15m")
	}
	if value <= 0 {
		return 0, fmt.Errorf("`%s` %q must be a positive duration", key, raw)
	}
	return value, nil
}

// normalizeRepos cleans every root and fills in the labels the CLI names repos
// by, so they have to stay unique and colon-free: `wd run LABEL:BRANCH ACTION`
// splits on the first colon, and two repos sharing a label leave no selector able
// to tell them apart. prefix scopes the errors to the host being read.
//
// expandTilde is off for a remote host's repos: those roots are interpreted on
// that host, so resolving a leading `~` against the local home would name a
// directory on the wrong machine.
func normalizeRepos(repos []Repo, prefix string, expandTilde bool) error {
	rootByLabel := map[string]string{}
	for i := range repos {
		repo := &repos[i]
		root := strings.TrimSpace(repo.Root)
		if root == "" {
			return fmt.Errorf("%srepo %d: `root` is required", prefix, i)
		}
		if expandTilde {
			root = expandHome(root)
		}
		repo.Root = filepath.Clean(root)
		if repo.Label == "" {
			repo.Label = filepath.Base(repo.Root)
		}
		if strings.Contains(repo.Label, ":") {
			return fmt.Errorf("%srepo %q: `label` %q must not contain %q", prefix, repo.Root, repo.Label, ":")
		}
		repo.GHAccount = strings.TrimSpace(repo.GHAccount)
		if strings.ContainsAny(repo.GHAccount, " \t\n\r") {
			return fmt.Errorf("%srepo %q: `gh_account` %q must not contain whitespace", prefix, repo.Label, repo.GHAccount)
		}
		if previous, ok := rootByLabel[repo.Label]; ok {
			return fmt.Errorf("%sduplicate repo `label` %q used by %q and %q: set a distinct `label` on one of them", prefix, repo.Label, previous, repo.Root)
		}
		rootByLabel[repo.Label] = repo.Root
	}
	return nil
}

// normalizeHosts validates the remote hosts. A host `label` prefixes selectors
// the same way a repo label does, so it carries the same uniqueness and
// colon-free constraints, plus the reservation of `local`.
func normalizeHosts(hosts []Host) error {
	seen := map[string]bool{LocalHost: true}
	for i := range hosts {
		host := &hosts[i]
		host.Label = strings.TrimSpace(host.Label)
		if host.Label == "" {
			return fmt.Errorf("host %d: `label` is required", i)
		}
		if strings.Contains(host.Label, ":") {
			return fmt.Errorf("host %q: `label` must not contain %q", host.Label, ":")
		}
		if host.Label == LocalHost {
			return fmt.Errorf("host %d: `label` %q is reserved for the local machine", i, LocalHost)
		}
		if seen[host.Label] {
			return fmt.Errorf("duplicate host `label` %q: set a distinct `label` on one of them", host.Label)
		}
		seen[host.Label] = true
		host.TmuxEnabled = host.Tmux == nil || *host.Tmux
		host.SSHTarget = strings.TrimSpace(host.SSHTarget)
		if host.SSHTarget == "" {
			return fmt.Errorf("host %q: `ssh_target` is required", host.Label)
		}
		if strings.ContainsAny(host.SSHTarget, " \t\n\r") {
			return fmt.Errorf("host %q: `ssh_target` %q must not contain whitespace", host.Label, host.SSHTarget)
		}
		if len(host.AttachArgv) == 0 {
			// -A forwards the agent and -t allocates a tty. The ssh target is not
			// appended here: the executor adds it, so a host default and an
			// action override have the same shape.
			host.AttachArgv = []string{"ssh", "-A", "-t"}
		}
		if err := normalizeRepos(host.Repos, fmt.Sprintf("host %q: ", host.Label), false); err != nil {
			return err
		}
	}
	return nil
}

// expandHome resolves a leading `~`, matching the convention the legacy loader
// established. Storing the expanded path means every later chdir and every repo
// identity comparison works on a real absolute path.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// knownFacets is the closed set a `needs` entry may name. Validating against it
// at load time turns a typo into a startup error instead of an action that is
// silently invisible on every workspace, since Workspace.Has reports false for
// any name it does not recognise.
var knownFacets = []string{workspace.FacetWorktree, workspace.FacetSession, workspace.FacetPR}

// checkFacet accepts a facet name, optionally prefixed with `!` to negate it:
// `!worktree` matches workspaces that have no worktree.
func checkFacet(actionID, need string) error {
	facet := strings.TrimPrefix(need, "!")
	if facet == "" {
		return fmt.Errorf("action %q: empty facet in `needs`", actionID)
	}
	for _, known := range knownFacets {
		if facet == known {
			return nil
		}
	}
	return fmt.Errorf("action %q: unknown facet %q in `needs`, want one of %s (optionally prefixed with `!`)", actionID, facet, strings.Join(knownFacets, ", "))
}
