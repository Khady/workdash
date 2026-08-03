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
	// has-session, new-session, the local window, then one set-option per label.
	if len(runner.calls) != 6 {
		t.Fatalf("calls = %d %q, want the full create-then-view sequence", len(runner.calls), runner.calls)
	}
	for i, want := range []string{"has-session", "new-session"} {
		if runner.argv(i)[0] != "ssh" {
			t.Fatalf("call %d = %q, want an ssh invocation", i, runner.argv(i))
		}
		if !strings.Contains(strings.Join(runner.argv(i), " "), want) {
			t.Fatalf("call %d = %q, want it to %s", i, runner.argv(i), want)
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
	if !strings.Contains(joined, "tmux new-session -t=") {
		t.Fatalf("third call = %q, want it to view the remote session", local)
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
	tail := local[len(local)-4:]
	wantArgv(t, tail, []string{"me@asia", "tmux", "new-session", "-t=topic"})
	for _, arg := range local {
		if strings.Contains(arg, "tmux attach") {
			t.Fatalf("argv = %q, want no pre-joined remote command: mosh execvp's it", local)
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
	for _, command := range remoteCommands(runner) {
		if strings.Contains(command, "new-session") && strings.Contains(command, " -s ") {
			t.Fatalf("remote command %q created a second named session", command)
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
	if !strings.Contains(joined, "mosh -p 25300:25399 -- me@asia tmux new-session") {
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
	if !strings.Contains(joined, "ssh -A -t me@asia tmux new-session") {
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
		if strings.Contains(command, "new-session") && strings.Contains(command, " -s ") {
			t.Fatalf("remote command %q created a named session", command)
		}
	}
	if !strings.Contains(strings.Join(remoteCommands(runner), "\n"), "gwt-monorepo-louis-x") {
		t.Fatalf("remote commands %q, want the existing session viewed", remoteCommands(runner))
	}
	if !strings.Contains(out, "gwt-monorepo-louis-x") {
		t.Fatalf("message = %q", out)
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
	if !strings.Contains(joined, "tmux new-session -t=gwt-x") {
		t.Fatalf("attach = %q, want new-session -t: it creates the grouped view and attaches in one step", joined)
	}
	if strings.Contains(joined, "tmux attach") {
		t.Fatalf("attach = %q, want no plain attach: that shares the base session's current window", joined)
	}
	// Nothing may be created before the attach, or a failed attach leaves a view
	// session behind on the host.
	for _, command := range remoteCommands(runner) {
		if strings.Contains(command, "new-session") {
			t.Fatalf("remote command %q pre-created a session; the attach itself must create the view", command)
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
	if strings.Contains(all, "destroy-unattached") {
		t.Fatalf("calls:\n%s\nwant no pre-created session to have to clean up", all)
	}
}
