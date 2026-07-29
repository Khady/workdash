package launch

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Khady/workdash/internal/action"
)

type recordedCall struct {
	cwd  string
	argv []string
}

// recordRunner keeps argv as argv: assertions compare elements, not a joined
// string, because the whole point of the shell path is that a quoted command
// stays a single argv element.
type recordRunner struct {
	calls []recordedCall
	err   error
	// sessions is the set of tmux sessions the fake server knows about. A
	// `tmux has-session` probe answers from it and `tmux new-session` adds to
	// it, so a second Pane on the same workspace sees the first one.
	sessions map[string]bool
}

func (r *recordRunner) Run(cwd string, argv []string) (string, error) {
	r.calls = append(r.calls, recordedCall{cwd: cwd, argv: append([]string(nil), argv...)})
	if len(argv) >= 3 && argv[0] == "tmux" && argv[1] == "has-session" {
		if r.sessions[strings.TrimPrefix(strings.TrimSuffix(argv[2], ":"), "-t=")] {
			return "", nil
		}
		return "", errors.New("can't find session")
	}
	if len(argv) >= 5 && argv[0] == "tmux" && argv[1] == "new-session" && r.err == nil {
		if r.sessions == nil {
			r.sessions = map[string]bool{}
		}
		for i, arg := range argv {
			if arg == "-s" && i+1 < len(argv) {
				r.sessions[argv[i+1]] = true
			}
		}
	}
	return "ok", r.err
}

func (r *recordRunner) argv(i int) []string {
	if i >= len(r.calls) {
		return nil
	}
	return r.calls[i].argv
}

func wantArgv(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("argv has %d elements %q, want %d elements %q", len(got), got, len(want), want)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

func TestPaneInsideTmuxOpensWindowNamedAfterWorkspace(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: true}
	got, err := tmux.Pane(action.Resolved{ID: "agent", Name: "topic", Argv: []string{"claude"}, Cwd: "/wt/topic"})
	if err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d %q, want 1", len(runner.calls), runner.calls)
	}
	wantArgv(t, runner.argv(0), []string{"tmux", "new-window", "-n", "topic", "-c", "/wt/topic", "--", "claude"})
	if runner.calls[0].cwd != "" {
		t.Fatalf("cwd = %q, want empty", runner.calls[0].cwd)
	}
	if !strings.Contains(got, "topic") {
		t.Fatalf("message = %q, want it to name the window", got)
	}
}

func TestPaneOutsideTmuxCreatesDetachedSession(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: false}
	out, err := tmux.Pane(action.Resolved{ID: "agent", Name: "topic", Argv: []string{"claude"}, Cwd: "/wt/topic"})
	if err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %d %q, want a has-session probe then new-session", len(runner.calls), runner.calls)
	}
	wantArgv(t, runner.argv(0), []string{"tmux", "has-session", "-t=topic"})
	wantArgv(t, runner.argv(1), []string{"tmux", "new-session", "-d", "-s", "topic", "-c", "/wt/topic", "--", "claude"})
	if !strings.Contains(out, "tmux attach -t topic") {
		t.Fatalf("message = %q, want attach hint", out)
	}
}

func TestPaneOutsideTmuxAddsWindowWhenSessionExists(t *testing.T) {
	runner := &recordRunner{sessions: map[string]bool{"topic": true}}
	tmux := Tmux{Runner: runner, InsideTmux: false}
	out, err := tmux.Pane(action.Resolved{ID: "agent", Name: "topic", Argv: []string{"claude"}, Cwd: "/wt/topic"})
	if err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %d %q, want a has-session probe then new-window", len(runner.calls), runner.calls)
	}
	wantArgv(t, runner.argv(0), []string{"tmux", "has-session", "-t=topic"})
	wantArgv(t, runner.argv(1), []string{"tmux", "new-window", "-t=topic:", "-n", "topic", "-c", "/wt/topic", "--", "claude"})
	if !strings.Contains(out, "topic") || !strings.Contains(out, "window") {
		t.Fatalf("message = %q, want it to say a window was added to session topic", out)
	}
}

// A workspace may carry several sessions, so a second action on the same branch
// has to land somewhere instead of failing with `duplicate session`.
func TestPaneOutsideTmuxTwiceOnSameWorkspaceSucceeds(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: false}
	resolved := action.Resolved{ID: "agent", Name: "topic", Argv: []string{"claude"}, Cwd: "/wt/topic"}
	if _, err := tmux.Pane(resolved); err != nil {
		t.Fatalf("first Pane() error = %v", err)
	}
	out, err := tmux.Pane(action.Resolved{ID: "shell", Name: "topic", Argv: []string{"htop"}, Cwd: "/wt/topic"})
	if err != nil {
		t.Fatalf("second Pane() error = %v", err)
	}
	if len(runner.calls) != 4 {
		t.Fatalf("calls = %d %q, want two probes and two creations", len(runner.calls), runner.calls)
	}
	wantArgv(t, runner.argv(3), []string{"tmux", "new-window", "-t=topic:", "-n", "topic", "-c", "/wt/topic", "--", "htop"})
	if !strings.Contains(out, "topic") {
		t.Fatalf("message = %q, want it to name where the command landed", out)
	}
}

func TestPaneWrapsShellActionsInInteractiveShell(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: true}
	resolved := action.Resolved{ID: "gwt", Name: "topic", Argv: []string{"gwt", "my topic"}, Cwd: "/repo", Shell: true}
	if _, err := tmux.Pane(resolved); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	wantArgv(t, runner.argv(0), []string{"tmux", "new-window", "-n", "topic", "-c", "/repo", "--", "bash", "-ic", `gwt 'my topic'`})
}

func TestPaneFallsBackToActionIDWhenUnnamed(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: true}
	if _, err := tmux.Pane(action.Resolved{ID: "agent", Argv: []string{"claude"}, Cwd: "/wt"}); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	wantArgv(t, runner.argv(0), []string{"tmux", "new-window", "-n", "agent", "-c", "/wt", "--", "claude"})
}

func TestPaneReportsRunnerFailure(t *testing.T) {
	runner := &recordRunner{err: errors.New("no server running")}
	tmux := Tmux{Runner: runner, InsideTmux: true}
	if _, err := tmux.Pane(action.Resolved{ID: "agent", Name: "topic", Argv: []string{"claude"}, Cwd: "/wt"}); err == nil {
		t.Fatal("Pane() error = nil, want the runner failure")
	}
}

func TestPaneReportsRunnerFailureOutsideTmux(t *testing.T) {
	runner := &recordRunner{err: errors.New("duplicate session")}
	tmux := Tmux{Runner: runner, InsideTmux: false}
	if _, err := tmux.Pane(action.Resolved{ID: "agent", Name: "topic", Argv: []string{"claude"}, Cwd: "/wt"}); err == nil {
		t.Fatal("Pane() error = nil, want the runner failure")
	}
}

func TestCaptureRunsInWorkspaceDirectory(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner}
	out, err := tmux.Capture(action.Resolved{ID: "status", Name: "topic", Argv: []string{"git", "status"}, Cwd: "/wt/topic"})
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	if out != "ok" {
		t.Fatalf("out = %q, want ok", out)
	}
	if runner.calls[0].cwd != "/wt/topic" {
		t.Fatalf("cwd = %q, want /wt/topic", runner.calls[0].cwd)
	}
	wantArgv(t, runner.argv(0), []string{"git", "status"})
}
