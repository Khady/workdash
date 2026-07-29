package launch

import (
	"fmt"
	"github.com/Khady/workdash/internal/action"
	"github.com/Khady/workdash/internal/shellquote"
)

// Remote runs actions on another machine. Nothing of workdash lives there: the
// remote side is driven entirely through ssh and the tmux that is already
// installed, so there is no remote binary to deploy or keep in step.
//
// A remote pane is two steps. First the window is created on the far side, so
// the work survives losing the connection. Then a local tmux window attaches to
// it, which is what makes remote work appear in the same window list — and the
// same F-key switching — as everything local.
type Remote struct {
	Runner Runner
	Target string
	// Attach reaches the host interactively, ending in the ssh target. Defaults
	// to ssh with a tty; mosh is worth configuring for a host you keep
	// long-running agents on, because it survives a suspended laptop.
	Attach []string
	// Local creates the window that attaches to the remote session.
	Local Tmux
}

func (r Remote) Pane(resolved action.Resolved) (string, error) {
	name := paneName(resolved)
	argv := commandArgv(resolved)
	create := []string{"tmux", "new-session", "-d", "-s", name, "-c", resolved.Cwd, "--"}
	verb := "created remote session"
	if r.hasSession(name) {
		create = []string{"tmux", "new-window", "-t" + exactTarget(name) + ":", "-n", name, "-c", resolved.Cwd, "--"}
		verb = "opened a window in remote session"
	}
	if _, err := r.ssh(append(create, argv...)); err != nil {
		return "", err
	}
	attach := append(append([]string{}, r.Attach...), "tmux", "attach", "-t"+exactTarget(name))
	local := action.Resolved{
		ID:   resolved.ID,
		Name: resolved.Host + "/" + name,
		Argv: attach,
		// The attaching window has no meaningful local directory: its cwd lives
		// on the other machine.
		Cwd: "",
	}
	if _, err := r.Local.Pane(local); err != nil {
		return "", fmt.Errorf("%s %q on %s, but attaching locally failed: %w", verb, name, resolved.Host, err)
	}
	return fmt.Sprintf("%s %q on %s and attached locally", verb, name, resolved.Host), nil
}

func (r Remote) Capture(resolved action.Resolved) (string, error) {
	command := shellquote.Join(commandArgv(resolved))
	if resolved.Cwd != "" {
		command = "cd -- " + shellquote.Quote(resolved.Cwd) + " && " + command
	}
	return r.sshText(command)
}

// ssh sends one command to the host.
//
// Each argument is quoted individually and the result joined with spaces, which
// is exactly what the remote shell will re-parse. Quoting the joined string as a
// whole instead would make the far side see a single word and try to execute a
// command with spaces in its name.
func (r Remote) ssh(argv []string) (string, error) {
	return r.sshText(shellquote.Join(argv))
}

func (r Remote) sshText(command string) (string, error) {
	call := []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", r.Target, command}
	return r.Runner.Run("", call)
}

func (r Remote) hasSession(name string) bool {
	_, err := r.ssh([]string{"tmux", "has-session", "-t" + exactTarget(name)})
	return err == nil
}
