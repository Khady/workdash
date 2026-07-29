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
