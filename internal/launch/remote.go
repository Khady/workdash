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
	AttachArgv []string
	// Local creates the window that attaches to the remote session.
	Local Tmux
}

func (r Remote) Pane(resolved action.Resolved) (string, error) {
	// Prefer the session the workspace already has. Its name was chosen by
	// whatever created it — gwt, or a hand-run tmux — so inventing one from the
	// branch would create a second session on the same worktree and attach to the
	// empty one.
	name := paneName(resolved)
	if resolved.Session != "" {
		name = resolved.Session
	}
	argv := commandArgv(resolved)
	create := []string{"tmux", "new-session", "-d", "-s", name, "-c", resolved.Cwd, "--"}
	verb := "created remote session"
	if resolved.Session != "" || r.hasSession(name) {
		create = []string{"tmux", "new-window", "-t" + exactTarget(name) + ":", "-n", paneName(resolved), "-c", resolved.Cwd, "--"}
		verb = "opened a window in remote session"
	}
	if _, err := r.ssh(append(create, argv...)); err != nil {
		return "", err
	}
	// The remote command travels as ONE argv element beginning with a word.
	// mosh keeps scanning for its own options after the host, so a first token
	// starting with "-" makes it print usage and exit 255 — and because tmux
	// closes a window whose command dies, that failure looks like nothing
	// happening at all.
	attach := r.attachArgv(resolved, name)
	local := action.Resolved{
		ID:   resolved.ID,
		Name: resolved.Host + "/" + paneName(resolved),
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

// Attach opens a local window connected to an existing remote session. Nothing is
// created on the far side — that is the difference from Pane.
func (r Remote) Attach(resolved action.Resolved) (string, error) {
	if resolved.Session == "" {
		return "", fmt.Errorf("nothing to attach to: %s has no session on %s", resolved.Name, resolved.Host)
	}
	local := action.Resolved{
		ID:   resolved.ID,
		Name: resolved.Host + "/" + paneName(resolved),
		Argv: r.attachArgv(resolved, resolved.Session),
	}
	if _, err := r.Local.Pane(local); err != nil {
		return "", err
	}
	return fmt.Sprintf("attached to session %q on %s", resolved.Session, resolved.Host), nil
}

// attachArgv builds the local command that reaches an existing remote session.
//
// The remote part travels as ONE argv element beginning with a word: mosh keeps
// scanning for its own options after the host, so a first token starting with "-"
// makes it print usage and exit 255 — and since tmux closes a window whose command
// dies, that failure looks like nothing happening at all.
func (r Remote) attachArgv(resolved action.Resolved, session string) []string {
	transport := r.AttachArgv
	if len(resolved.Attach) > 0 {
		transport = resolved.Attach
	}
	return append(append([]string{}, transport...), r.Target,
		shellquote.Join([]string{"tmux", "attach", "-t" + exactTarget(session)}))
}

// Background is a local-desktop idea: the programs that need it are GUI apps on
// the machine in front of you. Rather than guess at something plausible over ssh,
// say what to do instead.
func (r Remote) Background(resolved action.Resolved) (string, error) {
	return "", fmt.Errorf("action %q cannot run in the background on %s: add `run_on = \"local\"` if it is a desktop program",
		resolved.ID, resolved.Host)
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
