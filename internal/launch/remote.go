package launch

import (
	"fmt"
	"strings"

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
		// -P -F prints the new window's id: a grouped view starts on the session's
		// first window, so the view has to be pointed at this one explicitly.
		create = []string{"tmux", "new-window", "-t" + exactTarget(name) + ":",
			"-P", "-F", "#{window_id}", "-c", resolved.Cwd, "--"}
		verb = "opened a window in remote session"
	}
	created, err := r.ssh(append(create, argv...))
	if err != nil {
		return "", err
	}
	// The local step's own report, not an assumption about it: with no tmux client
	// anywhere it creates a detached session, and claiming "attached locally" sent
	// you looking for a window that was never opened.
	local, err := r.view(resolved, name, strings.TrimSpace(created))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %q on %s; %s", verb, name, resolved.Host, local), nil
}

// Attach opens a local window connected to an existing remote session. Nothing is
// created on the far side — that is the difference from Pane.
func (r Remote) Attach(resolved action.Resolved) (string, error) {
	if resolved.Session == "" {
		return "", fmt.Errorf("nothing to attach to: %s has no session on %s", resolved.Name, resolved.Host)
	}
	// Attach has no window of its own, so it shows whichever the session is on.
	// Asking lets the view be named after it and clean itself up, like every other
	// view — the unnamed form left tmux to call it "<base>-1" and never reaped it.
	focus, err := r.currentWindow(resolved.Session)
	if err != nil {
		return "", err
	}
	shown, err := r.view(resolved, resolved.Session, focus)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("viewing session %q on %s over %s; %s",
		resolved.Session, resolved.Host, r.transportName(resolved), shown), nil
}

// prepareView creates the grouped view, points it at the window to show, and makes
// it reap itself when the last client detaches. Done over our own ssh call, where
// the quoting is ours to control, rather than as a command chain the transport
// would mangle.
func (r Remote) prepareView(session, focus string) error {
	view := viewName(session, focus)
	// -A so clicking the same action twice reuses one view instead of piling up.
	if _, err := r.ssh([]string{"tmux", "new-session", "-d", "-A",
		"-t" + exactTarget(session), "-s", view}); err != nil {
		return err
	}
	// No destroy-unattached: setting it on a view that has no client yet destroys
	// it on the spot, and the select-window below then cannot find it. It could only
	// be set from inside the attached client, which a plain attach cannot do — so
	// views persist after you detach. -A above bounds that: one view per window
	// viewed, reused on the next click rather than piling up.
	_, err := r.ssh([]string{"tmux", "select-window", "-t", view + ":" + focus})
	return err
}

// currentWindow reads which window a session is on.
func (r Remote) currentWindow(session string) (string, error) {
	out, err := r.ssh([]string{"tmux", "display-message", "-p",
		"-t" + exactTarget(session), "#{window_id}"})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// view opens a local window showing an existing remote session.
//
// It attaches to a *grouped* session rather than the session itself. Two clients
// of one session share its current window, so a second view would drag the first
// one along — open an editor and then a shell on the same branch and both local
// windows show the same remote window. A grouped session shares the windows with
// an independent current-window, which is what tmux provides them for.
func (r Remote) view(resolved action.Resolved, session, focus string) (string, error) {
	if focus != "" {
		if err := r.prepareView(session, focus); err != nil {
			return "", err
		}
	}
	local := action.Resolved{
		ID:   resolved.ID,
		Name: r.windowName(resolved),
		Argv: r.attachArgv(resolved, session, focus),
	}
	shown, err := r.Local.pane(local, map[string]string{
		"@wd_host":      resolved.Host,
		"@wd_session":   session,
		"@wd_transport": r.transportName(resolved),
	})
	if err != nil {
		return "", err
	}
	return shown, nil
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
func (r Remote) attachArgv(resolved action.Resolved, session, focus string) []string {
	transport := r.AttachArgv
	if len(resolved.Attach) > 0 {
		transport = resolved.Attach
	}
	reach := append(append([]string{}, transport...), r.Target)

	if focus == "" {
		// Nothing specific to show: new-session -t creates the grouped view and
		// attaches in one step, so a failed attach leaves nothing behind.
		return append(reach, "tmux", "new-session", "-t"+exactTarget(session))
	}
	// The view already exists and is pointed at the right window, so the
	// interactive command is a plain attach.
	//
	// It has to be plain. A tmux command chain cannot travel over both transports:
	// ssh joins its arguments and lets the remote shell re-parse them, so ";" is a
	// *shell* separator there and tmux never sees it — the rest of the chain runs
	// as shell commands and fails with "set-option: command not found". mosh
	// execvp's the argv, where ";" must be its own element. Preparing the view over
	// our own ssh call sidesteps the disagreement entirely.
	return append(reach, "tmux", "attach", "-t"+exactTarget(viewName(session, focus)))
}

// viewName names the grouped view after the window it is showing, so clicking the
// same action twice reuses one view rather than piling them up. tmux forbids "."
// and ":" in a session name; a window id like "@42" is otherwise fine.
func viewName(session, focus string) string {
	return session + "~" + strings.NewReplacer(".", "-", ":", "-").Replace(focus)
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

// sshText runs one command on the host, through a login shell.
//
// The login shell is not a nicety. sshd runs a command in a non-interactive,
// non-login shell, whose PATH on a Debian host is /usr/local/bin:/usr/bin:/bin —
// none of what the user installed under their home. That matters far beyond the
// command itself, because the tmux server keeps the PATH it was born with for its
// whole life: on tmux 3.5a a pane's PATH comes from the server process, and
// neither `set-environment -g PATH` nor `new-window -e PATH=...` overrides it. So
// a server created by a bare `ssh host tmux new-session` cripples every window
// opened in it afterwards, including ones opened by the host's own tmux
// bindings — `claude: command not found` from a binding that had worked for
// years, because the server behind it was no longer started by hand.
//
// A login shell is what an interactive ssh would have given it, so this restores
// the environment the host was set up to have. It costs about 10ms per call.
//
// The command is quoted for the remote shell: ssh joins its arguments with spaces
// and lets the remote shell re-parse them, so `bash -lc` would otherwise take
// only the first word as its command and the rest as positional parameters.
func (r Remote) sshText(command string) (string, error) {
	call := []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", r.Target,
		"bash", "-lc", shellquote.Quote(command)}
	return r.Runner.Run("", call)
}

func (r Remote) hasSession(name string) bool {
	_, err := r.ssh([]string{"tmux", "has-session", "-t" + exactTarget(name)})
	return err == nil
}
