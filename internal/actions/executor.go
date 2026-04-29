package actions

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/Khady/workdash/internal/model"
)

type InlineAction interface {
	model.ShellAction
	inline()
}

func (KillTmuxSessionAction) inline() {}
func (OpenInBrowserAction) inline()   {}

type InlineActionError struct{ Detail string }

func (e InlineActionError) Error() string { return e.Detail }

type TerminalLaunchError struct{ Detail string }

func (e TerminalLaunchError) Error() string { return e.Detail }

func AsInlineAction(action model.ShellAction) (InlineAction, bool) {
	inline, ok := action.(InlineAction)
	return inline, ok
}

func IsTerminalLaunchAction(action model.ShellAction) bool {
	_, ok := action.(TerminalLaunchAction)
	return ok
}

func ExecuteInlineAction(action InlineAction) error {
	switch action.(type) {
	case OpenInBrowserAction:
		return exec.Command("/bin/bash", "-lc", action.ToShell()).Start()
	}
	if configured, ok := action.(ConfiguredInlineAction); ok && configured.Run == "background" {
		return exec.Command("/bin/bash", "-lc", action.ToShell()).Start()
	}
	cmd := exec.Command("/bin/bash", "-lc", action.ToShell())
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	detail := firstLine(string(out))
	if detail == "" {
		detail = err.Error()
	}
	return InlineActionError{Detail: detail}
}

func ExecuteTerminalLaunchAction(action model.ShellAction, terminalLauncher string) error {
	command := TerminalLaunchCommand(action)
	if command == "" {
		return TerminalLaunchError{Detail: "Action cannot be launched in a terminal"}
	}
	launcherCommand := stringsReplaceCommand(terminalLauncher, Quote(command))
	cmd := exec.Command("/bin/bash", "-lc", launcherCommand)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	detail := firstLine(string(out))
	if detail == "" {
		detail = err.Error()
	}
	return TerminalLaunchError{Detail: detail}
}

func DescribeAction(action model.ShellAction) (title, detail, success string) {
	if t, ok := action.(TerminalLaunchAction); ok {
		action = t.Action
	}
	switch a := action.(type) {
	case ConfiguredShellAction:
		return "Running " + a.Label, a.Detail, "Completed " + a.Label
	case ConfiguredInlineAction:
		return "Running " + a.Label, a.Detail, "Completed " + a.Label
	case CdAction:
		return "Opening terminal", a.Path, "Opened terminal at " + a.Path
	case RemoteShellAction:
		return "Opening remote terminal", a.SSHTarget + ":" + a.Path, "Opened remote terminal for " + a.SSHTarget
	case EditConfigAction:
		return "Opening config", a.Path, "Opened config " + a.Path
	case OpenInBrowserAction:
		return "Opening in browser", a.URL, "Opened " + a.URL + " in browser"
	case TmuxAction:
		return "Opening tmux session", a.Session, "Opened tmux session " + a.Session
	case KillTmuxSessionAction:
		return "Killing tmux session", a.Session, "Killed tmux session " + a.Session
	default:
		return "Running action", fmt.Sprintf("%T", action), "Action completed"
	}
}

func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func stringsReplaceCommand(template, command string) string {
	return strings.ReplaceAll(template, "{command}", command)
}
