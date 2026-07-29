// Package action turns declarative action definitions into concrete commands
// for one workspace.
//
// An action is applicable when every facet it declares in `needs` is present
// and every placeholder it references has a value. Nothing here executes
// anything: resolution and execution are separate so every frontend resolves
// the same way and only the executor touches the machine.
package action

import (
	"strconv"
	"strings"

	"github.com/Khady/workdash/internal/wdconf"
	"github.com/Khady/workdash/internal/workspace"
)

type Resolved struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Name  string   `json:"name"`
	Where string   `json:"where"`
	Argv  []string `json:"argv"`
	Cwd   string   `json:"cwd"`
	Shell bool     `json:"shell"`
	Host  string   `json:"host"`
}

// tmuxName makes a branch usable as a tmux window or session name. tmux treats
// "." and ":" as target separators, so they cannot survive in a name; slashes
// are fine and worth keeping because they carry the branch's namespace.
func tmuxName(branch string) string {
	name := strings.NewReplacer(".", "-", ":", "-").Replace(branch)
	return strings.Trim(name, "-")
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
			ID:    def.ID,
			Label: def.Label,
			Name:  tmuxName(ws.ID.Branch),
			Where: def.Where,
			Argv:  argv,
			Cwd:   cwd,
			Shell: def.Shell,
			Host:  ws.ID.Host,
		})
	}
	return resolved
}

// hasFacets evaluates `needs`. A "!" prefix negates, so an action can require a
// facet to be absent — "create worktree" belongs only on workspaces that have
// none. Facet absence is a first-class state, so the action language has to be
// able to select on it.
func hasFacets(ws workspace.Workspace, needs []string) bool {
	for _, facet := range needs {
		want := true
		if strings.HasPrefix(facet, "!") {
			want, facet = false, strings.TrimPrefix(facet, "!")
		}
		if ws.Has(facet) != want {
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
	// {session} is the first session on the workspace. Actions that need to pick
	// between several belong in a frontend that can ask, not in a template.
	if len(ws.Sessions) > 0 {
		values["{session}"] = ws.Sessions[0].Name
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

// known lists every placeholder workdash understands. Brace spans outside this
// set are left alone, because plenty of real commands contain braces of their
// own: `awk '{print $1}'`, `git log --format={...}`, `find -name '*.{go,md}'`.
// Treating those as placeholders silently deleted the action.
var known = map[string]bool{
	"{path}": true, "{branch}": true, "{repo}": true, "{host}": true,
	"{session}": true, "{pr_url}": true, "{pr_number}": true,
}

// render substitutes known {placeholder} spans and passes anything else through
// verbatim. A known placeholder with no value for this workspace makes the whole
// action inapplicable rather than producing a command with a hole in it, which
// is why the second return value exists.
func render(text string, values map[string]string) (string, bool) {
	done := ""
	for {
		start := strings.Index(text, "{")
		if start < 0 {
			return done + text, true
		}
		end := strings.Index(text[start:], "}")
		if end < 0 {
			return done + text, true
		}
		key := text[start : start+end+1]
		if !known[key] {
			done += text[:start+end+1]
			text = text[start+end+1:]
			continue
		}
		value := values[key]
		if value == "" {
			return "", false
		}
		done += text[:start] + value
		text = text[start+end+1:]
	}
}
