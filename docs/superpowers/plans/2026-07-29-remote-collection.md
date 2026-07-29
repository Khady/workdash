# Remote Collection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `wd` collect workspaces from remote hosts in one SSH round trip per host, with nothing installed on the remote side.

**Architecture:** Collection becomes declarative. A `Plan` lists the commands a host needs (`Section{Key, Cwd, Argv}`); a `Transport` executes a whole plan and returns each section's output. `Local` runs sections directly through the existing `Runner`; `SSH` renders the plan into one shell script, pipes it through `ssh host bash -s`, and splits the delimited output. The existing porcelain and `list-sessions` parsers are unchanged — only the transport differs, so the parsers stay the single place output is interpreted.

**Measured justification:** for asia's 11 repos, one ssh exec per git command took 2.72s with a warm `ControlMaster`; one batched ssh took 0.086s. The remote side needs only bash, git and tmux, all of which are already required today.

**Tech Stack:** Go 1.26, stdlib only. No remote binary, no daemon, no new dependencies.

**Non-goals:** remote *actions* (a follow-up slice), PR collection, the daemon, push freshness, the TUI. This slice ends when `wd ls` shows asia's workspaces alongside local ones.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/shellquote/shellquote.go` | POSIX single-quote quoting, extracted from `internal/launch` so both the SSH transport and the executor share one implementation |
| `internal/collect/plan.go` | `Section`, `Plan`, and `PlanFor(host)` — what a host needs fetched |
| `internal/collect/transport.go` | `Transport` interface, `Local`, and `SSH` (script rendering + delimited output splitting) |
| `internal/collect/host.go` | `Host` config struct and `CollectHost(transport, host)` producing records |
| `internal/wdconf/config.go` | add `[[hosts]]` with `label`, `ssh_target`, `tmux`, and `[[hosts.repos]]` |
| `cmd/wd/main.go` | collect every host concurrently, warn per host, keep the rest of the list |
| `cmd/wd/selector.go` | host-qualified selectors: `host:repo:branch`, shortest unambiguous form wins |

---

### Task 1: Extract shellquote

**Files:**
- Create: `internal/shellquote/shellquote.go`, `internal/shellquote/shellquote_test.go`
- Modify: `internal/launch/launch.go` (drop local `shellQuote`, call `shellquote.Quote`)

- [ ] **Step 1: Write the failing test** — move the existing round-trip property test over, since it is the strongest one in the tree.

```go
package shellquote

import (
	"os/exec"
	"testing"
)

func TestQuoteRoundTripsThroughBash(t *testing.T) {
	inputs := []string{
		"plain", "two words", "it's", "", "$HOME", "${HOME}", "$(id -u)",
		"`date`", `back\slash`, `a"b`, "*", "a\tb", "a\nb", ";", "|&", "#hash", "a'b'c",
	}
	for _, in := range inputs {
		out, err := exec.Command("bash", "-c", "printf %s "+Quote(in)).Output()
		if err != nil {
			t.Fatalf("Quote(%q) produced an unrunnable command: %v", in, err)
		}
		if string(out) != in {
			t.Fatalf("Quote(%q) round-tripped to %q", in, string(out))
		}
	}
}

func TestQuoteJoinsArgv(t *testing.T) {
	if got := Join([]string{"git", "-C", "/wt/my repo", "status"}); got != `git -C '/wt/my repo' status` {
		t.Fatalf("Join() = %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/shellquote/`
Expected: FAIL — `undefined: Quote`

- [ ] **Step 3: Write minimal implementation**

```go
// Package shellquote turns argv into a string a POSIX shell will split back into
// the same argv. It exists because two places genuinely need it — the SSH
// transport, which must send one command string over the wire, and the executor's
// `shell: true` path — and one implementation is easier to trust than two.
package shellquote

import "strings"

func Quote(text string) string {
	if text != "" && !strings.ContainsAny(text, " \t\n'\"\\$`*?[]|&;<>(){}#~=!") {
		return text
	}
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}

func Join(argv []string) string {
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		quoted = append(quoted, Quote(arg))
	}
	return strings.Join(quoted, " ")
}
```

- [ ] **Step 4: Run test to verify it passes, and rewire launch**

Run: `go test ./internal/shellquote/ ./internal/launch/`
In `internal/launch/launch.go`, delete `shellQuote` and call `shellquote.Quote`. Keep `internal/launch/launch_test.go`'s golden cases by having them call `shellquote.Quote` too, or delete the duplicated quoting tests there — the property test now lives in one place.
Expected: PASS for both packages.

- [ ] **Step 5: Commit**

```bash
git add internal/shellquote internal/launch
git commit -m "Extract shellquote so the SSH transport and executor share one implementation"
```

---

### Task 2: Collection plan

**Files:**
- Create: `internal/collect/plan.go`, `internal/collect/plan_test.go`

A `Section` is one command plus the key its output is filed under. `PlanFor` builds the whole list for a host: three git commands per repo plus one tmux command when enabled.

- [ ] **Step 1: Write the failing test**

```go
package collect

import "testing"

func TestPlanForCoversEveryRepoAndTmux(t *testing.T) {
	host := Host{Label: "asia", TmuxEnabled: true, Repos: []Repo{
		{Root: "/srv/a", Label: "a"}, {Root: "/srv/b", Label: "b"},
	}}
	plan := PlanFor(host)
	if len(plan.Sections) != 5 {
		t.Fatalf("sections = %d %+v, want 2 repos x 2 git commands + 1 tmux", len(plan.Sections), plan.Sections)
	}
	keys := map[string]bool{}
	for _, section := range plan.Sections {
		if keys[section.Key] {
			t.Fatalf("duplicate section key %q", section.Key)
		}
		keys[section.Key] = true
	}
	if !keys["tmux"] {
		t.Fatal("plan should include a tmux section when tmux is enabled")
	}
}

func TestPlanForSkipsTmuxWhenDisabled(t *testing.T) {
	plan := PlanFor(Host{Label: "asia", Repos: []Repo{{Root: "/srv/a", Label: "a"}}})
	for _, section := range plan.Sections {
		if section.Key == "tmux" {
			t.Fatal("tmux section should be absent when tmux is disabled")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/collect/`
Expected: FAIL — `undefined: Host`, `undefined: PlanFor`

- [ ] **Step 3: Write minimal implementation**

```go
package collect

import "fmt"

// Host is one machine to collect from. An empty SSHTarget means the local
// machine, so local and remote differ only in transport.
type Host struct {
	Label       string
	SSHTarget   string
	Repos       []Repo
	TmuxEnabled bool
}

// Section is one command whose output is filed under Key.
type Section struct {
	Key  string
	Cwd  string
	Argv []string
}

type Plan struct {
	Host     Host
	Sections []Section
}

func PlanFor(host Host) Plan {
	sections := []Section{}
	for i, repo := range host.Repos {
		sections = append(sections,
			Section{Key: fmt.Sprintf("worktrees/%d", i), Cwd: repo.Root, Argv: worktreeArgv},
			Section{Key: fmt.Sprintf("branches/%d", i), Cwd: repo.Root, Argv: branchArgv},
		)
	}
	if host.TmuxEnabled {
		sections = append(sections, Section{Key: "tmux", Argv: tmuxArgv})
	}
	return Plan{Host: host, Sections: sections}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/collect/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/collect
git commit -m "Describe host collection as a plan of sections"
```

---

### Task 3: SSH transport

**Files:**
- Create: `internal/collect/transport.go`, `internal/collect/transport_test.go`

The script prints a delimiter line before each section, then runs the command. Output is split on those delimiters. A section whose command fails contributes its stderr to that section's error rather than aborting the host, so one broken repo does not hide the other ten.

- [ ] **Step 1: Write the failing test**

```go
package collect

import (
	"strings"
	"testing"
)

func TestSSHScriptRunsEverySectionInOneCommand(t *testing.T) {
	plan := PlanFor(Host{Label: "asia", SSHTarget: "me@asia", TmuxEnabled: true,
		Repos: []Repo{{Root: "/srv/a", Label: "a"}}})
	script := sshScript(plan)
	if strings.Count(script, sectionMarker) != len(plan.Sections) {
		t.Fatalf("script has %d markers, want %d:\n%s", strings.Count(script, sectionMarker), len(plan.Sections), script)
	}
	if !strings.Contains(script, "'/srv/a'") && !strings.Contains(script, "/srv/a") {
		t.Fatalf("script does not cd into the repo:\n%s", script)
	}
	if !strings.Contains(script, "worktree list --porcelain") {
		t.Fatalf("script is missing the worktree command:\n%s", script)
	}
}

func TestSplitSectionsFilesOutputUnderKeys(t *testing.T) {
	plan := PlanFor(Host{Label: "asia", Repos: []Repo{{Root: "/srv/a", Label: "a"}}})
	out := sectionMarker + " worktrees/0\nworktree /srv/a\nbranch refs/heads/main\n" +
		sectionMarker + " branches/0\nmain\ntopic\n"
	got := splitSections(out)
	if got["worktrees/0"] != "worktree /srv/a\nbranch refs/heads/main" {
		t.Fatalf("worktrees section = %q", got["worktrees/0"])
	}
	if got["branches/0"] != "main\ntopic" {
		t.Fatalf("branches section = %q", got["branches/0"])
	}
	_ = plan
}

func TestSplitSectionsIgnoresPreamble(t *testing.T) {
	out := "motd noise from the remote shell\n" + sectionMarker + " tmux\nname\t/path\t1\t0\n"
	got := splitSections(out)
	if got["tmux"] != "name\t/path\t1\t0" {
		t.Fatalf("tmux section = %q, want the preamble discarded", got["tmux"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/collect/`
Expected: FAIL — `undefined: sshScript`, `undefined: sectionMarker`, `undefined: splitSections`

- [ ] **Step 3: Write minimal implementation**

```go
package collect

import (
	"fmt"
	"strings"

	"github.com/Khady/workdash/internal/shellquote"
)

// sectionMarker prefixes each section's output. It is deliberately unlikely to
// appear in git or tmux output.
const sectionMarker = "___WD_SECTION___"

// Transport runs a whole plan and returns each section's output by key.
type Transport interface {
	Run(plan Plan) (map[string]string, error)
}

// Local runs each section directly. Sections are independent, so a failing one
// yields an empty string rather than aborting the host.
type Local struct{ Runner Runner }

func (l Local) Run(plan Plan) (map[string]string, error) {
	out := map[string]string{}
	for _, section := range plan.Sections {
		text, err := l.Runner.Run(section.Cwd, section.Argv)
		if err != nil && !noServer(err) {
			return nil, err
		}
		out[section.Key] = text
	}
	return out, nil
}

// SSH runs the entire plan in one remote shell. This is the whole point: for a
// host with a dozen repos, one round trip replaces three dozen.
type SSH struct {
	Runner Runner
	Target string
}

func (s SSH) Run(plan Plan) (map[string]string, error) {
	text, err := s.Runner.Run("", []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5",
		s.Target, "bash", "-s"}, sshScript(plan))
	if err != nil {
		return nil, err
	}
	return splitSections(text), nil
}

// sshScript emits `cd` + command per section, each preceded by its marker. A
// failing section prints nothing and does not stop the script, so one broken
// repo cannot hide the rest of the host.
func sshScript(plan Plan) string {
	var script strings.Builder
	for _, section := range plan.Sections {
		fmt.Fprintf(&script, "printf '%%s %%s\\n' %s %s\n",
			shellquote.Quote(sectionMarker), shellquote.Quote(section.Key))
		if section.Cwd != "" {
			fmt.Fprintf(&script, "cd -- %s 2>/dev/null && ", shellquote.Quote(section.Cwd))
		}
		fmt.Fprintf(&script, "%s 2>/dev/null\n", shellquote.Join(section.Argv))
	}
	return script.String()
}

func splitSections(text string) map[string]string {
	out := map[string]string{}
	key := ""
	lines := []string{}
	flush := func() {
		if key != "" {
			out[key] = strings.Trim(strings.Join(lines, "\n"), "\n")
		}
		lines = nil
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, sectionMarker+" ") {
			flush()
			key = strings.TrimPrefix(line, sectionMarker+" ")
			continue
		}
		if key != "" {
			lines = append(lines, line)
		}
	}
	flush()
	return out
}
```

Note: `Runner.Run` gains an optional trailing stdin argument — change the interface to
`Run(cwd string, argv []string, stdin ...string) (string, error)` and have `LocalRunner`
write the first stdin value to the command's stdin. Update the existing `Runner`
implementations and fakes accordingly; `launch.Runner` stays a separate two-argument
interface, so pass `collect.LocalRunner{}` to it unchanged only if the signatures still
match — if they do not, widen `launch.Runner` the same way.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/collect/ ./internal/launch/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/collect internal/launch
git commit -m "Collect a whole host in one SSH round trip"
```

---

### Task 4: Collect a host into records

**Files:**
- Create: `internal/collect/host.go`, `internal/collect/host_test.go`

- [ ] **Step 1: Write the failing test**

```go
package collect

import (
	"testing"

	"github.com/Khady/workdash/internal/workspace"
)

type fakeTransport struct{ out map[string]string }

func (f fakeTransport) Run(Plan) (map[string]string, error) { return f.out, nil }

func TestCollectHostBuildsRecordsFromSections(t *testing.T) {
	host := Host{Label: "asia", SSHTarget: "me@asia", TmuxEnabled: true,
		Repos: []Repo{{Root: "/srv/a", Label: "a"}}}
	transport := fakeTransport{out: map[string]string{
		"worktrees/0": "worktree /srv/a\nHEAD abc\nbranch refs/heads/main\n",
		"branches/0":  "main\ntopic\n",
		"tmux":        "work session\t/srv/a\t3\t1\n",
	}}
	git, sessions, err := CollectHost(transport, host)
	if err != nil {
		t.Fatalf("CollectHost() error = %v", err)
	}
	snap := workspace.Index(git, sessions)
	if len(snap.Workspaces) != 2 {
		t.Fatalf("workspaces = %d %+v, want main and topic", len(snap.Workspaces), snap.Workspaces)
	}
	if snap.Workspaces[0].ID.Host != "asia" {
		t.Fatalf("host = %q, want asia", snap.Workspaces[0].ID.Host)
	}
	if len(snap.Workspaces) > 0 && len(snap.Orphans) != 0 {
		t.Fatalf("orphans = %+v, want the session attached to /srv/a", snap.Orphans)
	}
}

func TestCollectHostHandlesSessionNamesWithSpaces(t *testing.T) {
	host := Host{Label: "asia", TmuxEnabled: true, Repos: []Repo{{Root: "/srv/a", Label: "a"}}}
	transport := fakeTransport{out: map[string]string{
		"worktrees/0": "worktree /srv/a\nHEAD abc\nbranch refs/heads/main\n",
		"branches/0":  "main\n",
		"tmux":        "agents monorepo develop ocaml\t/srv/a\t4\t0\n",
	}}
	_, sessions, err := CollectHost(transport, host)
	if err != nil {
		t.Fatalf("CollectHost() error = %v", err)
	}
	if len(sessions) != 1 || sessions[0].Session.Name != "agents monorepo develop ocaml" {
		t.Fatalf("sessions = %+v, want the spaced name intact", sessions)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/collect/`
Expected: FAIL — `undefined: CollectHost`

- [ ] **Step 3: Write minimal implementation**

```go
package collect

import (
	"fmt"

	"github.com/Khady/workdash/internal/workspace"
)

// CollectHost runs a host's plan and parses each section with the same parsers
// the local path uses, so the transport is the only thing that differs.
func CollectHost(transport Transport, host Host) ([]workspace.GitRecord, []workspace.SessionRecord, error) {
	plan := PlanFor(host)
	sections, err := transport.Run(plan)
	if err != nil {
		return nil, nil, err
	}
	git := []workspace.GitRecord{}
	for i, repo := range host.Repos {
		git = append(git, parseWorktrees(sections[fmt.Sprintf("worktrees/%d", i)], host.Label, repo)...)
		git = append(git, parseBranches(sections[fmt.Sprintf("branches/%d", i)], host.Label, repo)...)
	}
	return git, parseSessions(sections["tmux"], host.Label), nil
}
```

Refactor `Git()` and `Tmux()` so their parsing bodies become `parseBranches` and
`parseSessions` alongside the existing `parseWorktrees`, and keep the old
functions as thin wrappers so their tests keep passing.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/collect/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/collect
git commit -m "Collect a host's records through a transport"
```

---

### Task 5: Hosts in config

**Files:**
- Modify: `internal/wdconf/config.go`, `internal/wdconf/config_test.go`

```toml
[[hosts]]
label = "asia"
ssh_target = "me@asia"
tmux = true

[[hosts.repos]]
root = "/home/me/Code/ahrefs/monorepo"
```

- [ ] **Step 1: Write the failing test**

```go
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
	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want a rejection of the reserved label `local`")
	}
}

func TestLoadRejectsHostWithoutSSHTarget(t *testing.T) {
	path := writeConfig(t, "[[hosts]]\nlabel = \"asia\"\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error about `ssh_target`")
	}
}
```

Add a `writeConfig(t *testing.T, body string) string` helper if one does not exist,
and reuse it in the existing tests.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/wdconf/`
Expected: FAIL — `cfg.Hosts` undefined

- [ ] **Step 3: Write minimal implementation**

Add to `config.go`:

```go
type Host struct {
	Label       string `toml:"label"`
	SSHTarget   string `toml:"ssh_target"`
	TmuxEnabled bool   `toml:"tmux"`
	Repos       []Repo `toml:"repos"`
}
```

Add `Hosts []Host \`toml:"hosts"\`` to `Config`. Validate: `label` non-empty, not
`"local"`, unique across hosts, no `:`; `ssh_target` non-empty and whitespace-free;
each host repo root normalized the same way local roots are, except `~` is NOT
expanded (it would expand to the *local* home); host repo labels default from the
root's base name and must be unique within that host.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/wdconf/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/wdconf
git commit -m "Configure remote hosts"
```

---

### Task 6: Host-qualified selectors

**Files:**
- Modify: `cmd/wd/selector.go`, `cmd/wd/selector_test.go`

With two hosts, the same repo cloned on both makes `repo:branch` ambiguous. A
selector becomes `[host:][repo:]branch`, and the ambiguity error must list
host-qualified candidates so one of them is actually typeable.

- [ ] **Step 1: Write the failing test**

```go
func TestSelectDisambiguatesByHost(t *testing.T) {
	snap := workspace.Snapshot{Workspaces: []workspace.Workspace{
		{ID: workspace.ID{Host: "local", Repo: "/a", Branch: "topic"}, RepoLabel: "mono"},
		{ID: workspace.ID{Host: "asia", Repo: "/srv/a", Branch: "topic"}, RepoLabel: "mono"},
	}}
	got, err := Select(snap, "asia:mono:topic")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.ID.Host != "asia" {
		t.Fatalf("selected host = %q, want asia", got.ID.Host)
	}
	_, err = Select(snap, "topic")
	if err == nil {
		t.Fatal("Select() error = nil, want ambiguity across hosts")
	}
	if !strings.Contains(err.Error(), "asia:mono:topic") || !strings.Contains(err.Error(), "local:mono:topic") {
		t.Fatalf("error = %q, want typeable host-qualified candidates", err.Error())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/wd/`
Expected: FAIL — `Select()` does not understand a three-part selector

- [ ] **Step 3: Write minimal implementation**

Split the selector on `:` into 1, 2, or 3 parts (branch; repo:branch; host:repo:branch),
match on whichever parts are present, and render ambiguity candidates as
`host:repo:branch`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/wd/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/wd
git commit -m "Select workspaces across hosts"
```

---

### Task 7: Wire hosts into the CLI

**Files:**
- Modify: `cmd/wd/main.go`, `cmd/wd/main_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestHostsFromConfigIncludesLocalAndRemotes(t *testing.T) {
	cfg := wdconf.Config{
		Repos: []wdconf.Repo{{Root: "/local/a", Label: "a"}},
		Hosts: []wdconf.Host{{Label: "asia", SSHTarget: "me@asia", TmuxEnabled: true,
			Repos: []wdconf.Repo{{Root: "/srv/b", Label: "b"}}}},
	}
	hosts := hostsFromConfig(cfg)
	if len(hosts) != 2 {
		t.Fatalf("hosts = %+v, want local and asia", hosts)
	}
	if hosts[0].Label != "local" || hosts[0].SSHTarget != "" {
		t.Fatalf("first host = %+v, want local with no ssh target", hosts[0])
	}
	if hosts[1].Label != "asia" || hosts[1].SSHTarget != "me@asia" {
		t.Fatalf("second host = %+v", hosts[1])
	}
}

func TestUnreachableHostWarnsAndKeepsOtherHosts(t *testing.T) {
	// A host whose transport fails must contribute a warning, not erase the
	// hosts that worked: a VPN drop should degrade one section of the list.
	good := []workspace.GitRecord{{ID: workspace.ID{Host: "local", Repo: "/a", Branch: "main"}, RepoLabel: "a"}}
	snap, warnings := mergeHostResults([]hostResult{
		{host: "local", git: good},
		{host: "asia", err: errors.New("ssh: connect to host asia port 22: Network is unreachable")},
	})
	if len(snap.Workspaces) != 1 {
		t.Fatalf("workspaces = %+v, want the local one kept", snap.Workspaces)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "asia") {
		t.Fatalf("warnings = %v, want one naming asia", warnings)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/wd/`
Expected: FAIL — `undefined: hostsFromConfig`, `undefined: mergeHostResults`

- [ ] **Step 3: Write minimal implementation**

`hostsFromConfig` returns `collect.Host` values, local first (label `"local"`, empty
`SSHTarget`, `TmuxEnabled: true`). `loadSnapshot` runs one goroutine per host, each
choosing `collect.Local` or `collect.SSH` by `SSHTarget`, collects `hostResult{host,
git, sessions, err}`, and `mergeHostResults` merges the successes through
`workspace.Index` while turning each error into one `"<host>: <err>"` warning.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... && go build ./...`
Expected: PASS, build clean

- [ ] **Step 5: Commit**

```bash
git add cmd/wd
git commit -m "Collect every host concurrently and degrade per host"
```

---

### Task 8: Verify against the real remote host

- [ ] **Step 1: Point a config at asia and list it**

```bash
WD_CONFIG=/path/to/asia.toml wd ls
```

Expected: asia's repos listed with `worktree` facets, and its live tmux sessions
attached to whichever worktrees they sit in — including sessions whose names
contain spaces.

- [ ] **Step 2: Time a cold refresh**

```bash
time WD_CONFIG=/path/to/asia.toml wd ls >/dev/null
```

Expected: comfortably under a second for eleven repos, matching the 0.086s
measured for the batched shell equivalent. If it is not, the transport is issuing
more than one ssh per host — check with `ssh -v` or by counting processes.

- [ ] **Step 3: Verify graceful degradation**

```bash
WD_CONFIG=/path/to/asia.toml ssh_target=nonexistent wd ls
```

Expected: a warning naming the unreachable host, local workspaces still listed,
exit 0.

---

## Self-Review

**Spec coverage:** one round trip per host (Tasks 2–4), nothing installed remotely (the transport is a rendered shell script, Task 3), hosts in config (Task 5), selection across hosts (Task 6), concurrent collection with per-host degradation (Task 7), real-host verification including the measured target (Task 8). Remote *actions*, PRs, the daemon and the TUI remain out of scope.

**Placeholder scan:** two steps describe a refactor in prose rather than showing final code — Task 3 Step 3's `Runner` stdin widening, and Task 4 Step 3's extraction of `parseBranches`/`parseSessions`. Both name the exact change and the exact files; resolve them while implementing rather than inventing a different shape.

**Type consistency:** `collect.Host`/`Section`/`Plan` are defined in Task 2 and used in Tasks 3, 4, 7. `Transport` is defined in Task 3 and consumed in Task 4 and Task 7. `wdconf.Host` is defined in Task 5 and mapped to `collect.Host` in Task 7. `shellquote.Quote`/`Join` are defined in Task 1 and used in Task 3. `hostResult` is introduced in Task 7 only.
