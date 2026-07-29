package dispatch

import (
	"errors"
	"testing"

	"github.com/Khady/workdash/internal/action"
	"github.com/Khady/workdash/internal/launch"
	"github.com/Khady/workdash/internal/wdconf"
)

type fakeExecutor struct {
	pane       []action.Resolved
	capture    []action.Resolved
	attach     []action.Resolved
	background []action.Resolved
	err        error
}

func (f *fakeExecutor) Background(resolved action.Resolved) (string, error) {
	f.background = append(f.background, resolved)
	return "backgrounded", f.err
}

func (f *fakeExecutor) Attach(resolved action.Resolved) (string, error) {
	f.attach = append(f.attach, resolved)
	return "attached", f.err
}

func (f *fakeExecutor) Pane(resolved action.Resolved) (string, error) {
	f.pane = append(f.pane, resolved)
	return "paned", f.err
}

func (f *fakeExecutor) Capture(resolved action.Resolved) (string, error) {
	f.capture = append(f.capture, resolved)
	return "captured", f.err
}

func TestExecuteRoutesPaneAndCaptureToTheRightVerb(t *testing.T) {
	executor := &fakeExecutor{}
	if _, err := Execute(executor, action.Resolved{ID: "agent", Where: wdconf.WherePane}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if _, err := Execute(executor, action.Resolved{ID: "status", Where: wdconf.WhereCapture}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(executor.pane) != 1 || executor.pane[0].ID != "agent" {
		t.Fatalf("pane calls = %+v, want one for agent", executor.pane)
	}
	if len(executor.capture) != 1 || executor.capture[0].ID != "status" {
		t.Fatalf("capture calls = %+v, want one for status", executor.capture)
	}
}

// Attach is its own verb, and routing it as a pane would silently launch a
// second copy of the work instead of taking you to the one already running.
func TestExecuteRoutesAttachToTheAttachVerb(t *testing.T) {
	executor := &fakeExecutor{}
	if _, err := Execute(executor, action.Resolved{ID: "attach", Where: wdconf.WhereAttach, Session: "gwt-x"}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(executor.attach) != 1 || executor.attach[0].Session != "gwt-x" {
		t.Fatalf("attach calls = %+v, want one for the existing session", executor.attach)
	}
	if len(executor.pane) != 0 {
		t.Fatalf("pane calls = %+v, want none: attaching creates nothing", executor.pane)
	}
}

func TestExecuteReturnsTheExecutorOutput(t *testing.T) {
	executor := &fakeExecutor{}
	out, err := Execute(executor, action.Resolved{ID: "status", Where: wdconf.WhereCapture})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if out != "captured" {
		t.Fatalf("Execute() output = %q, want the captured output passed through", out)
	}
}

func TestExecuteReturnsExecutorFailure(t *testing.T) {
	executor := &fakeExecutor{err: errors.New("no server running")}
	if _, err := Execute(executor, action.Resolved{ID: "agent", Where: wdconf.WherePane}); err == nil {
		t.Fatal("Execute() error = nil, want the executor failure")
	}
}

func TestExecutorForPicksLocalOrRemoteByHost(t *testing.T) {
	cfg := wdconf.Config{Hosts: []wdconf.Host{
		{Label: "asia", SSHTarget: "me@asia", AttachArgv: []string{"mosh", "me@asia"}},
	}}
	local, err := ExecutorFor(cfg, wdconf.LocalHost)
	if err != nil {
		t.Fatalf("ExecutorFor(local) error = %v", err)
	}
	if _, ok := local.(launch.Tmux); !ok {
		t.Fatalf("local executor = %T, want launch.Tmux", local)
	}
	remote, err := ExecutorFor(cfg, "asia")
	if err != nil {
		t.Fatalf("ExecutorFor(asia) error = %v", err)
	}
	got, ok := remote.(launch.Remote)
	if !ok {
		t.Fatalf("remote executor = %T, want launch.Remote", remote)
	}
	if got.Target != "me@asia" || len(got.AttachArgv) != 2 {
		t.Fatalf("remote executor = %+v, want the host's target and attach argv", got)
	}
}

func TestHostHonoursRunOnLocal(t *testing.T) {
	remote := action.Resolved{ID: "shell", Host: "asia"}
	if got := Host(remote); got != "asia" {
		t.Errorf("Host() = %q, want the workspace's own host", got)
	}
	if got := Host(action.Resolved{ID: "browse", Host: "asia", RunOn: wdconf.RunOnLocal}); got != wdconf.LocalHost {
		t.Errorf("Host() = %q, want %q: a browser opens on this machine", got, wdconf.LocalHost)
	}
}

func TestExecutorForRejectsUnknownHost(t *testing.T) {
	if _, err := ExecutorFor(wdconf.Config{}, "nowhere"); err == nil {
		t.Fatal("ExecutorFor() error = nil, want an error naming the unknown host")
	}
}

// A GUI program routed to capture would block the caller until it exits.
func TestExecuteRoutesBackgroundToTheBackgroundVerb(t *testing.T) {
	executor := &fakeExecutor{}
	if _, err := Execute(executor, action.Resolved{ID: "open-pr", Where: wdconf.WhereBackground}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(executor.background) != 1 || len(executor.capture) != 0 || len(executor.pane) != 0 {
		t.Fatalf("routing = background %d capture %d pane %d, want background only",
			len(executor.background), len(executor.capture), len(executor.pane))
	}
}
