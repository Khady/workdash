package actions

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/Khady/workdash/internal/model"
)

const RelaunchAlwaysMarker = "# workdash:relaunch=always"

type RelaunchPolicy string

const (
	RelaunchAlways RelaunchPolicy = "always"
	RelaunchNever  RelaunchPolicy = "never"
)

func Quote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func WrapRemoteCommand(sshTarget, command string, interactive bool) string {
	remote := Quote("bash -lc " + Quote(command))
	if interactive {
		return fmt.Sprintf("ssh -A -t %s %s", Quote(sshTarget), remote)
	}
	return fmt.Sprintf("ssh -A -o BatchMode=yes -o ConnectTimeout=5 -o StrictHostKeyChecking=yes %s %s", Quote(sshTarget), remote)
}

type CdAction struct{ Path string }

func (a CdAction) ToShell() string { return "cd -- " + Quote(a.Path) }

type RemoteShellAction struct {
	Path      string
	SSHTarget string
}

func (a RemoteShellAction) ToShell() string {
	return WrapRemoteCommand(a.SSHTarget, fmt.Sprintf("cd -- %s && exec \"${SHELL:-bash}\"", Quote(a.Path)), true)
}

type CheckoutAction struct {
	RepoRoot  string
	Branch    string
	SSHTarget string
}

func (a CheckoutAction) ToShell() string {
	if a.SSHTarget == "" {
		return fmt.Sprintf("cd -- %s && git checkout %s", Quote(a.RepoRoot), Quote(a.Branch))
	}
	cmd := fmt.Sprintf("cd -- %s && git checkout %s && exec \"${SHELL:-bash}\"", Quote(a.RepoRoot), Quote(a.Branch))
	return WrapRemoteCommand(a.SSHTarget, cmd, true)
}

type OpenInBrowserAction struct{ URL string }

func (a OpenInBrowserAction) ToShell() string {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return opener + " " + Quote(a.URL)
}

type EditConfigAction struct {
	Path   string
	Editor string
}

func (a EditConfigAction) ToShell() string {
	p := Quote(a.Path)
	if a.Editor == "" {
		return `if [ -z "${EDITOR:-}" ]; then echo "Set $EDITOR or configure ` + "`config_editor`" + ` in workdash" >&2; exit 1; fi; exec $EDITOR -- ` + p
	}
	if strings.Contains(a.Editor, "{path}") {
		return "exec " + strings.ReplaceAll(a.Editor, "{path}", p)
	}
	return fmt.Sprintf("exec %s -- %s", a.Editor, p)
}

type DeleteWorktreeAction struct {
	RepoRoot     string
	WorktreePath string
	SSHTarget    string
}

func (a DeleteWorktreeAction) ToShell() string {
	cmd := fmt.Sprintf("cd -- %s && git worktree remove --force %s", Quote(a.RepoRoot), Quote(a.WorktreePath))
	if a.SSHTarget == "" {
		return cmd
	}
	return WrapRemoteCommand(a.SSHTarget, cmd, false)
}

type TmuxAction struct {
	Session    string
	InsideTmux bool
	SSHTarget  string
}

func (a TmuxAction) ToShell() string {
	if a.SSHTarget != "" {
		return WrapRemoteCommand(a.SSHTarget, "tmux attach-session -t "+Quote(a.Session), true)
	}
	cmd := "attach-session"
	if a.InsideTmux {
		cmd = "switch-client"
	}
	return fmt.Sprintf("tmux %s -t %s", cmd, Quote(a.Session))
}

type KillTmuxSessionAction struct {
	Session   string
	SSHTarget string
}

func (a KillTmuxSessionAction) ToShell() string {
	cmd := "tmux kill-session -t " + Quote(a.Session)
	if a.SSHTarget == "" {
		return cmd
	}
	return WrapRemoteCommand(a.SSHTarget, cmd, false)
}

type NoopAction struct{}

func (NoopAction) ToShell() string { return "" }

type TerminalLaunchAction struct{ Action model.ShellAction }

func (a TerminalLaunchAction) ToShell() string { return a.Action.ToShell() }

func RelaunchPolicyFor(action model.ShellAction) RelaunchPolicy {
	switch a := action.(type) {
	case TerminalLaunchAction:
		return RelaunchPolicyFor(a.Action)
	case ConfiguredShellAction:
		if a.Relaunch == "always" {
			return RelaunchAlways
		}
	case RemoteShellAction, TmuxAction:
		return RelaunchAlways
	case CheckoutAction:
		if a.SSHTarget != "" {
			return RelaunchAlways
		}
	}
	return RelaunchNever
}

func TerminalLaunchCommand(action model.ShellAction) string {
	switch a := action.(type) {
	case TerminalLaunchAction:
		return TerminalLaunchCommand(a.Action)
	case ConfiguredShellAction:
		return a.ToShell()
	case CdAction:
		return fmt.Sprintf("cd -- %s && exec \"${SHELL:-bash}\"", Quote(a.Path))
	case EditConfigAction:
		return a.ToShell()
	case RemoteShellAction:
		return a.ToShell()
	case TmuxAction:
		if a.SSHTarget != "" {
			return a.ToShell()
		}
		return "tmux attach-session -t " + Quote(a.Session)
	default:
		return ""
	}
}

func SerializeEmittedAction(action model.ShellAction) string {
	if action == nil {
		return ""
	}
	shell := action.ToShell()
	if shell == "" {
		return ""
	}
	if RelaunchPolicyFor(action) == RelaunchAlways {
		return RelaunchAlwaysMarker + "\n" + shell
	}
	return shell
}
