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
		AttachArgv: []string{"mosh", "-p", "25300:25399", "me@asia"},
		Local:      Tmux{Runner: runner, InsideTmux: insideTmux},
	}
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
	if len(runner.calls) != 3 {
		t.Fatalf("calls = %d %q, want has-session, new-session, local attach", len(runner.calls), runner.calls)
	}
	for i, want := range []string{"has-session", "new-session"} {
		if runner.argv(i)[0] != "ssh" {
			t.Fatalf("call %d = %q, want an ssh invocation", i, runner.argv(i))
		}
		if !strings.Contains(strings.Join(runner.argv(i), " "), want) {
			t.Fatalf("call %d = %q, want it to %s", i, runner.argv(i), want)
		}
	}
	local := runner.argv(2)
	if local[0] != "tmux" || local[1] != "new-window" {
		t.Fatalf("third call = %q, want a local tmux window", local)
	}
	joined := strings.Join(local, " ")
	if !strings.Contains(joined, "mosh -p 25300:25399 me@asia") {
		t.Fatalf("third call = %q, want it to attach over mosh", local)
	}
	if !strings.Contains(joined, "tmux attach") {
		t.Fatalf("third call = %q, want it to attach to the remote session", local)
	}
}

// The local window name carries the host, so two hosts with the same branch do
// not produce two identically named windows.
func TestRemotePaneNamesLocalWindowWithHost(t *testing.T) {
	runner := &recordRunner{}
	if _, err := remoteFixture(runner, true).Pane(remoteResolved()); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	local := runner.argv(2)
	name := ""
	for i, arg := range local {
		if arg == "-n" && i+1 < len(local) {
			name = local[i+1]
		}
	}
	if name != "asia/topic" {
		t.Fatalf("local window name = %q, want asia/topic", name)
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

// mosh scans for its own options after the host, so a remote command whose first
// token starts with "-" makes it print usage and exit 255 — which looked exactly
// like "nothing happened", because tmux closes a window whose command dies. The
// remote command therefore travels as one argv element that starts with a word.
func TestRemotePaneSendsTheAttachCommandAsASingleWord(t *testing.T) {
	runner := &recordRunner{sessions: map[string]bool{"topic": true}}
	if _, err := remoteFixture(runner, true).Pane(remoteResolved()); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	local := runner.argv(len(runner.calls) - 1)
	last := local[len(local)-1]
	if !strings.HasPrefix(last, "tmux attach") {
		t.Fatalf("attach element = %q, want one element beginning with \"tmux attach\"", last)
	}
	for _, arg := range local {
		if strings.HasPrefix(arg, "-t=") {
			t.Fatalf("argv = %q, want no bare -t element for mosh to misread", local)
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
	for i := range runner.calls {
		if joined := strings.Join(runner.argv(i), " "); strings.Contains(joined, "new-session") {
			t.Fatalf("call %d = %q, want no new session when one already exists", i, runner.argv(i))
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
	local := runner.argv(len(runner.calls) - 1)
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
	joined := strings.Join(runner.argv(len(runner.calls)-1), " ")
	if !strings.Contains(joined, "ssh -A -t me@asia tmux attach") {
		t.Fatalf("argv = %q, want the host's ssh attach with the target appended", joined)
	}
}

// Attaching to a remote session must not create a window there: it opens a local
// window that connects to the session as it is.
func TestRemoteAttachOpensALocalWindowAndCreatesNothingRemote(t *testing.T) {
	runner := &recordRunner{sessions: map[string]bool{"gwt-monorepo-louis-x": true}}
	resolved := remoteResolved()
	resolved.Session = "gwt-monorepo-louis-x"
	out, err := remoteFixture(runner, true).Attach(resolved)
	if err != nil {
		t.Fatalf("Attach() error = %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d %q, want only the local window", len(runner.calls), runner.calls)
	}
	joined := strings.Join(runner.argv(0), " ")
	if !strings.HasPrefix(joined, "tmux new-window") {
		t.Fatalf("call = %q, want a local tmux window", runner.argv(0))
	}
	if strings.Contains(joined, "new-session") || strings.Contains(joined, "ssh -o") {
		t.Fatalf("call = %q, want nothing created on the far side", runner.argv(0))
	}
	if !strings.Contains(joined, "gwt-monorepo-louis-x") {
		t.Fatalf("call = %q, want it to attach to the existing session", runner.argv(0))
	}
	if !strings.Contains(out, "gwt-monorepo-louis-x") {
		t.Fatalf("message = %q", out)
	}
}
