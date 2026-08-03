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
	"os/exec"
	"sort"
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
func (t Tmux) pane(resolved action.Resolved, options map[string]string) (string, error) {
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
		call := append(append([]string{"tmux", "new-window"}, target...),
			"-P", "-F", "#{window_id}", "-n", name, "-c", resolved.Cwd, "--")
		call = append(call, argv...)
		id, err := t.Runner.Run("", call)
		if err != nil {
			return "", err
		}
		t.stamp(strings.TrimSpace(id), options)
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
