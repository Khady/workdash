package wdconf

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestLoadRejectsEmptyArgv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wd.toml")
	body := `
[[actions]]
id = "empty"
label = "empty"
where = "pane"
argv = []
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error about `argv`")
	}
}

func TestLoadExpandsTildeInRepoRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	path := filepath.Join(dir, "wd.toml")
	body := `
[[repos]]
root = "~/Code/workdash"

[[repos]]
root = "~"
label = "home"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
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
	dir := t.TempDir()
	path := filepath.Join(dir, "wd.toml")
	body := `
[[repos]]
root = "/x/repo/"
label = "slashed"

[[repos]]
root = "/x/./repo"
label = "clean"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
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
	dir := t.TempDir()
	path := filepath.Join(dir, "wd.toml")
	body := `
[[repos]]
root = "/work/a/api"

[[repos]]
root = "/work/b/api"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
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
	dir := t.TempDir()
	path := filepath.Join(dir, "wd.toml")
	body := `
[[repos]]
root = "/work/a/api"
label = "a-api"

[[repos]]
root = "/work/b/api"
label = "b-api"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Repos) != 2 {
		t.Fatalf("repos = %+v, want 2", cfg.Repos)
	}
}

func TestLoadRejectsLabelContainingColon(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wd.toml")
	body := `
[[repos]]
root = "/work/api"
label = "api:main"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error about the colon in `label`")
	}
	if !strings.Contains(err.Error(), "api:main") {
		t.Fatalf("Load() error = %v, want it to name the offending label", err)
	}
}

func TestLoadRejectsUnknownFacetInNeeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wd.toml")
	body := `
[[actions]]
id = "typo"
label = "typo"
needs = ["worktee"]
argv = ["true"]
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want an error about an unknown facet in `needs`")
	}
	if !strings.Contains(err.Error(), "worktee") {
		t.Fatalf("Load() error = %v, want it to name the offending facet", err)
	}
}

func TestLoadRejectsNegatedUnknownFacetInNeeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wd.toml")
	body := `
[[actions]]
id = "typo"
label = "typo"
needs = ["!worktee"]
argv = ["true"]
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want an error about an unknown facet in `needs`")
	}
}

func TestLoadRejectsEmptyFacetInNeeds(t *testing.T) {
	for _, needs := range []string{`[""]`, `["!"]`} {
		dir := t.TempDir()
		path := filepath.Join(dir, "wd.toml")
		body := `
[[actions]]
id = "blank"
label = "blank"
needs = ` + needs + `
argv = ["true"]
`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("Load() with needs = %s error = nil, want an error about `needs`", needs)
		}
	}
}

func TestLoadAcceptsKnownAndNegatedFacetsInNeeds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wd.toml")
	body := `
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
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
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
	dir := t.TempDir()
	path := filepath.Join(dir, "wd.toml")
	body := `
[[actions]]
id = "bare"
label = "bare"
argv = ["true"]
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
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
