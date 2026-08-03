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
		// No -n: tmux names the window after what is running and keeps that
		// current. Passing a name would pin it and disable automatic-rename, and
		// the branch is already the session's name — repeating it on every window
		// says nothing.
		create = []string{"tmux", "new-window", "-t" + exactTarget(name) + ":", "-c", resolved.Cwd, "--"}
		verb = "opened a window in remote session"
	}
	if _, err := r.ssh(append(create, argv...)); err != nil {
		return "", err
	}
	if _, err := r.view(resolved, name); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %q on %s and attached locally", verb, name, resolved.Host), nil
}

// Attach opens a local window connected to an existing remote session. Nothing is
// created on the far side — that is the difference from Pane.
func (r Remote) Attach(resolved action.Resolved) (string, error) {
	if resolved.Session == "" {
		return "", fmt.Errorf("nothing to attach to: %s has no session on %s", resolved.Name, resolved.Host)
	}
	return r.view(resolved, resolved.Session)
}

// view opens a local window showing an existing remote session.
//
// It attaches to a *grouped* session rather than the session itself. Two clients
// of one session share its current window, so a second view would drag the first
// one along — open an editor and then a shell on the same branch and both local
// windows show the same remote window. A grouped session shares the windows with
// an independent current-window, which is what tmux provides them for.
func (r Remote) view(resolved action.Resolved, session string) (string, error) {
	local := action.Resolved{
		ID:   resolved.ID,
		Name: r.windowName(resolved),
		Argv: r.attachArgv(resolved, session),
	}
	if _, err := r.Local.pane(local, map[string]string{
		"@wd_host":      resolved.Host,
		"@wd_session":   session,
		"@wd_transport": r.transportName(resolved),
	}); err != nil {
		return "", err
	}
	return fmt.Sprintf("attached to session %q on %s over %s",
		session, resolved.Host, r.transportName(resolved)), nil
}

// windowName says which host and branch the window shows, and names the transport
// only when it is not the default. ssh carries nearly everything, so labelling it
// everywhere is noise; mosh is the exception worth seeing, since it is the one
// without agent or port forwarding.
func (r Remote) windowName(resolved action.Resolved) string {
	where := resolved.Host + "/" + paneName(resolved)
	if transport := r.transportName(resolved); transport != defaultTransport {
		return transport + "·" + where
	}
	return where
}

// defaultTransport is the one that needs no announcing.
const defaultTransport = "ssh"

func (r Remote) transportName(resolved action.Resolved) string {
	transport := r.AttachArgv
	if len(resolved.Attach) > 0 {
		transport = resolved.Attach
	}
	if len(transport) == 0 {
		return defaultTransport
	}
	return transport[0]
}

// attachArgv builds the local command that reaches an existing remote session.
//
// The remote command is passed as SEPARATE argv elements, because the two
// transports disagree about what they receive:
//
//   - ssh joins its arguments with spaces and hands the result to the remote
//     shell, which re-parses it. Separate elements survive that.
//   - mosh execvp's the arguments directly. A single pre-joined string makes it
//     try to exec a program whose name contains spaces:
//     "mosh-server: execvp: tmux attach -t=master: No such file or directory".
//
// mosh does keep scanning for its own options up to the host, so a transport
// using it needs a "--" before the target — hence attach_argv ending in "--".
// Both failures are invisible without looking: tmux closes a window whose command
// dies, so a broken attach looks like nothing happening.
func (r Remote) attachArgv(resolved action.Resolved, session string) []string {
	transport := r.AttachArgv
	if len(resolved.Attach) > 0 {
		transport = resolved.Attach
	}
	// new-session -t creates the grouped view AND attaches in one step, so nothing
	// is left behind when the attach fails — a pre-created view session would
	// linger, since destroy-unattached only fires when a client detaches.
	return append(append(append([]string{}, transport...), r.Target),
		"tmux", "new-session", "-t"+exactTarget(session))
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
