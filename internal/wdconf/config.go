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
