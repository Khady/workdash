package launch

import (
	"errors"
	"strings"
	"testing"

	"github.com/Khady/workdash/internal/action"
)

type recordRunner struct {
	calls [][]string
	err   error
}

func (r *recordRunner) Run(cwd string, argv []string) (string, error) {
	r.calls = append(r.calls, append([]string{cwd}, argv...))
	return "ok", r.err
}

func (r *recordRunner) call(i int) string { return strings.Join(r.calls[i], " ") }

func TestPaneInsideTmuxOpensWindowNamedAfterWorkspace(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: true}
	got, err := tmux.Pane(action.Resolved{ID: "agent", Name: "topic", Argv: []string{"claude"}, Cwd: "/wt/topic"})
	if err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	want := " tmux new-window -n topic -c /wt/topic -- claude"
	if runner.call(0) != want {
		t.Fatalf("call = %q, want %q", runner.call(0), want)
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
	if !strings.Contains(runner.call(0), "tmux new-session -d -s topic -c /wt/topic -- claude") {
		t.Fatalf("call = %q", runner.call(0))
	}
	if !strings.Contains(out, "tmux attach -t topic") {
		t.Fatalf("message = %q, want attach hint", out)
	}
}

func TestPaneWrapsShellActionsInLoginShell(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: true}
	resolved := action.Resolved{ID: "gwt", Name: "topic", Argv: []string{"gwt", "my topic"}, Cwd: "/repo", Shell: true}
	if _, err := tmux.Pane(resolved); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	if !strings.HasSuffix(runner.call(0), `-- bash -ic gwt 'my topic'`) {
		t.Fatalf("call = %q, want a bash -ic wrapper with quoting", runner.call(0))
	}
}

func TestPaneFallsBackToActionIDWhenUnnamed(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: true}
	if _, err := tmux.Pane(action.Resolved{ID: "agent", Argv: []string{"claude"}, Cwd: "/wt"}); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	if !strings.Contains(runner.call(0), "-n agent") {
		t.Fatalf("call = %q, want the action id as the window name", runner.call(0))
	}
}

func TestPaneReportsRunnerFailure(t *testing.T) {
	runner := &recordRunner{err: errors.New("no server running")}
	tmux := Tmux{Runner: runner, InsideTmux: true}
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
	if runner.calls[0][0] != "/wt/topic" {
		t.Fatalf("cwd = %q, want /wt/topic", runner.calls[0][0])
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"plain":     "plain",
		"two words": "'two words'",
		"it's":      `'it'\''s'`,
		"":          "''",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Fatalf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
