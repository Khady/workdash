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
	name := paneName(resolved)
	argv := commandArgv(resolved)
	if t.InsideTmux {
		call := append([]string{"tmux", "new-window", "-n", name, "-c", resolved.Cwd, "--"}, argv...)
		if _, err := t.Runner.Run("", call); err != nil {
			return "", err
		}
		return fmt.Sprintf("opened tmux window %q", name), nil
	}
	call := append([]string{"tmux", "new-session", "-d", "-s", name, "-c", resolved.Cwd, "--"}, argv...)
	if _, err := t.Runner.Run("", call); err != nil {
		return "", err
	}
	return fmt.Sprintf("created detached session %q; attach with `tmux attach -t %s`", name, name), nil
}

func (t Tmux) Capture(resolved action.Resolved) (string, error) {
	return t.Runner.Run(resolved.Cwd, commandArgv(resolved))
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
	quoted := make([]string, 0, len(resolved.Argv))
	for _, arg := range resolved.Argv {
		quoted = append(quoted, shellQuote(arg))
	}
	return []string{"bash", "-ic", strings.Join(quoted, " ")}
}

func shellQuote(text string) string {
	if text != "" && !strings.ContainsAny(text, " \t\n'\"\\$`*?[]|&;<>(){}#~=") {
		return text
	}
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}
