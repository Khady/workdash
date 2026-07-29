// Package launch executes resolved actions. It has exactly two verbs.
//
// Pane puts a command in a tmux window, which is the substrate for anything
// interactive: tmux owns the tabs, the scrollback, and the survival of the
// process, so workdash never has to hand a shell back to a caller or spawn a
// terminal emulator.
//
// Capture runs a command and returns its output for the calling frontend to
// display.
package launch

import (
	"fmt"

	"github.com/Khady/workdash/internal/action"
	"github.com/Khady/workdash/internal/shellquote"
)

// Runner executes a command somewhere. Declared here rather than imported so
// the executor does not depend on the collector package; any collect.Runner
// satisfies it structurally.
type Runner interface {
	Run(cwd string, argv []string) (string, error)
}

type Executor interface {
	Pane(action.Resolved) (string, error)
	Capture(action.Resolved) (string, error)
	// Attach connects to a session that already exists, creating nothing. It is
	// its own verb because "take me to the work that is already running" is not a
	// command to run, and it is what a dashboard built around focus rather than
	// launch mostly does.
	Attach(action.Resolved) (string, error)
}

type Tmux struct {
	Runner     Runner
	InsideTmux bool
}

func (t Tmux) Pane(resolved action.Resolved) (string, error) {
	name := paneName(resolved)
	argv := commandArgv(resolved)
	if t.InsideTmux {
		// Target the workspace's own session when it has one, so related work
		// stays together instead of landing in whichever session you happen to be
		// attached to.
		target := []string{}
		if resolved.Session != "" {
			target = []string{"-t" + exactTarget(resolved.Session) + ":"}
		}
		call := append(append([]string{"tmux", "new-window"}, target...), "-n", name, "-c", resolved.Cwd, "--")
		call = append(call, argv...)
		if _, err := t.Runner.Run("", call); err != nil {
			return "", err
		}
		return fmt.Sprintf("opened tmux window %q", name), nil
	}
	// A workspace may carry several sessions, so this is routinely the second
	// action on the same branch. Creating a session that already exists fails
	// with `duplicate session`, so an existing one gets a window instead.
	if t.hasSession(name) {
		call := append([]string{"tmux", "new-window", "-t" + exactTarget(name) + ":", "-n", name, "-c", resolved.Cwd, "--"}, argv...)
		if _, err := t.Runner.Run("", call); err != nil {
			return "", err
		}
		return fmt.Sprintf("opened window %q in existing session %q; attach with `tmux attach -t %s`", name, name, name), nil
	}
	call := append([]string{"tmux", "new-session", "-d", "-s", name, "-c", resolved.Cwd, "--"}, argv...)
	if _, err := t.Runner.Run("", call); err != nil {
		return "", err
	}
	return fmt.Sprintf("created detached session %q; attach with `tmux attach -t %s`", name, name), nil
}

func (t Tmux) Attach(resolved action.Resolved) (string, error) {
	if resolved.Session == "" {
		return "", fmt.Errorf("nothing to attach to: %s has no session", resolved.Name)
	}
	verb := "attach"
	if t.InsideTmux {
		// switch-client moves the client you are already using; attach would try to
		// nest a session inside itself.
		verb = "switch-client"
	}
	if _, err := t.Runner.Run("", []string{"tmux", verb, "-t" + exactTarget(resolved.Session)}); err != nil {
		return "", err
	}
	return fmt.Sprintf("attached to session %q", resolved.Session), nil
}

func (t Tmux) Capture(resolved action.Resolved) (string, error) {
	return t.Runner.Run(resolved.Cwd, commandArgv(resolved))
}

// hasSession probes for a session by name. Any failure — no server, no such
// session, no tmux at all — means there is nothing to add a window to, which is
// exactly the case where creating a session is right.
func (t Tmux) hasSession(name string) bool {
	_, err := t.Runner.Run("", []string{"tmux", "has-session", "-t" + exactTarget(name)})
	return err == nil
}

// exactTarget builds a tmux target that only matches this session. Without the
// "=" prefix tmux also accepts a prefix match, so a workspace named `topic`
// would find the unrelated session `topic-2` and drop the window in it.
func exactTarget(name string) string {
	return "=" + name
}

// paneName prefers the workspace name so the tmux window picker stays readable;
// actions resolved without one fall back to the action id.
func paneName(resolved action.Resolved) string {
	if resolved.Name != "" {
		return resolved.Name
	}
	return resolved.ID
}

// commandArgv keeps argv as argv unless the action opts into a shell. Commands
// like `gwt` are shell functions from the user's rc rather than binaries on
// PATH, so they need an interactive shell to resolve — and that is the only
// place in workdash where a command becomes a quoted string.
func commandArgv(resolved action.Resolved) []string {
	if !resolved.Shell {
		return resolved.Argv
	}
	return []string{"bash", "-ic", shellquote.Join(resolved.Argv)}
}
