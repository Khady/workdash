package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigReadsLocalAndRemoteHosts(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(`
tmux = false
config_editor = "nvim"
terminal_launcher = "gnome-terminal -- bash -lc {command}"
terminal_launcher_default = false

[[repos]]
root = "`+repo+`"
label = "repo"
gh_account = "work"

[[hosts]]
label = "devbox"
ssh_target = "me@devbox"
tmux = true

[[hosts.repos]]
root = "/srv/repo"
label = "remote"
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.LocalHost.TmuxEnabled || got.Config.ConfigEditor != "nvim" || got.Config.TerminalLauncherDefault {
		t.Fatalf("unexpected top-level config: %#v", got.Config)
	}
	if len(got.Config.LocalHost.Repos) != 1 || got.Config.LocalHost.Repos[0].GHAccount != "work" {
		t.Fatalf("unexpected local repos: %#v", got.Config.LocalHost.Repos)
	}
	if len(got.Config.RemoteHosts) != 1 || got.Config.RemoteHosts[0].Repos[0].Root != "/srv/repo" {
		t.Fatalf("unexpected remote hosts: %#v", got.Config.RemoteHosts)
	}
}

func TestLoadConfigReadsCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(`
[[commands]]
id = "open-shell"
shortcut = "s"
label = "open shell"
detail = "{path}"
command = "bash"
cwd = "{path}"
run = "shell"
contexts = ["worktree", "branch"]
requires = ["path"]
remote = true
remote_interactive = true
relaunch = "always"
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Config.Commands) != 1 {
		t.Fatalf("unexpected commands: %#v", got.Config.Commands)
	}
	command := got.Config.Commands[0]
	if command.ID != "open-shell" || command.Shortcut != 's' || !command.Remote || !command.RemoteInteractive || command.Relaunch != "always" {
		t.Fatalf("unexpected command: %#v", command)
	}
}

func TestDefaultConfigHasNoCommands(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Config.Commands) != 0 {
		t.Fatalf("unexpected default commands: %#v", got.Config.Commands)
	}
}

func TestLoadConfigRejectsEmptyGHAccount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(`
[[repos]]
root = "/tmp"
gh_account = ""
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadConfigDefaultsRepoLabelToBasename(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(`
[[repos]]
root = "`+repo+`"

[[hosts]]
label = "devbox"
ssh_target = "me@devbox"
tmux = false

[[hosts.repos]]
root = "/srv/code/example-repo"
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Config.LocalHost.Repos) != 1 || got.Config.LocalHost.Repos[0].Label != filepath.Base(repo) {
		t.Fatalf("unexpected local repo labels: %#v", got.Config.LocalHost.Repos)
	}
	if len(got.Config.RemoteHosts) != 1 || len(got.Config.RemoteHosts[0].Repos) != 1 || got.Config.RemoteHosts[0].Repos[0].Label != "example-repo" {
		t.Fatalf("unexpected remote repo labels: %#v", got.Config.RemoteHosts)
	}
}

func TestLoadConfigWarnsWhenMissing(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Warnings) != 1 || !got.Config.LocalHost.TmuxEnabled {
		t.Fatalf("unexpected result: %#v", got)
	}
}
