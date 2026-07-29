package wdconf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/Khady/workdash/internal/workspace"
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
	// Labels are how the CLI names a repo, so they have to stay unique and
	// colon-free: `wd run LABEL:BRANCH ACTION` splits on the first colon, and two
	// repos sharing a label leave no selector able to tell them apart.
	rootByLabel := map[string]string{}
	for i := range cfg.Repos {
		repo := &cfg.Repos[i]
		if strings.TrimSpace(repo.Root) == "" {
			return Config{}, fmt.Errorf("repo %d: `root` is required", i)
		}
		repo.Root = filepath.Clean(expandHome(strings.TrimSpace(repo.Root)))
		if repo.Label == "" {
			repo.Label = filepath.Base(repo.Root)
		}
		if strings.Contains(repo.Label, ":") {
			return Config{}, fmt.Errorf("repo %q: `label` %q must not contain %q", repo.Root, repo.Label, ":")
		}
		if previous, ok := rootByLabel[repo.Label]; ok {
			return Config{}, fmt.Errorf("duplicate repo `label` %q used by %q and %q: set a distinct `label` on one of them", repo.Label, previous, repo.Root)
		}
		rootByLabel[repo.Label] = repo.Root
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
		for _, need := range action.Needs {
			if err := checkFacet(action.ID, need); err != nil {
				return Config{}, err
			}
		}
	}
	return cfg, nil
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
