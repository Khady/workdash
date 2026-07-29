package wdconf

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// writeConfig drops body into a throwaway wd.toml and hands back its path, so a
// test reads as the config it is about rather than as tempdir bookkeeping.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wd.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadReadsRemoteHosts(t *testing.T) {
	path := writeConfig(t, `
[[hosts]]
label = "asia"
ssh_target = "me@asia"
tmux = true

[[hosts.repos]]
root = "/home/me/Code/monorepo"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Hosts) != 1 {
		t.Fatalf("hosts = %+v, want 1", cfg.Hosts)
	}
	host := cfg.Hosts[0]
	if host.Label != "asia" || host.SSHTarget != "me@asia" || !host.TmuxEnabled {
		t.Fatalf("host = %+v", host)
	}
	if len(host.Repos) != 1 || host.Repos[0].Label != "monorepo" {
		t.Fatalf("host repos = %+v", host.Repos)
	}
}

func TestLoadRejectsHostLabelCollidingWithLocal(t *testing.T) {
	path := writeConfig(t, "[[hosts]]\nlabel = \"local\"\nssh_target = \"me@asia\"\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want a rejection of the reserved label `local`")
	}
	// The config never spells out the local machine, so "duplicate" would send the
	// reader hunting for a second host that does not exist: say it is reserved.
	if !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("Load() error = %v, want it to say the label is reserved", err)
	}
}

func TestLoadRejectsHostWithoutSSHTarget(t *testing.T) {
	path := writeConfig(t, "[[hosts]]\nlabel = \"asia\"\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error about `ssh_target`")
	}
}

func TestLoadRejectsHostWithoutLabel(t *testing.T) {
	path := writeConfig(t, "[[hosts]]\nssh_target = \"me@asia\"\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error about `label`")
	}
}

func TestLoadRejectsDuplicateHostLabels(t *testing.T) {
	path := writeConfig(t, `
[[hosts]]
label = "asia"
ssh_target = "me@asia-1"

[[hosts]]
label = "asia"
ssh_target = "me@asia-2"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error about the duplicate host label")
	}
	if !strings.Contains(err.Error(), "asia") {
		t.Fatalf("Load() error = %v, want it to name the offending label", err)
	}
}

func TestLoadRejectsHostLabelContainingColon(t *testing.T) {
	// A host label prefixes a selector, and `wd run HOST:REPO:BRANCH` splits on
	// colons, so a colon inside the label leaves the host unaddressable.
	path := writeConfig(t, "[[hosts]]\nlabel = \"asia:1\"\nssh_target = \"me@asia\"\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error about the colon in `label`")
	}
	if !strings.Contains(err.Error(), "asia:1") {
		t.Fatalf("Load() error = %v, want it to name the offending label", err)
	}
}

func TestLoadRejectsSSHTargetWithWhitespace(t *testing.T) {
	// The target is interpolated into a rendered ssh command line, so smuggling
	// extra words through it would append arguments nobody configured.
	path := writeConfig(t, "[[hosts]]\nlabel = \"asia\"\nssh_target = \"me@asia -o ProxyCommand=x\"\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error about whitespace in `ssh_target`")
	}
}

func TestLoadKeepsTildeInHostRepoRoot(t *testing.T) {
	// A host root is resolved on the remote machine, so expanding `~` against the
	// local home would point the collector at a path that need not exist there.
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := writeConfig(t, `
[[hosts]]
label = "asia"
ssh_target = "me@asia"

[[hosts.repos]]
root = "~/Code/x"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Hosts) != 1 || len(cfg.Hosts[0].Repos) != 1 {
		t.Fatalf("hosts = %+v, want one host with one repo", cfg.Hosts)
	}
	repo := cfg.Hosts[0].Repos[0]
	if repo.Root != "~/Code/x" {
		t.Fatalf("root = %q, want it preserved verbatim for the remote machine", repo.Root)
	}
	if repo.Label != "x" {
		t.Fatalf("label = %q, want %q", repo.Label, "x")
	}
}

func TestLoadRejectsDuplicateRepoLabelsWithinHost(t *testing.T) {
	path := writeConfig(t, `
[[hosts]]
label = "asia"
ssh_target = "me@asia"

[[hosts.repos]]
root = "/work/a/api"

[[hosts.repos]]
root = "/work/b/api"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error about the duplicate label")
	}
	for _, want := range []string{"asia", "api", "/work/a/api", "/work/b/api"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Load() error = %v, want it to mention %q", err, want)
		}
	}
}

func TestLoadAcceptsTheSameRepoLabelOnDifferentHosts(t *testing.T) {
	// Uniqueness is per host: the same repo cloned on two machines is the normal
	// case, and the host prefix is what disambiguates it.
	path := writeConfig(t, `
[[repos]]
root = "/home/me/api"

[[hosts]]
label = "asia"
ssh_target = "me@asia"

[[hosts.repos]]
root = "/srv/api"

[[hosts]]
label = "eu"
ssh_target = "me@eu"

[[hosts.repos]]
root = "/srv/api"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Hosts) != 2 {
		t.Fatalf("hosts = %+v, want 2", cfg.Hosts)
	}
	for _, host := range cfg.Hosts {
		if len(host.Repos) != 1 || host.Repos[0].Label != "api" {
			t.Fatalf("host %q repos = %+v, want one labelled api", host.Label, host.Repos)
		}
	}
}

func TestLoadReadsReposAndActions(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
root = "/home/me/code/repo"

[[actions]]
id = "agent"
label = "launch agent"
needs = ["worktree"]
where = "pane"
argv = ["claude"]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Repos) != 1 || cfg.Repos[0].Label != "repo" {
		t.Fatalf("repos = %+v, want one labelled repo", cfg.Repos)
	}
	if len(cfg.Actions) != 1 {
		t.Fatalf("actions = %+v, want 1", cfg.Actions)
	}
	action := cfg.Actions[0]
	// An omitted cwd stays empty here: what it means depends on the workspace, so
	// internal/action resolves it (worktree path when there is one, nothing when
	// there is not) rather than the config baking in a requirement.
	if action.ID != "agent" || action.Where != "pane" || action.Cwd != "" {
		t.Fatalf("action = %+v, want an empty cwd", action)
	}
}

func TestLoadReadsRepoGHAccount(t *testing.T) {
	// Surrounding whitespace is trimmed because the value is handed to
	// `gh auth token --user <account>`, where a stray space is not a valid user.
	path := writeConfig(t, `
[[repos]]
root = "/home/me/code/monorepo"
gh_account = "  louis-roche_ahrefs  "
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Repos) != 1 {
		t.Fatalf("repos = %+v, want 1", cfg.Repos)
	}
	if cfg.Repos[0].GHAccount != "louis-roche_ahrefs" {
		t.Fatalf("gh account = %q, want %q", cfg.Repos[0].GHAccount, "louis-roche_ahrefs")
	}
}

// A host repo's pull requests are fetched by the local `gh`, so the account has
// to survive Load on `[[hosts.repos]]` just as it does on a local repo.
func TestLoadReadsHostRepoGHAccount(t *testing.T) {
	path := writeConfig(t, `
[[hosts]]
label = "asia"
ssh_target = "me@asia"

[[hosts.repos]]
root = "/srv/monorepo"
gh_account = "louis-roche_ahrefs"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Hosts) != 1 || len(cfg.Hosts[0].Repos) != 1 {
		t.Fatalf("hosts = %+v, want one host with one repo", cfg.Hosts)
	}
	if got := cfg.Hosts[0].Repos[0].GHAccount; got != "louis-roche_ahrefs" {
		t.Fatalf("gh account = %q, want %q", got, "louis-roche_ahrefs")
	}
}

// An omitted account means "whatever the local `gh` is authenticated as", which
// is the only sensible default for a single-account machine.
func TestLoadAcceptsRepoWithoutGHAccount(t *testing.T) {
	path := writeConfig(t, "[[repos]]\nroot = \"/home/me/code/repo\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Repos[0].GHAccount != "" {
		t.Fatalf("gh account = %q, want it left empty", cfg.Repos[0].GHAccount)
	}
}

// The account is passed as one argument to `gh auth token --user <account>`, so
// an embedded space is never a real account and would surface as an opaque `gh`
// failure long after Load, on whichever repo happens to be refreshed first.
func TestLoadRejectsGHAccountWithWhitespace(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
root = "/home/me/code/monorepo"
label = "mono"
gh_account = "louis roche"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error about whitespace in `gh_account`")
	}
	for _, want := range []string{"mono", "gh_account"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Load() error = %v, want it to mention %q", err, want)
		}
	}
}

func TestLoadRejectsHostRepoGHAccountWithWhitespace(t *testing.T) {
	path := writeConfig(t, `
[[hosts]]
label = "asia"
ssh_target = "me@asia"

[[hosts.repos]]
root = "/srv/monorepo"
gh_account = "louis roche"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error about whitespace in `gh_account`")
	}
	if !strings.Contains(err.Error(), "asia") {
		t.Fatalf("Load() error = %v, want it to name the host", err)
	}
}

// Pull requests are fetched once per distinct account, so the caller wants the
// set, not the per-repo values: duplicates would mean querying the same account
// twice, and an empty entry would mean an extra query for the default account
// that the accountless repos already cover.
func TestGHAccountsReturnsDistinctNonEmptyAccounts(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
root = "/home/me/code/monorepo"
gh_account = "work"

[[repos]]
root = "/home/me/code/tools"
gh_account = "work"

[[repos]]
root = "/home/me/code/dotfiles"

[[hosts]]
label = "asia"
ssh_target = "me@asia"

[[hosts.repos]]
root = "/srv/monorepo"
gh_account = "personal"

[[hosts.repos]]
root = "/srv/tools"
gh_account = "work"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := cfg.GHAccounts()
	if want := []string{"work", "personal"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("GHAccounts() = %q, want %q in first-seen order", got, want)
	}
}

func TestGHAccountsIsEmptyWithoutConfiguredAccounts(t *testing.T) {
	path := writeConfig(t, "[[repos]]\nroot = \"/home/me/code/repo\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := cfg.GHAccounts(); len(got) != 0 {
		t.Fatalf("GHAccounts() = %q, want none", got)
	}
}

func TestLoadRejectsUnknownWhere(t *testing.T) {
	path := writeConfig(t, `
[[actions]]
id = "bad"
label = "bad"
where = "sideways"
argv = ["true"]
`)
	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error about `where`")
	}
}

func TestLoadRejectsEmptyArgv(t *testing.T) {
	path := writeConfig(t, `
[[actions]]
id = "empty"
label = "empty"
where = "pane"
argv = []
`)
	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error about `argv`")
	}
}

func TestLoadExpandsTildeInRepoRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := writeConfig(t, `
[[repos]]
root = "~/Code/workdash"

[[repos]]
root = "~"
label = "home"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Repos) != 2 {
		t.Fatalf("repos = %+v, want 2", cfg.Repos)
	}
	if want := filepath.Join(home, "Code", "workdash"); cfg.Repos[0].Root != want {
		t.Fatalf("root = %q, want %q", cfg.Repos[0].Root, want)
	}
	if cfg.Repos[0].Label != "workdash" {
		t.Fatalf("label = %q, want %q", cfg.Repos[0].Label, "workdash")
	}
	if cfg.Repos[1].Root != home {
		t.Fatalf("root = %q, want %q", cfg.Repos[1].Root, home)
	}
}

func TestLoadNormalizesTrailingSlashInRepoRoot(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
root = "/x/repo/"
label = "slashed"

[[repos]]
root = "/x/./repo"
label = "clean"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Repos) != 2 {
		t.Fatalf("repos = %+v, want 2", cfg.Repos)
	}
	if cfg.Repos[0].Root != "/x/repo" {
		t.Fatalf("root = %q, want %q", cfg.Repos[0].Root, "/x/repo")
	}
	if cfg.Repos[0].Root != cfg.Repos[1].Root {
		t.Fatalf("roots = %q and %q, want one identity for one repo", cfg.Repos[0].Root, cfg.Repos[1].Root)
	}
}

func TestLoadRejectsDuplicateRepoLabels(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
root = "/work/a/api"

[[repos]]
root = "/work/b/api"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error about the duplicate label")
	}
	for _, want := range []string{"api", "/work/a/api", "/work/b/api"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Load() error = %v, want it to mention %q", err, want)
		}
	}
}

func TestLoadAcceptsDistinctExplicitLabelsForSameBase(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
root = "/work/a/api"
label = "a-api"

[[repos]]
root = "/work/b/api"
label = "b-api"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Repos) != 2 {
		t.Fatalf("repos = %+v, want 2", cfg.Repos)
	}
}

func TestLoadRejectsLabelContainingColon(t *testing.T) {
	path := writeConfig(t, `
[[repos]]
root = "/work/api"
label = "api:main"
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error about the colon in `label`")
	}
	if !strings.Contains(err.Error(), "api:main") {
		t.Fatalf("Load() error = %v, want it to name the offending label", err)
	}
}

func TestLoadRejectsUnknownFacetInNeeds(t *testing.T) {
	path := writeConfig(t, `
[[actions]]
id = "typo"
label = "typo"
needs = ["worktee"]
argv = ["true"]
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error about an unknown facet in `needs`")
	}
	if !strings.Contains(err.Error(), "worktee") {
		t.Fatalf("Load() error = %v, want it to name the offending facet", err)
	}
}

func TestLoadRejectsNegatedUnknownFacetInNeeds(t *testing.T) {
	path := writeConfig(t, `
[[actions]]
id = "typo"
label = "typo"
needs = ["!worktee"]
argv = ["true"]
`)
	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error about an unknown facet in `needs`")
	}
}

func TestLoadRejectsEmptyFacetInNeeds(t *testing.T) {
	for _, needs := range []string{`[""]`, `["!"]`} {
		path := writeConfig(t, `
[[actions]]
id = "blank"
label = "blank"
needs = `+needs+`
argv = ["true"]
`)
		if _, err := Load(path); err == nil {
			t.Fatalf("Load() with needs = %s error = nil, want an error about `needs`", needs)
		}
	}
}

func TestLoadAcceptsKnownAndNegatedFacetsInNeeds(t *testing.T) {
	path := writeConfig(t, `
[[actions]]
id = "positive"
label = "positive"
needs = ["worktree", "session", "pr"]
argv = ["true"]

[[actions]]
id = "negative"
label = "negative"
needs = ["!worktree", "!session", "!pr"]
argv = ["true"]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Actions) != 2 {
		t.Fatalf("actions = %+v, want 2", cfg.Actions)
	}
	if got := cfg.Actions[1].Needs; !reflect.DeepEqual(got, []string{"!worktree", "!session", "!pr"}) {
		t.Fatalf("needs = %v, want the negations preserved verbatim", got)
	}
}

func TestLoadDefaultsMissingWhereToPane(t *testing.T) {
	path := writeConfig(t, `
[[actions]]
id = "bare"
label = "bare"
argv = ["true"]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Actions) != 1 {
		t.Fatalf("actions = %+v, want 1", cfg.Actions)
	}
	if cfg.Actions[0].Where != WherePane {
		t.Fatalf("where = %q, want %q", cfg.Actions[0].Where, WherePane)
	}
}

// Omitting `tmux` on a host must not silently hide its sessions: a missing
// session facet looks identical to "the session died", which is the failure mode
// this project keeps having to fix. Collecting tmux from a host that has none is
// harmless, so the default is on.
func TestLoadDefaultsHostTmuxToEnabled(t *testing.T) {
	path := writeConfig(t, "[[hosts]]\nlabel = \"asia\"\nssh_target = \"me@asia\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Hosts[0].TmuxEnabled {
		t.Fatal("host tmux should default to enabled when the key is omitted")
	}
}

func TestLoadHonoursExplicitHostTmuxFalse(t *testing.T) {
	path := writeConfig(t, "[[hosts]]\nlabel = \"asia\"\nssh_target = \"me@asia\"\ntmux = false\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Hosts[0].TmuxEnabled {
		t.Fatal("explicit tmux = false must be honoured")
	}
}

func TestLoadDefaultsHostAttachArgv(t *testing.T) {
	path := writeConfig(t, "[[hosts]]\nlabel = \"asia\"\nssh_target = \"me@asia\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	// The ssh target is appended by the executor, not here, so a host default and
	// an action-level override have the same shape. -A forwards the agent, which
	// mosh cannot do.
	want := []string{"ssh", "-A", "-t"}
	got := cfg.Hosts[0].AttachArgv
	if len(got) != len(want) {
		t.Fatalf("attach argv = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("attach argv = %q, want %q", got, want)
		}
	}
}

// mosh survives a suspended laptop, which is why it is worth configuring for a
// host you keep long-running agents on.
func TestLoadKeepsConfiguredAttachArgvVerbatim(t *testing.T) {
	path := writeConfig(t, `
[[hosts]]
label = "asia"
ssh_target = "me@asia"
attach_argv = ["mosh", "-p", "25300:25399"]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := cfg.Hosts[0].AttachArgv
	want := []string{"mosh", "-p", "25300:25399"}
	if len(got) != len(want) || got[0] != "mosh" {
		t.Fatalf("attach argv = %q, want %q", got, want)
	}
}

// The dashboard is opened reflexively, so a stale-but-recent cache has to be
// good enough to render from: the hard limit is long and the soft one short, and
// only the soft one is crossed in the common case.
func TestLoadDefaultsPRCacheDurations(t *testing.T) {
	path := writeConfig(t, "[[repos]]\nroot = \"/home/me/code/repo\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.PRCacheTTL != 24*time.Hour {
		t.Fatalf("pr cache ttl = %v, want %v", cfg.PRCacheTTL, 24*time.Hour)
	}
	if cfg.PRRefreshAfter != 15*time.Minute {
		t.Fatalf("pr refresh after = %v, want %v", cfg.PRRefreshAfter, 15*time.Minute)
	}
}

func TestLoadReadsPRCacheDurations(t *testing.T) {
	path := writeConfig(t, "pr_cache_ttl = \"2h30m\"\npr_refresh_after = \"90s\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.PRCacheTTL != 2*time.Hour+30*time.Minute {
		t.Fatalf("pr cache ttl = %v, want %v", cfg.PRCacheTTL, 2*time.Hour+30*time.Minute)
	}
	if cfg.PRRefreshAfter != 90*time.Second {
		t.Fatalf("pr refresh after = %v, want %v", cfg.PRRefreshAfter, 90*time.Second)
	}
}

// A bare number is the mistake to expect, since every other duration-shaped knob
// in a config file is usually seconds. It has to fail at load rather than silently
// leave the cache on its default.
func TestLoadRejectsUnparseablePRCacheDuration(t *testing.T) {
	for key, body := range map[string]string{
		"pr_cache_ttl":     "pr_cache_ttl = \"900\"\n",
		"pr_refresh_after": "pr_refresh_after = \"soon\"\n",
	} {
		_, err := Load(writeConfig(t, body))
		if err == nil {
			t.Fatalf("Load() with %s error = nil, want an error about the duration", key)
		}
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("Load() error = %v, want it to name `%s`", err, key)
		}
	}
}

// Zero or negative would mean "always expired", turning every launch into a
// blocking fetch, so it is a misconfiguration rather than a way to disable the
// cache.
func TestLoadRejectsNonPositivePRCacheDuration(t *testing.T) {
	cases := []struct{ key, body string }{
		{"pr_cache_ttl", "pr_cache_ttl = \"0s\"\n"},
		{"pr_cache_ttl", "pr_cache_ttl = \"-1h\"\n"},
		{"pr_refresh_after", "pr_refresh_after = \"0s\"\n"},
		{"pr_refresh_after", "pr_refresh_after = \"-5m\"\n"},
	}
	for _, tc := range cases {
		_, err := Load(writeConfig(t, tc.body))
		if err == nil {
			t.Fatalf("Load() with %s error = nil, want an error about the non-positive duration", tc.body)
		}
		if !strings.Contains(err.Error(), tc.key) {
			t.Fatalf("Load() error = %v, want it to name `%s`", err, tc.key)
		}
	}
}

// A soft limit past the hard one can never fire: the cache would hit the blocking
// refetch first, so the background refresh would be dead config.
func TestLoadRejectsPRRefreshAfterBeyondTTL(t *testing.T) {
	path := writeConfig(t, "pr_cache_ttl = \"10m\"\npr_refresh_after = \"30m\"\n")
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error about `pr_refresh_after` exceeding `pr_cache_ttl`")
	}
	for _, want := range []string{"pr_refresh_after", "pr_cache_ttl"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Load() error = %v, want it to name `%s`", err, want)
		}
	}
}

// Equal limits are legal: it just means there is no window in which the refresh
// happens in the background, not a contradiction.
func TestLoadAcceptsPRRefreshAfterEqualToTTL(t *testing.T) {
	path := writeConfig(t, "pr_cache_ttl = \"30m\"\npr_refresh_after = \"30m\"\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.PRCacheTTL != 30*time.Minute || cfg.PRRefreshAfter != 30*time.Minute {
		t.Fatalf("ttl = %v, refresh after = %v, want both %v", cfg.PRCacheTTL, cfg.PRRefreshAfter, 30*time.Minute)
	}
}
