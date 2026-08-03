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
	// Session is the workspace's existing tmux session, when it has one. The
	// executor targets it instead of inventing a name, so an action lands in the
	// session already doing that work rather than beside it.
	Session string `json:"session,omitempty"`
	// Attach overrides how a remote pane is reached, without the ssh target,
	// which the executor appends. mosh survives a suspended laptop but breaks
	// agent and port forwarding, so which transport to use is a property of the
	// action, not of the host.
	Attach []string `json:"attach,omitempty"`
	// RunOn is empty for the workspace's host, or "local" for this machine.
	RunOn string `json:"run_on,omitempty"`
	Shell bool   `json:"shell"`
	Host  string `json:"host"`
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
		if !workspace.Matches(ws, def.Needs) {
			continue
		}
		if !inScope(ws.ID.Host, def.Scope) {
			continue
		}
		if !onHost(ws.ID.Host, def.Hosts) {
			continue
		}
		argv, ok := renderAll(def.Argv, values)
		if !ok {
			continue
		}
		cwd, ok := resolveCwd(def.Cwd, values)
		if !ok {
			continue
		}
		resolved = append(resolved, Resolved{
			ID:      def.ID,
			Label:   def.Label,
			Name:    tmuxName(ws.ID.Branch),
			Where:   def.Where,
			Argv:    argv,
			Cwd:     cwd,
			Session: firstSession(ws),
			Attach:  def.AttachArgv,
			RunOn:   def.RunOn,
			Shell:   def.Shell,
			Host:    ws.ID.Host,
		})
	}
	return resolved
}

// resolveCwd distinguishes an omitted cwd from a configured one. Omitted means
// "the worktree if there is one", so an action that needs no directory — opening
// a pull request in a browser — stays available on a workspace that has no
// worktree. A configured cwd is a requirement, and an unresolvable one drops the
// action rather than running it somewhere unintended.
func resolveCwd(cwd string, values map[string]string) (string, bool) {
	if cwd == "" {
		return values["{path}"], true
	}
	return render(cwd, values)
}

func firstSession(ws workspace.Workspace) string {
	if len(ws.Sessions) == 0 {
		return ""
	}
	return ws.Sessions[0].Name
}

// inScope reports whether a workspace's host matches an action's scope. Empty
// scope means both, so scoping stays opt-in.
func inScope(host, scope string) bool {
	switch scope {
	case wdconf.ScopeLocal:
		return host == wdconf.LocalHost
	case wdconf.ScopeRemote:
		return host != wdconf.LocalHost
	}
	return true
}

// onHost reports whether an action applies to a host. An empty list means every
// host, so scoping is opt-in.
func onHost(host string, hosts []string) bool {
	if len(hosts) == 0 {
		return true
	}
	for _, candidate := range hosts {
		if candidate == host {
			return true
		}
	}
	return false
}

func placeholders(ws workspace.Workspace) map[string]string {
	values := map[string]string{
		"{branch}": ws.ID.Branch,
		"{repo}":   ws.ID.Repo,
		"{host}":   ws.ID.Host,
	}
	// {ssh_target} lets a local editor open a remote path: "code --remote
	// ssh-remote+{ssh_target} {path}". Empty on the local host, which makes such
	// an action inapplicable there — correct, since a local editor opens the path
	// directly.
	if ws.SSHTarget != "" {
		values["{ssh_target}"] = ws.SSHTarget
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
	"{session}": true, "{pr_url}": true, "{pr_number}": true, "{ssh_target}": true,
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
