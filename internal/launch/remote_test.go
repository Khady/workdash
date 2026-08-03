package launch

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/Khady/workdash/internal/action"
)

func remoteFixture(runner Runner, insideTmux bool) Remote {
	return Remote{
		Runner:     runner,
		Target:     "me@asia",
		AttachArgv: []string{"mosh", "-p", "25300:25399", "--"},
		Local:      Tmux{Runner: runner, InsideTmux: insideTmux},
	}
}

// localWindow finds the local tmux new-window call. The last call is no longer it:
// the window gets labelled with set-option afterwards.
func localWindow(t *testing.T, runner *recordRunner) []string {
	t.Helper()
	for i := range runner.calls {
		argv := runner.argv(i)
		if len(argv) > 1 && argv[0] == "tmux" && argv[1] == "new-window" {
			return argv
		}
	}
	t.Fatalf("no local tmux new-window in %q", runner.calls)
	return nil
}

// remoteCommands returns the command text sent over ssh, one per call.
func remoteCommands(runner *recordRunner) []string {
	out := []string{}
	for i := range runner.calls {
		argv := runner.argv(i)
		if len(argv) > 0 && argv[0] == "ssh" {
			out = append(out, argv[len(argv)-1])
		}
	}
	return out
}

func remoteResolved() action.Resolved {
	return action.Resolved{ID: "agent", Name: "topic", Host: "asia",
		Argv: []string{"claude"}, Cwd: "/srv/wt/topic"}
}

// A remote pane is two steps: make the window on the far side, then attach to it
// from a local tmux window so remote work shows up in local window switching.
func TestRemotePaneCreatesRemoteSessionThenAttachesLocally(t *testing.T) {
	runner := &recordRunner{}
	if _, err := remoteFixture(runner, true).Pane(remoteResolved()); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	commands := strings.Join(remoteCommands(runner), "\n")
	for _, want := range []string{"has-session", "new-session -d -s topic"} {
		if !strings.Contains(commands, want) {
			t.Fatalf("remote commands:\n%s\nwant one to %s", commands, want)
		}
	}
	local := localWindow(t, runner)
	if local[0] != "tmux" || local[1] != "new-window" {
		t.Fatalf("third call = %q, want a local tmux window", local)
	}
	joined := strings.Join(local, " ")
	if !strings.Contains(joined, "mosh -p 25300:25399 -- me@asia") {
		t.Fatalf("third call = %q, want it to attach over mosh", local)
	}
	if !strings.Contains(joined, "tmux attach -t=topic~") {
		t.Fatalf("third call = %q, want it to attach to a view of the remote session", local)
	}
}

// The local window name carries the host, so two hosts with the same branch do
// not produce two identically named windows.
func TestRemotePaneNamesLocalWindowWithHost(t *testing.T) {
	runner := &recordRunner{}
	if _, err := remoteFixture(runner, true).Pane(remoteResolved()); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	local := localWindow(t, runner)
	name := ""
	for i, arg := range local {
		if arg == "-n" && i+1 < len(local) {
			name = local[i+1]
		}
	}
	// The transport is part of the name: without it there is no way to tell a mosh
	// view from an ssh one by looking at your window list.
	if name != "mosh·asia/topic" {
		t.Fatalf("local window name = %q, want mosh·asia/topic", name)
	}
}

func TestRemotePaneAddsWindowWhenRemoteSessionExists(t *testing.T) {
	runner := &recordRunner{sessions: map[string]bool{"topic": true}}
	if _, err := remoteFixture(runner, true).Pane(remoteResolved()); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	second := strings.Join(runner.argv(1), " ")
	if !strings.Contains(second, "new-window") {
		t.Fatalf("second call = %q, want new-window on the existing remote session", runner.argv(1))
	}
}

// Same trap as the collector: ssh joins its command arguments with spaces and
// the remote shell re-parses them, so anything sent must survive that.
func TestRemoteCaptureSurvivesRemoteReparsing(t *testing.T) {
	runner := &recordRunner{}
	resolved := action.Resolved{ID: "echo", Name: "topic", Host: "asia",
		Argv: []string{"printf", "%s", "two words"}, Cwd: "/tmp"}
	if _, err := remoteFixture(runner, false).Capture(resolved); err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	argv := runner.argv(0)
	if argv[0] != "ssh" {
		t.Fatalf("argv = %q, want ssh", argv)
	}
	// ssh sends the last element verbatim for the remote shell to parse.
	remote := argv[len(argv)-1]
	out, err := exec.Command("bash", "-c", remote).Output()
	if err != nil {
		t.Fatalf("remote command %q failed: %v", remote, err)
	}
	if string(out) != "two words" {
		t.Fatalf("remote command produced %q, want \"two words\"", string(out))
	}
}

func TestRemoteCaptureRunsInTheWorkspaceDirectory(t *testing.T) {
	runner := &recordRunner{}
	resolved := action.Resolved{ID: "pwd", Name: "topic", Host: "asia",
		Argv: []string{"pwd"}, Cwd: "/srv/wt/topic"}
	if _, err := remoteFixture(runner, false).Capture(resolved); err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	if !strings.Contains(strings.Join(runner.argv(0), " "), "/srv/wt/topic") {
		t.Fatalf("argv = %q, want it to cd into the workspace", runner.argv(0))
	}
}

func TestRemotePaneReportsFailureToReachTheHost(t *testing.T) {
	runner := &recordRunner{err: errors.New("ssh: Network is unreachable")}
	if _, err := remoteFixture(runner, true).Pane(remoteResolved()); err == nil {
		t.Fatal("Pane() error = nil, want the ssh failure")
	}
}

// The two transports disagree about the remote command: ssh re-parses it in the
// remote shell, mosh execvp's it. Separate argv elements are the only form both
// accept — a pre-joined string made mosh report
// "execvp: tmux attach -t=master: No such file or directory" and the window died
// silently.
func TestRemotePaneSendsTheAttachCommandAsSeparateArguments(t *testing.T) {
	runner := &recordRunner{sessions: map[string]bool{"topic": true}, out: "topic-1"}
	if _, err := remoteFixture(runner, true).Pane(remoteResolved()); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	local := localWindow(t, runner)
	// The transport, the target and then tmux's own words, each its own element:
	// mosh execvp's the argv, so a pre-joined string becomes a program name with
	// spaces in it.
	for i, arg := range local {
		if arg != "me@asia" {
			continue
		}
		wantArgv(t, local[i:i+3], []string{"me@asia", "tmux", "attach"})
		break
	}
	for _, arg := range local {
		if strings.Contains(arg, " ") {
			t.Fatalf("argv element %q contains a space: mosh execvp's these", arg)
		}
	}
}

// The workspace already has a session, created by gwt with a name workdash would
// never pick. Attaching must land in that one, not in a second session named
// after the branch.
func TestRemotePaneReusesTheWorkspacesExistingSession(t *testing.T) {
	runner := &recordRunner{sessions: map[string]bool{"gwt-monorepo-louis-x": true}}
	resolved := remoteResolved()
	resolved.Session = "gwt-monorepo-louis-x"
	out, err := remoteFixture(runner, true).Pane(resolved)
	if err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	// A grouped view session (new-session -t) is expected; what must not happen is
	// creating a second *named* session for the same worktree.
	// The grouped view is a new-session too, so what must not appear is one that
	// creates a base session: a new-session with a name and no group target.
	for _, command := range remoteCommands(runner) {
		if strings.Contains(command, "new-session") && !strings.Contains(command, "-t=") {
			t.Fatalf("remote command %q created a second base session", command)
		}
	}
	all := ""
	for i := range runner.calls {
		all += strings.Join(runner.argv(i), " ") + "\n"
	}
	if !strings.Contains(all, "gwt-monorepo-louis-x") {
		t.Fatalf("calls:\n%s\nwant the existing session targeted", all)
	}
	if strings.Contains(all, "-s topic") {
		t.Fatalf("calls:\n%s\nwant no session named after the branch", all)
	}
	if !strings.Contains(out, "gwt-monorepo-louis-x") {
		t.Fatalf("message = %q, want it to name where the command landed", out)
	}
}

// mosh breaks agent forwarding and port forwarding, so it must be selectable per
// action rather than per host: vim and emacs want it, a shell does not.
func TestRemotePaneUsesTheActionsOwnAttachWhenGiven(t *testing.T) {
	runner := &recordRunner{sessions: map[string]bool{"topic": true}}
	remote := Remote{Runner: runner, Target: "me@asia",
		AttachArgv: []string{"ssh", "-A", "-t"}, Local: Tmux{Runner: runner, InsideTmux: true}}
	resolved := remoteResolved()
	resolved.Attach = []string{"mosh", "-p", "25300:25399", "--"}
	if _, err := remote.Pane(resolved); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	local := localWindow(t, runner)
	joined := strings.Join(local, " ")
	if !strings.Contains(joined, "mosh -p 25300:25399 -- me@asia tmux attach") {
		t.Fatalf("argv = %q, want mosh with the target after --", local)
	}
	if strings.Contains(joined, "ssh -A") {
		t.Fatalf("argv = %q, want the host default overridden", local)
	}
}

func TestRemotePaneFallsBackToTheHostAttach(t *testing.T) {
	runner := &recordRunner{sessions: map[string]bool{"topic": true}}
	remote := Remote{Runner: runner, Target: "me@asia",
		AttachArgv: []string{"ssh", "-A", "-t"}, Local: Tmux{Runner: runner, InsideTmux: true}}
	if _, err := remote.Pane(remoteResolved()); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	joined := strings.Join(localWindow(t, runner), " ")
	if !strings.Contains(joined, "ssh -A -t me@asia tmux attach") {
		t.Fatalf("argv = %q, want the host's ssh attach with the target appended", joined)
	}
}

// Attaching to a remote session must not create a window there: it opens a local
// window that connects to the session as it is.
func TestRemoteAttachAddsNoWindowToTheRemoteSession(t *testing.T) {
	runner := &recordRunner{sessions: map[string]bool{"gwt-monorepo-louis-x": true}}
	resolved := remoteResolved()
	resolved.Session = "gwt-monorepo-louis-x"
	out, err := remoteFixture(runner, true).Attach(resolved)
	if err != nil {
		t.Fatalf("Attach() error = %v", err)
	}
	// Attaching creates a grouped view session, which self-destructs — but it must
	// not add a window to the work, which is what separates Attach from Pane.
	for _, command := range remoteCommands(runner) {
		if strings.Contains(command, "new-window") {
			t.Fatalf("remote command %q added a window; attach must create no work", command)
		}
		if strings.Contains(command, "new-session") && !strings.Contains(command, "-t=") {
			t.Fatalf("remote command %q created a base session", command)
		}
	}
	// Attach makes no ssh calls at all now: the view is created by the command
	// inside the local window, so the session is named there.
	if joined := strings.Join(localWindow(t, runner), " "); !strings.Contains(joined, "-t=gwt-monorepo-louis-x") {
		t.Fatalf("local window = %q, want it to view the existing session", joined)
	}
	if !strings.Contains(out, "gwt-monorepo-louis-x") || !strings.Contains(out, "mosh") {
		t.Fatalf("message = %q, want the session viewed and the transport", out)
	}
}

// Attaching twice to one session makes two clients of it, and tmux clients on a
// session share the current window — so opening vim and then a shell on the same
// branch gave two local windows showing the same remote window, moving together.
// A grouped session (new-session -t) shares the windows with an independent
// current-window, which is tmux's own idiom for two views of one set of windows.
func TestRemoteAttachViewsThroughAGroupedSession(t *testing.T) {
	runner := &recordRunner{sessions: map[string]bool{"gwt-x": true}, out: "gwt-x-1"}
	resolved := remoteResolved()
	resolved.Session = "gwt-x"
	if _, err := remoteFixture(runner, true).Attach(resolved); err != nil {
		t.Fatalf("Attach() error = %v", err)
	}
	joined := strings.Join(localWindow(t, runner), " ")
	if !strings.Contains(strings.Join(remoteCommands(runner), "\n"), "new-session -d -A '-t=gwt-x'") {
		t.Fatalf("remote commands %q, want a grouped view of gwt-x", remoteCommands(runner))
	}
	if !strings.Contains(joined, "attach -t=gwt-x~") {
		t.Fatalf("attach = %q, want it to attach to the view, not the base session", joined)
	}
	// The view is created by the command inside the local window, so a connection
	// that never succeeds leaves nothing behind on the host.
	for _, command := range remoteCommands(runner) {
		if strings.Contains(command, "new-window") {
			t.Fatalf("remote command %q added a window to the work", command)
		}
	}
}

// The view session must clean itself up, or every editor you open leaves a
// session behind on the host.
func TestRemoteAttachCreatesNothingBeforeAttaching(t *testing.T) {
	runner := &recordRunner{sessions: map[string]bool{"gwt-x": true}, out: "gwt-x-1"}
	resolved := remoteResolved()
	resolved.Session = "gwt-x"
	if _, err := remoteFixture(runner, true).Attach(resolved); err != nil {
		t.Fatalf("Attach() error = %v", err)
	}
	all := ""
	for i := range runner.calls {
		all += strings.Join(runner.argv(i), " ") + "\n"
	}
	for _, command := range remoteCommands(runner) {
		if strings.Contains(command, "new-window") {
			t.Fatalf("remote command %q added a window before attaching", command)
		}
	}
}

// The remote window gets no name at all: tmux names it after what is running and
// keeps that current, which is what the sessions gwt creates already look like
// (bash, yclaude, notes). Passing -n would pin the name AND disable
// automatic-rename, so a window opened for bash would still say "bash" after you
// started vim in it. The branch is already the session's name, so repeating it on
// every window says nothing.
func TestRemotePaneLetsTmuxNameTheRemoteWindow(t *testing.T) {
	runner := &recordRunner{sessions: map[string]bool{"gwt-x": true}}
	resolved := remoteResolved()
	resolved.Session = "gwt-x"
	if _, err := remoteFixture(runner, true).Pane(resolved); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	create := remoteCommands(runner)[0]
	if !strings.Contains(create, "new-window") {
		t.Fatalf("remote command = %q, want a new window", create)
	}
	if strings.Contains(create, " -n ") {
		t.Fatalf("remote command = %q, want no -n: it pins the name and disables automatic-rename", create)
	}
}

// The transport is shown only when it is not the default. ssh carries nearly
// everything, so naming it on every window is noise; mosh is the exception worth
// seeing, because it is the one without agent or port forwarding.
func TestRemoteWindowNameOmitsSSHAndShowsMosh(t *testing.T) {
	runner := &recordRunner{sessions: map[string]bool{"gwt-x": true}}
	overSSH := Remote{Runner: runner, Target: "me@asia",
		AttachArgv: []string{"ssh", "-A", "-t"}, Local: Tmux{Runner: runner, InsideTmux: true}}
	resolved := remoteResolved()
	resolved.Session = "gwt-x"
	if got := overSSH.windowName(resolved); got != "asia/topic" {
		t.Fatalf("ssh window name = %q, want asia/topic with no transport", got)
	}
	overMosh := resolved
	overMosh.Attach = []string{"mosh", "-p", "25300:25399", "--"}
	if got := overSSH.windowName(overMosh); got != "mosh·asia/topic" {
		t.Fatalf("mosh window name = %q, want the transport shown", got)
	}
}

// new-window makes the window current in the BASE session, but a grouped view has
// its own current-window and starts on the session's first window — so opening vim
// in a session that already had windows attached you to the wrong one. The view has
// to be pointed at the window that was just created.
func TestRemotePaneFocusesTheWindowItCreated(t *testing.T) {
	runner := &recordRunner{sessions: map[string]bool{"gwt-x": true}, out: "@42"}
	resolved := remoteResolved()
	resolved.Session = "gwt-x"
	if _, err := remoteFixture(runner, true).Pane(resolved); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	create := remoteCommands(runner)[0]
	if !strings.Contains(create, "#{window_id}") {
		t.Fatalf("create = %q, want the new window's id printed back", create)
	}
	commands := strings.Join(remoteCommands(runner), "\n")
	if !strings.Contains(commands, "select-window") || !strings.Contains(commands, "@42") {
		t.Fatalf("remote commands:\n%s\nwant the created window selected in the view", commands)
	}
	// The interactive command must be a plain attach: a tmux command chain cannot
	// survive both transports, since ssh gives ";" to the remote shell.
	local := strings.Join(localWindow(t, runner), " ")
	if strings.Contains(local, ";") {
		t.Fatalf("view = %q, want no command chain: ssh would let the remote shell eat it", local)
	}
	if !strings.Contains(local, "tmux attach -t=") {
		t.Fatalf("view = %q, want a plain attach", local)
	}
}

// Every view should be named after the window it shows and destroy itself on
// detach. The unnamed form let tmux auto-name it "<base>-1", "-2", … and left it
// behind: clutter in your session list and an extra session on the workspace in
// wd ls.
func TestRemoteAttachNamesItsViewAfterTheWindow(t *testing.T) {
	runner := &recordRunner{sessions: map[string]bool{"gwt-x": true}, out: "@7"}
	resolved := remoteResolved()
	resolved.Session = "gwt-x"
	if _, err := remoteFixture(runner, true).Attach(resolved); err != nil {
		t.Fatalf("Attach() error = %v", err)
	}
	commands := strings.Join(remoteCommands(runner), "\n")
	if !strings.Contains(commands, "-s 'gwt-x~@7'") && !strings.Contains(commands, "-s gwt-x~@7") {
		t.Fatalf("remote commands:\n%s\nwant the view named after the window it shows", commands)
	}
	// No destroy-unattached: on a view with no client yet it destroys the session
	// immediately, and the select-window that follows cannot find it.
	if strings.Contains(commands, "destroy-unattached") {
		t.Fatalf("remote commands:\n%s\nwant no destroy-unattached: it reaps the view before it is used", commands)
	}
	// It must ask which window the session is on, since attach has no window of
	// its own to focus.
	if !strings.Contains(remoteCommands(runner)[0], "window_id") {
		t.Fatalf("first remote command = %q, want it to read the session's current window",
			remoteCommands(runner)[0])
	}
}

// The message has to say what happened, not what was hoped for. With no client
// anywhere the local step creates a detached session, and reporting "attached
// locally" sent you looking for a window that was never opened.
func TestRemotePaneReportsWhatTheLocalStepActuallyDid(t *testing.T) {
	runner := &recordRunner{sessions: map[string]bool{"gwt-x": true}, out: "@3"}
	remote := Remote{Runner: runner, Target: "me@asia", AttachArgv: []string{"ssh", "-A", "-t"},
		Local: Tmux{Runner: runner, InsideTmux: false}}
	resolved := remoteResolved()
	resolved.Session = "gwt-x"
	out, err := remote.Pane(resolved)
	if err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	if strings.Contains(out, "attached locally") {
		t.Fatalf("message = %q, want it not to claim an attach that did not happen", out)
	}
	if !strings.Contains(out, "tmux attach") {
		t.Fatalf("message = %q, want it to carry the local step's own report", out)
	}
}
