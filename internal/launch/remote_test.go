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
		Runner: runner,
		Target: "me@asia",
		Attach: []string{"mosh", "-p", "25300:25399", "me@asia"},
		Local:  Tmux{Runner: runner, InsideTmux: insideTmux},
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
