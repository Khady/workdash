// Package launch executes resolved actions.
//
// Pane puts a command in a tmux window, which is the substrate for anything
// interactive: tmux owns the tabs, the scrollback, and the survival of the
// process, so workdash never has to hand a shell back to a caller or spawn a
// terminal emulator. Attach connects to a session that exists, Capture runs a
// command and returns its output, and Background starts a GUI program detached.
//
// A caller is not always inside tmux — the Firefox host is not — so where a window
// should appear is asked of tmux rather than assumed from $TMUX.
package launch

import (
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"syscall"

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
	// Background starts a command and forgets it. GUI programs need this: emacs
	// in a window stays in the foreground until closed, and a browser handed a URL
	// must never make the caller wait.
	Background(action.Resolved) (string, error)
}

type Tmux struct {
	Runner     Runner
	InsideTmux bool
	// Terminal spawns a terminal emulator, with the command to run appended. It is
	// used in exactly one case: there is no tmux client anywhere to show a window
	// in, so a detached session would be invisible and the printed "attach with…"
	// hint unusable — which is what a browser popup gets. Empty means do without.
	Terminal []string
	// Detach starts a command without waiting for it. Injected so the background
	// verb is testable without spawning real processes; nil means the real one.
	Detach func(cwd string, argv []string) error
}

func (t Tmux) Background(resolved action.Resolved) (string, error) {
	detach := t.Detach
	if detach == nil {
		detach = detachProcess
	}
	if err := detach(resolved.Cwd, resolved.Argv); err != nil {
		return "", err
	}
	return fmt.Sprintf("started %s in the background", resolved.ID), nil
}

// detachProcess puts the child in its own session so it survives this process and
// never shares its terminal — a GUI app has no business writing to the popup that
// launched it.
func detachProcess(cwd string, argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func (t Tmux) Pane(resolved action.Resolved) (string, error) {
	return t.pane(resolved, nil)
}

// pane creates the window and stamps tmux user options on it. The options record
// what the window *is* — which host, session and transport it shows — so the fact
// lives on the object rather than being encoded in a name for something to parse
// later. A collector can then report "this window is a view of asia:gwt-x".
// client is an attached tmux client: where the user is actually looking.
type client struct {
	TTY     string
	Session string
}

// attachedClient asks tmux which client is attached, most recently active first.
//
// This is what makes the browser useful. wd-host runs outside tmux, so deciding
// by $TMUX alone left every popup action in a detached session with a printed
// "attach with tmux attach -t …" — advice nobody can follow from a browser. tmux
// knows where the user is; ask it.
func (t Tmux) attachedClient() (client, bool) {
	out, err := t.Runner.Run("", []string{"tmux", "list-clients", "-F", "#{client_activity} #{client_session} #{client_tty}"})
	if err != nil {
		return client{}, false
	}
	best, bestActivity := client{}, int64(-1)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		activity, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		if activity > bestActivity {
			best, bestActivity = client{TTY: fields[2], Session: fields[1]}, activity
		}
	}
	return best, bestActivity >= 0
}

func (t Tmux) pane(resolved action.Resolved, options map[string]string) (string, error) {
	name := paneName(resolved)
	argv := commandArgv(resolved)

	// Where the window belongs: the workspace's own session when it has one, so
	// related work stays together rather than landing in whichever session you
	// happen to be attached to.
	target := resolved.Session

	// A session the snapshot recorded may be gone — the program it was opened for
	// was quit, taking its last window with it — and `new-window -t` on a name that
	// no longer exists fails. Forgetting it is what lets the same click work twice:
	// the window lands wherever a window with no session belongs, including a fresh
	// session of that name.
	if target != "" && !t.hasSession(target) {
		target = ""
	}

	// Where the user is looking. Inside tmux that is here, so no client has to be
	// moved. Outside it — the Firefox host, a cron job — ask tmux.
	viewer, hasViewer := client{}, false
	if !t.InsideTmux {
		viewer, hasViewer = t.attachedClient()
		if target == "" && hasViewer {
			// Nothing says where this belongs, so put it where it will be seen.
			target = viewer.Session
		}
	}

	if target == "" && !t.InsideTmux && !hasViewer {
		// Nowhere to show it: leave the work in a session and say how to reach it.
		// A session named for this workspace may already exist — a second action on
		// the same branch is routine — and creating it again fails with `duplicate
		// session`, so an existing one gets a window instead.
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
		return t.show(name)
	}

	window := []string{"tmux", "new-window"}
	if target != "" {
		window = append(window, "-t"+exactTarget(target)+":")
	}
	window = append(window, "-P", "-F", "#{window_id}", "-n", name, "-c", resolved.Cwd, "--")
	id, err := t.Runner.Run("", append(window, argv...))
	if err != nil {
		return "", err
	}
	t.stamp(strings.TrimSpace(id), options)

	// Always name the session. Suppressing it when the window went to the session
	// the client is already viewing looked like removing noise, but the caller that
	// most needs it is exactly the one that is not inside tmux: a browser popup
	// saying "opened tmux window" and nothing else does not say where to look.
	where := ""
	if target != "" {
		where = fmt.Sprintf(" in session %q", target)
	}
	// A window in a session the client is not viewing is still invisible, so move
	// the client to it.
	if hasViewer && target != "" && target != viewer.Session {
		if _, err := t.Runner.Run("", []string{"tmux", "switch-client", "-c", viewer.TTY, "-t" + exactTarget(target)}); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("opened tmux window %q%s", name, where), nil
}

// show makes a session visible when there is no client to put it in front of. A
// configured terminal is opened on it; without one, all that can be done is say
// where the work went.
func (t Tmux) show(session string) (string, error) {
	if len(t.Terminal) == 0 {
		return fmt.Sprintf("created detached session %q; attach with `tmux attach -t %s`", session, session), nil
	}
	detach := t.Detach
	if detach == nil {
		detach = detachProcess
	}
	argv := append(append([]string{}, t.Terminal...), "tmux", "attach", "-t"+exactTarget(session))
	// No working directory: the session already has one, and for a remote workspace
	// the path exists only on the other machine.
	if err := detach("", argv); err != nil {
		return "", fmt.Errorf("created session %q but could not open a terminal on it: %w", session, err)
	}
	return fmt.Sprintf("opened a terminal on session %q", session), nil
}

// stamp records what a window is, best effort: failing to label a window is not
// a reason to fail the action that created it.
func (t Tmux) stamp(windowID string, options map[string]string) {
	if windowID == "" || len(options) == 0 {
		return
	}
	for _, key := range sortedKeys(options) {
		_, _ = t.Runner.Run("", []string{"tmux", "set-option", "-w", "-t", windowID, key, options[key]})
	}
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (t Tmux) Attach(resolved action.Resolved) (string, error) {
	if resolved.Session == "" {
		return "", fmt.Errorf("nothing to attach to: %s has no session", resolved.Name)
	}
	// switch-client moves a client that already exists; attach would nest a session
	// inside itself when we are in tmux, and has nowhere to draw when we are not.
	call := []string{"tmux", "switch-client", "-t" + exactTarget(resolved.Session)}
	switch viewer, ok := t.attachedClient(); {
	case t.InsideTmux:
		// Our own client is the one to move.
	case ok:
		// Called from outside tmux — the Firefox host, a script — so name the
		// client that is actually on screen.
		call = []string{"tmux", "switch-client", "-c", viewer.TTY, "-t" + exactTarget(resolved.Session)}
	default:
		// No client anywhere: attaching is all that is left, and it needs a tty.
		call = []string{"tmux", "attach", "-t" + exactTarget(resolved.Session)}
	}
	if _, err := t.Runner.Run("", call); err != nil {
		if sessionGone(err) {
			return "", fmt.Errorf("nothing to attach to: session %q is gone", resolved.Session)
		}
		return "", err
	}
	return fmt.Sprintf("attached to session %q", resolved.Session), nil
}

// sessionGone reports whether a tmux failure means the session is not there any
// more, as opposed to tmux or the host being unreachable.
//
// The distinction matters because a session on a workspace is a snapshot: the
// work can have finished since it was taken — the last window closing ends the
// session, the last session ends the server — and tmux then answers "no server
// running", which read as a broken machine rather than as work that is over.
// Pane recreates what is gone; attach has nothing to connect to, so saying so is
// all it can do. Everything else must pass through untouched: an unreachable host
// reported as finished work would send you looking for a session that is still
// running.
//
// The message text is the only signal — tmux exits 1 for all of these — and a
// Runner that captures stderr folds it into the error, which is where ssh's
// relayed remote stderr arrives too.
func sessionGone(err error) bool {
	text := strings.ToLower(err.Error())
	for _, said := range []string{
		"no server running",   // the session took the last server with it
		"can't find session",  // the server outlived the session
		"no such session",     // older tmux, same thing
		"error connecting to", // the socket went with the server
	} {
		if strings.Contains(text, said) {
			return true
		}
	}
	return false
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

// exactTarget builds a tmux target that only matches this session.
//
// Not every tmux command accepts it: set-option resolves -t as a pane target and
// tmux 3.5a rejects "=name" there, reporting "no such session" for a session that
// exists. has-session, attach, new-session and new-window all take it. Without the
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
