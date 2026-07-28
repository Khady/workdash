# Workdash Core Rewrite — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild workdash's core as a facet-based workspace index with a two-verb executor and a CLI client, so the dashboard owns no authoritative state and every action is the same command the user would type by hand.

**Architecture:** Collectors read git and tmux through a `Runner` interface and emit facet records. A pure `Index` function merges records into `Workspace` values keyed by `(host, repo, branch)`, where worktree/session/PR are optional facets. Actions are declarative (`argv` + `needs` + `where`), resolved against a workspace's present facets, and executed by exactly two verbs: `Pane` (a tmux window) and `Capture` (run and return output). A CLI client (`wd ls`, `wd run`) is the first frontend; the TUI, daemon, and browser become later clients over the same core.

**Tech Stack:** Go 1.26, `github.com/pelletier/go-toml/v2` (already a dependency), stdlib `testing`. No new dependencies.

**Non-goals for this slice:** remote hosts, GitHub/PR collection, the daemon and its socket protocol, push notifications, the TUI, the browser popup. The PR facet exists in the model but no collector fills it yet.

**Coexistence:** all new packages live beside the existing tree; nothing under `internal/{config,source,actions,ui,daemon,loader,model,search}` is modified, so the current `workdash` binary keeps building. New config file is `~/.config/workdash/wd.toml` (override with `$WD_CONFIG`) so the existing `config.toml` is untouched.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/workspace/workspace.go` | `ID`, facet types (`Worktree`, `Session`, `PR`), `Workspace` and facet predicates |
| `internal/workspace/index.go` | Pure merge: facet records → `Snapshot{Workspaces, Orphans}` |
| `internal/collect/runner.go` | `Runner` interface + `LocalRunner` |
| `internal/collect/git.go` | Parse `git worktree list --porcelain` and `for-each-ref` into records |
| `internal/collect/tmux.go` | Parse `tmux list-sessions -F` into session records |
| `internal/wdconf/config.go` | TOML config: repos + action definitions |
| `internal/action/action.go` | Resolve action definitions against a workspace, render argv |
| `internal/launch/launch.go` | `Executor` interface, tmux implementation, shell quoting |
| `cmd/wd/main.go` | CLI: `wd ls [--json]`, `wd run <selector> <action>` |
| `cmd/wd/selector.go` | Workspace selection by branch or `repo:branch` |

---

### Task 1: Workspace model and facets

**Files:**
- Create: `internal/workspace/workspace.go`
- Test: `internal/workspace/workspace_test.go`

- [ ] **Step 1: Write the failing test**

```go
package workspace

import "testing"

func TestWorkspaceReportsPresentFacets(t *testing.T) {
	ws := Workspace{ID: ID{Host: "local", Repo: "/repo", Branch: "topic"}}
	if ws.Has(FacetWorktree) {
		t.Fatal("bare workspace should not have a worktree facet")
	}
	ws.Worktree = &Worktree{Path: "/repo/topic"}
	if !ws.Has(FacetWorktree) {
		t.Fatal("workspace with worktree should report the worktree facet")
	}
	if got := ws.Facets(); len(got) != 1 || got[0] != FacetWorktree {
		t.Fatalf("Facets() = %v, want [worktree]", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/workspace/`
Expected: FAIL — `undefined: Workspace`

- [ ] **Step 3: Write minimal implementation**

```go
package workspace

import "fmt"

const (
	FacetWorktree = "worktree"
	FacetSession  = "session"
	FacetPR       = "pr"
)

type ID struct {
	Host   string
	Repo   string
	Branch string
}

func (id ID) String() string {
	return fmt.Sprintf("%s:%s@%s", id.Host, id.Repo, id.Branch)
}

type Worktree struct {
	Path   string
	IsMain bool
	Head   string
}

type Session struct {
	Name     string
	Path     string
	Windows  int
	Attached bool
}

type PR struct {
	Number int
	URL    string
	State  string
	Draft  bool
}

type Workspace struct {
	ID        ID
	RepoLabel string
	Worktree  *Worktree
	Session   *Session
	PR        *PR
}

func (w Workspace) Has(facet string) bool {
	switch facet {
	case FacetWorktree:
		return w.Worktree != nil
	case FacetSession:
		return w.Session != nil
	case FacetPR:
		return w.PR != nil
	}
	return false
}

func (w Workspace) Facets() []string {
	facets := []string{}
	for _, facet := range []string{FacetWorktree, FacetSession, FacetPR} {
		if w.Has(facet) {
			facets = append(facets, facet)
		}
	}
	return facets
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/workspace/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/workspace/
git commit -m "Add facet-based workspace model"
```

---

### Task 2: Index merge

**Files:**
- Create: `internal/workspace/index.go`
- Test: `internal/workspace/index_test.go`

Merge rules: one `Workspace` per unique `ID`; later records fill missing facets; a session attaches to the workspace whose worktree path equals the session path on the same host; unmatched sessions become orphans. Workspaces sort by host, then repo label, then branch; orphans sort by name.

- [ ] **Step 1: Write the failing test**

```go
package workspace

import "testing"

func TestIndexMergesRecordsByID(t *testing.T) {
	git := []GitRecord{
		{ID: ID{Host: "local", Repo: "/repo", Branch: "topic"}, RepoLabel: "repo"},
		{ID: ID{Host: "local", Repo: "/repo", Branch: "topic"}, RepoLabel: "repo",
			Worktree: &Worktree{Path: "/wt/topic"}},
	}
	snap := Index(git, nil)
	if len(snap.Workspaces) != 1 {
		t.Fatalf("got %d workspaces, want 1", len(snap.Workspaces))
	}
	if !snap.Workspaces[0].Has(FacetWorktree) {
		t.Fatal("merged workspace should carry the worktree facet")
	}
}

func TestIndexAttachesSessionByWorktreePath(t *testing.T) {
	git := []GitRecord{{ID: ID{Host: "local", Repo: "/repo", Branch: "topic"},
		Worktree: &Worktree{Path: "/wt/topic"}}}
	sessions := []SessionRecord{{Host: "local", Session: Session{Name: "topic", Path: "/wt/topic"}}}
	snap := Index(git, sessions)
	if len(snap.Orphans) != 0 {
		t.Fatalf("got %d orphans, want 0", len(snap.Orphans))
	}
	if snap.Workspaces[0].Session == nil || snap.Workspaces[0].Session.Name != "topic" {
		t.Fatalf("session not attached: %+v", snap.Workspaces[0].Session)
	}
}

func TestIndexKeepsUnmatchedSessionAsOrphan(t *testing.T) {
	sessions := []SessionRecord{{Host: "local", Session: Session{Name: "stray", Path: "/tmp"}}}
	snap := Index(nil, sessions)
	if len(snap.Workspaces) != 0 {
		t.Fatalf("got %d workspaces, want 0", len(snap.Workspaces))
	}
	if len(snap.Orphans) != 1 || snap.Orphans[0].Session.Name != "stray" {
		t.Fatalf("orphans = %+v, want one named stray", snap.Orphans)
	}
}

func TestIndexSortsWorkspaces(t *testing.T) {
	git := []GitRecord{
		{ID: ID{Host: "local", Repo: "/b", Branch: "z"}, RepoLabel: "b"},
		{ID: ID{Host: "local", Repo: "/a", Branch: "b"}, RepoLabel: "a"},
		{ID: ID{Host: "local", Repo: "/a", Branch: "a"}, RepoLabel: "a"},
	}
	snap := Index(git, nil)
	got := []string{}
	for _, ws := range snap.Workspaces {
		got = append(got, ws.RepoLabel+"/"+ws.ID.Branch)
	}
	want := []string{"a/a", "a/b", "b/z"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/workspace/`
Expected: FAIL — `undefined: GitRecord`

- [ ] **Step 3: Write minimal implementation**

```go
package workspace

import "sort"

type GitRecord struct {
	ID        ID
	RepoLabel string
	Worktree  *Worktree
}

type SessionRecord struct {
	Host    string
	Session Session
}

type Snapshot struct {
	Workspaces []Workspace
	Orphans    []SessionRecord
}

func Index(git []GitRecord, sessions []SessionRecord) Snapshot {
	order := []ID{}
	byID := map[ID]*Workspace{}
	for _, record := range git {
		existing, ok := byID[record.ID]
		if !ok {
			byID[record.ID] = &Workspace{ID: record.ID, RepoLabel: record.RepoLabel, Worktree: record.Worktree}
			order = append(order, record.ID)
			continue
		}
		if existing.Worktree == nil {
			existing.Worktree = record.Worktree
		}
		if existing.RepoLabel == "" {
			existing.RepoLabel = record.RepoLabel
		}
	}

	orphans := []SessionRecord{}
	for _, record := range sessions {
		target := (*Workspace)(nil)
		for _, id := range order {
			ws := byID[id]
			if ws.ID.Host == record.Host && ws.Worktree != nil && ws.Worktree.Path == record.Session.Path && ws.Session == nil {
				target = ws
				break
			}
		}
		if target == nil {
			orphans = append(orphans, record)
			continue
		}
		session := record.Session
		target.Session = &session
	}

	workspaces := make([]Workspace, 0, len(order))
	for _, id := range order {
		workspaces = append(workspaces, *byID[id])
	}
	sort.SliceStable(workspaces, func(i, j int) bool {
		a, b := workspaces[i], workspaces[j]
		if a.ID.Host != b.ID.Host {
			return a.ID.Host < b.ID.Host
		}
		if a.RepoLabel != b.RepoLabel {
			return a.RepoLabel < b.RepoLabel
		}
		return a.ID.Branch < b.ID.Branch
	})
	sort.SliceStable(orphans, func(i, j int) bool {
		return orphans[i].Session.Name < orphans[j].Session.Name
	})
	return Snapshot{Workspaces: workspaces, Orphans: orphans}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/workspace/`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/workspace/
git commit -m "Merge facet records into a workspace snapshot"
```

---

### Task 3: Runner and git collector

**Files:**
- Create: `internal/collect/runner.go`, `internal/collect/git.go`
- Test: `internal/collect/git_test.go`

- [ ] **Step 1: Write the failing test**

```go
package collect

import (
	"strings"
	"testing"

	"github.com/Khady/workdash/internal/workspace"
)

type fakeRunner struct{ out map[string]string }

func (f fakeRunner) Run(cwd string, argv []string) (string, error) {
	return f.out[strings.Join(argv, " ")], nil
}

func TestGitCollectsWorktreesAndBranches(t *testing.T) {
	runner := fakeRunner{out: map[string]string{
		"git worktree list --porcelain": "worktree /repo\nHEAD abc123\nbranch refs/heads/main\n\n" +
			"worktree /wt/topic\nHEAD def456\nbranch refs/heads/topic\n",
		"git for-each-ref --format=%(refname:short) refs/heads": "main\ntopic\nidle\n",
	}}
	records, err := Git(runner, "local", Repo{Root: "/repo", Label: "repo"})
	if err != nil {
		t.Fatalf("Git() error = %v", err)
	}
	snap := workspace.Index(records, nil)
	if len(snap.Workspaces) != 3 {
		t.Fatalf("got %d workspaces, want 3", len(snap.Workspaces))
	}
	byBranch := map[string]workspace.Workspace{}
	for _, ws := range snap.Workspaces {
		byBranch[ws.ID.Branch] = ws
	}
	if wt := byBranch["main"].Worktree; wt == nil || !wt.IsMain || wt.Path != "/repo" {
		t.Fatalf("main worktree = %+v", wt)
	}
	if wt := byBranch["topic"].Worktree; wt == nil || wt.IsMain || wt.Head != "def456" {
		t.Fatalf("topic worktree = %+v", wt)
	}
	if byBranch["idle"].Has(workspace.FacetWorktree) {
		t.Fatal("branch without a worktree should have no worktree facet")
	}
}

func TestGitSkipsDetachedWorktrees(t *testing.T) {
	runner := fakeRunner{out: map[string]string{
		"git worktree list --porcelain": "worktree /repo\nHEAD abc123\ndetached\n",
		"git for-each-ref --format=%(refname:short) refs/heads": "",
	}}
	records, err := Git(runner, "local", Repo{Root: "/repo", Label: "repo"})
	if err != nil {
		t.Fatalf("Git() error = %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("got %d records, want 0", len(records))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/collect/`
Expected: FAIL — `undefined: Git`

- [ ] **Step 3: Write minimal implementation**

`internal/collect/runner.go`:

```go
package collect

import (
	"os/exec"
	"strings"
)

type Runner interface {
	Run(cwd string, argv []string) (string, error)
}

type LocalRunner struct{}

func (LocalRunner) Run(cwd string, argv []string) (string, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	return strings.TrimRight(string(out), "\n"), err
}
```

`internal/collect/git.go`:

```go
package collect

import (
	"strings"

	"github.com/Khady/workdash/internal/workspace"
)

type Repo struct {
	Root  string
	Label string
}

var (
	worktreeArgv = []string{"git", "worktree", "list", "--porcelain"}
	branchArgv   = []string{"git", "for-each-ref", "--format=%(refname:short)", "refs/heads"}
)

func Git(runner Runner, host string, repo Repo) ([]workspace.GitRecord, error) {
	worktreeOut, err := runner.Run(repo.Root, worktreeArgv)
	if err != nil {
		return nil, err
	}
	branchOut, err := runner.Run(repo.Root, branchArgv)
	if err != nil {
		return nil, err
	}
	records := parseWorktrees(worktreeOut, host, repo)
	for _, branch := range strings.Split(branchOut, "\n") {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			continue
		}
		records = append(records, workspace.GitRecord{
			ID:        workspace.ID{Host: host, Repo: repo.Root, Branch: branch},
			RepoLabel: repo.Label,
		})
	}
	return records, nil
}

func parseWorktrees(out, host string, repo Repo) []workspace.GitRecord {
	records := []workspace.GitRecord{}
	path, head, branch := "", "", ""
	first := true
	flush := func() {
		if path != "" && branch != "" {
			records = append(records, workspace.GitRecord{
				ID:        workspace.ID{Host: host, Repo: repo.Root, Branch: branch},
				RepoLabel: repo.Label,
				Worktree:  &Worktree{Path: path, Head: head, IsMain: first}.record(),
			})
			first = false
		} else if path != "" {
			first = false
		}
		path, head, branch = "", "", ""
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch refs/heads/"):
			branch = strings.TrimPrefix(line, "branch refs/heads/")
		}
	}
	flush()
	return records
}
```

Note: replace the `Worktree{...}.record()` sketch with a plain `&workspace.Worktree{Path: path, Head: head, IsMain: isMain}` — track `isMain` with a counter of flushed worktrees so the first `worktree` block is main even when it is detached.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/collect/`
Expected: PASS (2 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/collect/
git commit -m "Collect git worktrees and branches as facet records"
```

---

### Task 4: tmux collector

**Files:**
- Create: `internal/collect/tmux.go`
- Test: `internal/collect/tmux_test.go`

- [ ] **Step 1: Write the failing test**

```go
package collect

import (
	"errors"
	"strings"
	"testing"
)

type errRunner struct{ err error }

func (e errRunner) Run(string, []string) (string, error) { return "no server running on /tmp/tmux-1000/default", e.err }

func TestTmuxCollectsSessions(t *testing.T) {
	runner := fakeRunner{out: map[string]string{
		strings.Join(tmuxArgv, " "): "topic\t/wt/topic\t3\t1\nidle\t/repo\t1\t0\n",
	}}
	records, err := Tmux(runner, "local")
	if err != nil {
		t.Fatalf("Tmux() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	if records[0].Session.Name != "topic" || records[0].Session.Windows != 3 || !records[0].Session.Attached {
		t.Fatalf("first session = %+v", records[0].Session)
	}
	if records[1].Session.Attached {
		t.Fatal("second session should be detached")
	}
}

func TestTmuxTreatsMissingServerAsEmpty(t *testing.T) {
	records, err := Tmux(errRunner{err: errors.New("exit status 1")}, "local")
	if err != nil {
		t.Fatalf("Tmux() error = %v, want nil", err)
	}
	if len(records) != 0 {
		t.Fatalf("got %d records, want 0", len(records))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/collect/`
Expected: FAIL — `undefined: tmuxArgv`

- [ ] **Step 3: Write minimal implementation**

```go
package collect

import (
	"strconv"
	"strings"

	"github.com/Khady/workdash/internal/workspace"
)

var tmuxArgv = []string{"tmux", "list-sessions", "-F",
	"#{session_name}\t#{session_path}\t#{session_windows}\t#{session_attached}"}

func Tmux(runner Runner, host string) ([]workspace.SessionRecord, error) {
	out, err := runner.Run("", tmuxArgv)
	if err != nil {
		if strings.Contains(out, "no server running") || strings.Contains(err.Error(), "exit status 1") {
			return nil, nil
		}
		return nil, err
	}
	records := []workspace.SessionRecord{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) < 4 || fields[0] == "" {
			continue
		}
		windows, _ := strconv.Atoi(fields[2])
		records = append(records, workspace.SessionRecord{
			Host: host,
			Session: workspace.Session{
				Name:     fields[0],
				Path:     fields[1],
				Windows:  windows,
				Attached: fields[3] != "0",
			},
		})
	}
	return records, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/collect/`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/collect/
git commit -m "Collect tmux sessions as facet records"
```

---

### Task 5: Config

**Files:**
- Create: `internal/wdconf/config.go`
- Test: `internal/wdconf/config_test.go`

Config shape — `argv` as a list (no quoting DSL), `needs` replacing both `contexts` and `requires`, `where` replacing `run`/`scope`/`relaunch`/`remote_interactive`:

```toml
[[repos]]
root = "/home/louis/Code/github/workdash"

[[actions]]
id = "agent"
label = "launch agent"
needs = ["worktree"]
where = "pane"
argv = ["claude"]

[[actions]]
id = "gwt"
label = "create worktree"
where = "pane"
shell = true
argv = ["gwt", "{branch}"]
cwd = "{repo}"
```

- [ ] **Step 1: Write the failing test**

```go
package wdconf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReadsReposAndActions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wd.toml")
	body := `
[[repos]]
root = "/home/me/code/repo"

[[actions]]
id = "agent"
label = "launch agent"
needs = ["worktree"]
where = "pane"
argv = ["claude"]
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
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
	if action.ID != "agent" || action.Where != "pane" || action.Cwd != "{path}" {
		t.Fatalf("action = %+v, want cwd defaulted to {path}", action)
	}
}

func TestLoadRejectsUnknownWhere(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wd.toml")
	body := `
[[actions]]
id = "bad"
label = "bad"
where = "sideways"
argv = ["true"]
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error about `where`")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/wdconf/`
Expected: FAIL — `undefined: Load`

- [ ] **Step 3: Write minimal implementation**

```go
package wdconf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	WherePane    = "pane"
	WhereCapture = "capture"
)

type Repo struct {
	Root  string `toml:"root"`
	Label string `toml:"label"`
}

type Action struct {
	ID    string   `toml:"id"`
	Label string   `toml:"label"`
	Needs []string `toml:"needs"`
	Where string   `toml:"where"`
	Argv  []string `toml:"argv"`
	Cwd   string   `toml:"cwd"`
	Shell bool     `toml:"shell"`
}

type Config struct {
	Repos   []Repo   `toml:"repos"`
	Actions []Action `toml:"actions"`
}

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
	for i := range cfg.Repos {
		repo := &cfg.Repos[i]
		if strings.TrimSpace(repo.Root) == "" {
			return Config{}, fmt.Errorf("repo %d: `root` is required", i)
		}
		if repo.Label == "" {
			repo.Label = filepath.Base(strings.TrimRight(repo.Root, "/"))
		}
	}
	for i := range cfg.Actions {
		action := &cfg.Actions[i]
		if strings.TrimSpace(action.ID) == "" {
			return Config{}, fmt.Errorf("action %d: `id` is required", i)
		}
		if len(action.Argv) == 0 {
			return Config{}, fmt.Errorf("action %q: `argv` must be non-empty", action.ID)
		}
		if action.Where == "" {
			action.Where = WherePane
		}
		if action.Where != WherePane && action.Where != WhereCapture {
			return Config{}, fmt.Errorf("action %q: `where` must be %q or %q", action.ID, WherePane, WhereCapture)
		}
		if action.Cwd == "" {
			action.Cwd = "{path}"
		}
	}
	return cfg, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/wdconf/`
Expected: PASS (2 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/wdconf/
git commit -m "Add argv-and-facet based config"
```

---

### Task 6: Action resolution

**Files:**
- Create: `internal/action/action.go`
- Test: `internal/action/action_test.go`

Rules: skip an action when a `needs` facet is absent; substitute `{path}`, `{branch}`, `{repo}`, `{session}`, `{pr_url}` per argv element; skip the action when a referenced placeholder has no value.

- [ ] **Step 1: Write the failing test**

```go
package action

import (
	"strings"
	"testing"

	"github.com/Khady/workdash/internal/wdconf"
	"github.com/Khady/workdash/internal/workspace"
)

func ws() workspace.Workspace {
	return workspace.Workspace{
		ID:        workspace.ID{Host: "local", Repo: "/repo", Branch: "topic"},
		RepoLabel: "repo",
		Worktree:  &workspace.Worktree{Path: "/wt/topic"},
	}
}

func TestResolveSkipsActionsMissingFacets(t *testing.T) {
	defs := []wdconf.Action{
		{ID: "attach", Needs: []string{workspace.FacetSession}, Where: "pane", Argv: []string{"tmux", "attach"}, Cwd: "{path}"},
		{ID: "agent", Needs: []string{workspace.FacetWorktree}, Where: "pane", Argv: []string{"claude"}, Cwd: "{path}"},
	}
	got := Resolve(ws(), defs)
	if len(got) != 1 || got[0].ID != "agent" {
		t.Fatalf("resolved = %+v, want only agent", got)
	}
}

func TestResolveRendersPlaceholders(t *testing.T) {
	defs := []wdconf.Action{{ID: "gwt", Where: "pane", Shell: true,
		Argv: []string{"gwt", "{branch}"}, Cwd: "{repo}"}}
	got := Resolve(ws(), defs)
	if len(got) != 1 {
		t.Fatalf("resolved = %+v, want 1", got)
	}
	if strings.Join(got[0].Argv, " ") != "gwt topic" {
		t.Fatalf("argv = %v, want [gwt topic]", got[0].Argv)
	}
	if got[0].Cwd != "/repo" {
		t.Fatalf("cwd = %q, want /repo", got[0].Cwd)
	}
	if !got[0].Shell {
		t.Fatal("Shell flag should carry through")
	}
}

func TestResolveSkipsUnresolvablePlaceholders(t *testing.T) {
	defs := []wdconf.Action{{ID: "open-pr", Where: "capture", Argv: []string{"xdg-open", "{pr_url}"}, Cwd: "{path}"}}
	if got := Resolve(ws(), defs); len(got) != 0 {
		t.Fatalf("resolved = %+v, want none", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/action/`
Expected: FAIL — `undefined: Resolve`

- [ ] **Step 3: Write minimal implementation**

```go
package action

import (
	"strconv"
	"strings"

	"github.com/Khady/workdash/internal/wdconf"
	"github.com/Khady/workdash/internal/workspace"
)

type Resolved struct {
	ID    string
	Label string
	Where string
	Argv  []string
	Cwd   string
	Shell bool
	Host  string
}

func Resolve(ws workspace.Workspace, defs []wdconf.Action) []Resolved {
	values := placeholders(ws)
	resolved := []Resolved{}
	for _, def := range defs {
		if !hasFacets(ws, def.Needs) {
			continue
		}
		argv, ok := renderAll(def.Argv, values)
		if !ok {
			continue
		}
		cwd, ok := render(def.Cwd, values)
		if !ok {
			continue
		}
		resolved = append(resolved, Resolved{
			ID: def.ID, Label: def.Label, Where: def.Where,
			Argv: argv, Cwd: cwd, Shell: def.Shell, Host: ws.ID.Host,
		})
	}
	return resolved
}

func hasFacets(ws workspace.Workspace, needs []string) bool {
	for _, facet := range needs {
		if !ws.Has(facet) {
			return false
		}
	}
	return true
}

func placeholders(ws workspace.Workspace) map[string]string {
	values := map[string]string{
		"{branch}": ws.ID.Branch,
		"{repo}":   ws.ID.Repo,
		"{host}":   ws.ID.Host,
	}
	if ws.Worktree != nil {
		values["{path}"] = ws.Worktree.Path
	}
	if ws.Session != nil {
		values["{session}"] = ws.Session.Name
	}
	if ws.PR != nil {
		values["{pr_url}"] = ws.PR.URL
		values["{pr_number}"] = strconv.Itoa(ws.PR.Number)
	}
	return values
}

func renderAll(argv []string, values map[string]string) ([]string, bool) {
	out := make([]string, 0, len(argv))
	for _, arg := range argv {
		rendered, ok := render(arg, values)
		if !ok {
			return nil, false
		}
		out = append(out, rendered)
	}
	return out, true
}

func render(text string, values map[string]string) (string, bool) {
	for {
		start := strings.Index(text, "{")
		if start < 0 {
			return text, true
		}
		end := strings.Index(text[start:], "}")
		if end < 0 {
			return text, true
		}
		key := text[start : start+end+1]
		value, ok := values[key]
		if !ok || value == "" {
			return "", false
		}
		text = text[:start] + value + text[start+end+1:]
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/action/`
Expected: PASS (3 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/action/
git commit -m "Resolve actions against workspace facets"
```

---

### Task 7: Two-verb executor

**Files:**
- Create: `internal/launch/launch.go`
- Test: `internal/launch/launch_test.go`

`Pane` opens a tmux window (inside tmux) or creates a detached session (outside). `Capture` runs the argv and returns output. `Shell: true` wraps the argv in `bash -ic` so shell functions like `gwt` resolve — the single place quoting exists.

- [ ] **Step 1: Write the failing test**

```go
package launch

import (
	"strings"
	"testing"

	"github.com/Khady/workdash/internal/action"
)

type recordRunner struct{ calls [][]string }

func (r *recordRunner) Run(cwd string, argv []string) (string, error) {
	r.calls = append(r.calls, append([]string{cwd}, argv...))
	return "ok", nil
}

func TestPaneInsideTmuxOpensWindow(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: true}
	if _, err := tmux.Pane(action.Resolved{ID: "agent", Argv: []string{"claude"}, Cwd: "/wt/topic"}); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	got := strings.Join(runner.calls[0], " ")
	want := " tmux new-window -n agent -c /wt/topic -- claude"
	if got != want {
		t.Fatalf("call = %q, want %q", got, want)
	}
}

func TestPaneOutsideTmuxCreatesDetachedSession(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: false}
	out, err := tmux.Pane(action.Resolved{ID: "agent", Argv: []string{"claude"}, Cwd: "/wt/topic"})
	if err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	got := strings.Join(runner.calls[0], " ")
	if !strings.Contains(got, "tmux new-session -d -s agent -c /wt/topic -- claude") {
		t.Fatalf("call = %q", got)
	}
	if !strings.Contains(out, "tmux attach -t agent") {
		t.Fatalf("message = %q, want attach hint", out)
	}
}

func TestPaneWrapsShellActionsInLoginShell(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: true}
	if _, err := tmux.Pane(action.Resolved{ID: "gwt", Argv: []string{"gwt", "my topic"}, Cwd: "/repo", Shell: true}); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	got := strings.Join(runner.calls[0], " ")
	if !strings.HasSuffix(got, `-- bash -ic gwt 'my topic'`) {
		t.Fatalf("call = %q, want a bash -ic wrapper with quoting", got)
	}
}

func TestCaptureRunsInWorkspaceDirectory(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner}
	out, err := tmux.Capture(action.Resolved{ID: "status", Argv: []string{"git", "status"}, Cwd: "/wt/topic"})
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("out = %q, want ok", out)
	}
	if runner.calls[0][0] != "/wt/topic" {
		t.Fatalf("cwd = %q, want /wt/topic", runner.calls[0][0])
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{"plain": "plain", "two words": "'two words'", "it's": `'it'\''s'`}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Fatalf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/launch/`
Expected: FAIL — `undefined: Tmux`

- [ ] **Step 3: Write minimal implementation**

```go
package launch

import (
	"fmt"
	"strings"

	"github.com/Khady/workdash/internal/action"
	"github.com/Khady/workdash/internal/collect"
)

type Executor interface {
	Pane(action.Resolved) (string, error)
	Capture(action.Resolved) (string, error)
}

type Tmux struct {
	Runner     collect.Runner
	InsideTmux bool
}

func (t Tmux) Pane(resolved action.Resolved) (string, error) {
	argv := commandArgv(resolved)
	if t.InsideTmux {
		call := append([]string{"tmux", "new-window", "-n", resolved.ID, "-c", resolved.Cwd, "--"}, argv...)
		if _, err := t.Runner.Run("", call); err != nil {
			return "", err
		}
		return fmt.Sprintf("opened tmux window %q", resolved.ID), nil
	}
	call := append([]string{"tmux", "new-session", "-d", "-s", resolved.ID, "-c", resolved.Cwd, "--"}, argv...)
	if _, err := t.Runner.Run("", call); err != nil {
		return "", err
	}
	return fmt.Sprintf("created detached session %q; attach with `tmux attach -t %s`", resolved.ID, resolved.ID), nil
}

func (t Tmux) Capture(resolved action.Resolved) (string, error) {
	return t.Runner.Run(resolved.Cwd, commandArgv(resolved))
}

func commandArgv(resolved action.Resolved) []string {
	if !resolved.Shell {
		return resolved.Argv
	}
	quoted := make([]string, 0, len(resolved.Argv))
	for _, arg := range resolved.Argv {
		quoted = append(quoted, shellQuote(arg))
	}
	return []string{"bash", "-ic", strings.Join(quoted, " ")}
}

func shellQuote(text string) string {
	if text != "" && !strings.ContainsAny(text, " \t\n'\"\\$`*?[]|&;<>(){}") {
		return text
	}
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/launch/`
Expected: PASS (5 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/launch/
git commit -m "Add two-verb tmux executor"
```

---

### Task 8: CLI client

**Files:**
- Create: `cmd/wd/main.go`, `cmd/wd/selector.go`
- Test: `cmd/wd/selector_test.go`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"testing"

	"github.com/Khady/workdash/internal/workspace"
)

func snapshot() workspace.Snapshot {
	return workspace.Snapshot{Workspaces: []workspace.Workspace{
		{ID: workspace.ID{Host: "local", Repo: "/a", Branch: "topic"}, RepoLabel: "a"},
		{ID: workspace.ID{Host: "local", Repo: "/b", Branch: "topic"}, RepoLabel: "b"},
		{ID: workspace.ID{Host: "local", Repo: "/a", Branch: "solo"}, RepoLabel: "a"},
	}}
}

func TestSelectMatchesUniqueBranch(t *testing.T) {
	got, err := Select(snapshot(), "solo")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.RepoLabel != "a" || got.ID.Branch != "solo" {
		t.Fatalf("selected = %+v", got)
	}
}

func TestSelectRejectsAmbiguousBranch(t *testing.T) {
	_, err := Select(snapshot(), "topic")
	if err == nil {
		t.Fatal("Select() error = nil, want ambiguity error")
	}
	if got := err.Error(); !contains(got, "a:topic") || !contains(got, "b:topic") {
		t.Fatalf("error = %q, want both candidates listed", got)
	}
}

func TestSelectAcceptsRepoQualifiedBranch(t *testing.T) {
	got, err := Select(snapshot(), "b:topic")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got.RepoLabel != "b" {
		t.Fatalf("selected = %+v, want repo b", got)
	}
}

func TestSelectReportsUnknownSelector(t *testing.T) {
	if _, err := Select(snapshot(), "nope"); err == nil {
		t.Fatal("Select() error = nil, want not-found error")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/wd/`
Expected: FAIL — `undefined: Select`

- [ ] **Step 3: Write minimal implementation**

`cmd/wd/selector.go`:

```go
package main

import (
	"fmt"
	"strings"

	"github.com/Khady/workdash/internal/workspace"
)

func Select(snap workspace.Snapshot, selector string) (workspace.Workspace, error) {
	repo, branch := "", selector
	if idx := strings.LastIndex(selector, ":"); idx > 0 {
		repo, branch = selector[:idx], selector[idx+1:]
	}
	matches := []workspace.Workspace{}
	for _, ws := range snap.Workspaces {
		if ws.ID.Branch != branch {
			continue
		}
		if repo != "" && ws.RepoLabel != repo {
			continue
		}
		matches = append(matches, ws)
	}
	switch len(matches) {
	case 0:
		return workspace.Workspace{}, fmt.Errorf("no workspace matches %q", selector)
	case 1:
		return matches[0], nil
	}
	names := []string{}
	for _, ws := range matches {
		names = append(names, ws.RepoLabel+":"+ws.ID.Branch)
	}
	return workspace.Workspace{}, fmt.Errorf("%q is ambiguous: %s", selector, strings.Join(names, ", "))
}
```

`cmd/wd/main.go` — wires config → collectors → index → print or execute:

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/Khady/workdash/internal/action"
	"github.com/Khady/workdash/internal/collect"
	"github.com/Khady/workdash/internal/launch"
	"github.com/Khady/workdash/internal/wdconf"
	"github.com/Khady/workdash/internal/workspace"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "wd: "+err.Error())
		os.Exit(1)
	}
}

func run(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("usage: wd ls [--json] | wd run <selector> <action> | wd actions <selector>")
	}
	cfg, err := wdconf.Load(wdconf.DefaultPath())
	if err != nil {
		return err
	}
	snap, warnings := loadSnapshot(cfg)
	for _, warning := range warnings {
		fmt.Fprintln(os.Stderr, "wd: "+warning)
	}
	switch argv[0] {
	case "ls":
		if len(argv) > 1 && argv[1] == "--json" {
			return json.NewEncoder(os.Stdout).Encode(snap)
		}
		printSnapshot(snap)
		return nil
	case "actions":
		if len(argv) < 2 {
			return fmt.Errorf("usage: wd actions <selector>")
		}
		ws, err := Select(snap, argv[1])
		if err != nil {
			return err
		}
		for _, resolved := range action.Resolve(ws, cfg.Actions) {
			fmt.Printf("%-12s %s\n", resolved.ID, resolved.Label)
		}
		return nil
	case "run":
		if len(argv) < 3 {
			return fmt.Errorf("usage: wd run <selector> <action>")
		}
		ws, err := Select(snap, argv[1])
		if err != nil {
			return err
		}
		for _, resolved := range action.Resolve(ws, cfg.Actions) {
			if resolved.ID != argv[2] {
				continue
			}
			executor := launch.Tmux{Runner: collect.LocalRunner{}, InsideTmux: os.Getenv("TMUX") != ""}
			out := ""
			if resolved.Where == wdconf.WhereCapture {
				out, err = executor.Capture(resolved)
			} else {
				out, err = executor.Pane(resolved)
			}
			if err != nil {
				return err
			}
			if out != "" {
				fmt.Println(out)
			}
			return nil
		}
		return fmt.Errorf("action %q is not available for %q", argv[2], argv[1])
	}
	return fmt.Errorf("unknown command %q", argv[0])
}

func loadSnapshot(cfg wdconf.Config) (workspace.Snapshot, []string) {
	runner := collect.LocalRunner{}
	warnings := []string{}
	records := []workspace.GitRecord{}
	for _, repo := range cfg.Repos {
		got, err := collect.Git(runner, "local", collect.Repo{Root: repo.Root, Label: repo.Label})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", repo.Label, err))
			continue
		}
		records = append(records, got...)
	}
	sessions, err := collect.Tmux(runner, "local")
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("tmux: %v", err))
	}
	return workspace.Index(records, sessions), warnings
}

func printSnapshot(snap workspace.Snapshot) {
	for _, ws := range snap.Workspaces {
		facets := strings.Join(ws.Facets(), ",")
		if facets == "" {
			facets = "-"
		}
		fmt.Printf("%-10s %-24s %-30s %s\n", ws.ID.Host, ws.RepoLabel, ws.ID.Branch, facets)
	}
	for _, orphan := range snap.Orphans {
		fmt.Printf("%-10s %-24s %-30s %s\n", orphan.Host, "(no repo)", orphan.Session.Name, "session")
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/wd/` then `go build ./...` and `go test ./...`
Expected: PASS, build clean, existing tests still green

- [ ] **Step 5: Commit**

```bash
git add cmd/wd/
git commit -m "Add wd CLI client over the workspace core"
```

---

### Task 9: End-to-end check against a real repo

**Files:**
- Create: `docs/wd.example.toml`

- [ ] **Step 1: Write an example config**

```toml
[[repos]]
root = "/home/louis/Code/github/workdash"

[[actions]]
id = "shell"
label = "open shell"
needs = ["worktree"]
where = "pane"
argv = ["bash"]

[[actions]]
id = "agent"
label = "launch claude"
needs = ["worktree"]
where = "pane"
argv = ["claude"]

[[actions]]
id = "status"
label = "git status"
needs = ["worktree"]
where = "capture"
argv = ["git", "status", "--short", "--branch"]

[[actions]]
id = "gwt"
label = "create worktree"
where = "pane"
shell = true
argv = ["gwt", "{branch}"]
cwd = "{repo}"
```

- [ ] **Step 2: Run the CLI against the real repository**

```bash
go build -o /tmp/wd ./cmd/wd && WD_CONFIG=docs/wd.example.toml /tmp/wd ls
```

Expected: one row per local branch of this repo, with `worktree` on checked-out branches and `worktree,session` on any branch whose worktree path matches a live tmux session path.

- [ ] **Step 3: Verify a capture action end to end**

```bash
WD_CONFIG=docs/wd.example.toml /tmp/wd run rewrite/core-facets status
```

Expected: `git status --short --branch` output for the workspace.

- [ ] **Step 4: Commit**

```bash
git add docs/wd.example.toml
git commit -m "Add example wd config"
```

---

## Self-Review

**Spec coverage:** facet model (T1), derived session↔worktree join with orphans (T2), git collector (T3), tmux collector (T4), argv/needs/where config with the old `run`/`scope`/`contexts`/`relaunch` keys gone (T5), facet-driven action resolution (T6), two-verb executor plus the `shell: true` escape hatch that lets workdash call `gwt` rather than reimplement it (T7), CLI as a first-class frontend (T8), real-repo verification (T9). Remote hosts, PRs, daemon, TUI, and browser are explicit non-goals for this slice; the `PR` facet and `Host` field exist so they slot in without a model change.

**Placeholder scan:** the only prose-instead-of-code step is the `IsMain` note in Task 3 Step 3, which names the exact fix (count flushed worktree blocks) — resolve it while implementing rather than copying the sketch.

**Type consistency:** `workspace.GitRecord`/`SessionRecord`/`Snapshot` are defined in T2 and consumed in T3, T4, T8. `wdconf.Action` is defined in T5 and consumed in T6. `action.Resolved` is defined in T6 and consumed in T7, T8. `collect.Runner` is defined in T3 and consumed in T4, T7, T8. `Select` is defined in T8 and used only there.
