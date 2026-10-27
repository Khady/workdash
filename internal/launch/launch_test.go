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
	// clients is what `tmux list-clients` reports: "activity session tty" per line.
	clients string
	// out is what Run returns for commands the fake does not model specially,
	// letting a test stand in for output the executor reads back — the grouped
	// session's generated name, for instance.
	out string
	// sessions is the set of tmux sessions the fake server knows about. A
	// `tmux has-session` probe answers from it and `tmux new-session` adds to
	// it, so a second Pane on the same workspace sees the first one.
	sessions map[string]bool
}

func (r *recordRunner) Run(cwd string, argv []string) (string, error) {
	r.calls = append(r.calls, recordedCall{cwd: cwd, argv: append([]string(nil), argv...)})
	// The fake models one tmux server, and does so for both the direct form
	// (`tmux has-session ...`) and the ssh-wrapped form, where the whole tmux
	// command arrives as remote shell text. Recognising only the direct form
	// would let the remote path silently take the wrong branch.
	text := strings.Join(argv, " ")
	if argv[0] == "ssh" {
		// What a shell on the far side would run: the last element, one layer of
		// quoting removed. Matching the raw element instead would see the escaped
		// form ('\'') and read every -t= target as garbage.
		text = unquoteOnce(argv[len(argv)-1])
	}
	if strings.Contains(text, "list-clients") {
		return r.clients, nil
	}
	if strings.Contains(text, "tmux has-session") {
		if r.sessions[tmuxTargetName(text)] {
			return "", nil
		}
		return "", errors.New("can't find session")
	}
	if name := afterMarker(text, "tmux new-session -d -s "); name != "" && r.err == nil {
		if r.sessions == nil {
			r.sessions = map[string]bool{}
		}
		r.sessions[name] = true
	}
	if r.out != "" {
		return r.out, r.err
	}
	return "ok", r.err
}

// tmuxTargetName pulls the session out of a "-t=name" target, which may be
// quoted and may carry a trailing ":" for session scope.
func tmuxTargetName(text string) string {
	name := afterMarker(text, "-t=")
	return strings.Trim(name, "':\"")
}

func afterMarker(text, marker string) string {
	i := strings.Index(text, marker)
	if i < 0 {
		return ""
	}
	rest := text[i+len(marker):]
	if j := strings.IndexAny(rest, " \t"); j >= 0 {
		rest = rest[:j]
	}
	return strings.Trim(rest, "':\"")
}

func (r *recordRunner) argv(i int) []string {
	if i >= len(r.calls) {
		return nil
	}
	return r.calls[i].argv
}

// findCall returns the first recorded call containing want. Assertions go by
// content rather than index: the executor asks tmux where the user is looking
// before it does anything, and counting calls made every test brittle to that.
func findCall(t *testing.T, runner *recordRunner, want string) []string {
	t.Helper()
	for i := range runner.calls {
		if strings.Contains(strings.Join(runner.argv(i), " "), want) {
			return runner.argv(i)
		}
	}
	t.Fatalf("no call containing %q in %q", want, runner.calls)
	return nil
}

func countCalls(runner *recordRunner, want string) int {
	n := 0
	for i := range runner.calls {
		if strings.Contains(strings.Join(runner.argv(i), " "), want) {
			n++
		}
	}
	return n
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
	wantArgv(t, runner.argv(0), []string{"tmux", "new-window", "-P", "-F", "#{window_id}", "-n", "topic", "-c", "/wt/topic", "--", "claude"})
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
	wantArgv(t, findCall(t, runner, "has-session"), []string{"tmux", "has-session", "-t=topic"})
	wantArgv(t, findCall(t, runner, "new-session"),
		[]string{"tmux", "new-session", "-d", "-s", "topic", "-c", "/wt/topic", "--", "claude"})
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
	wantArgv(t, findCall(t, runner, "has-session"), []string{"tmux", "has-session", "-t=topic"})
	wantArgv(t, findCall(t, runner, "new-window"),
		[]string{"tmux", "new-window", "-t=topic:", "-n", "topic", "-c", "/wt/topic", "--", "claude"})
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
	if got := countCalls(runner, "has-session"); got != 2 {
		t.Fatalf("has-session probes = %d, want one per call", got)
	}
	wantArgv(t, findCall(t, runner, "new-window"),
		[]string{"tmux", "new-window", "-t=topic:", "-n", "topic", "-c", "/wt/topic", "--", "htop"})
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
	wantArgv(t, runner.argv(0), []string{"tmux", "new-window", "-P", "-F", "#{window_id}", "-n", "topic", "-c", "/repo", "--", "bash", "-ic", `gwt 'my topic'`})
}

func TestPaneFallsBackToActionIDWhenUnnamed(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: true}
	if _, err := tmux.Pane(action.Resolved{ID: "agent", Argv: []string{"claude"}, Cwd: "/wt"}); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	wantArgv(t, runner.argv(0), []string{"tmux", "new-window", "-P", "-F", "#{window_id}", "-n", "agent", "-c", "/wt", "--", "claude"})
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

// Locally too: a workspace with a session already doing that work should get the
// new window in that session, not beside it in whichever session you happen to
// be sitting in.
func TestPaneTargetsTheWorkspacesExistingSessionInsideTmux(t *testing.T) {
	runner := &recordRunner{sessions: map[string]bool{"gwt-local-x": true}}
	tmux := Tmux{Runner: runner, InsideTmux: true}
	resolved := action.Resolved{ID: "shell", Name: "topic", Session: "gwt-local-x",
		Argv: []string{"bash"}, Cwd: "/wt/topic"}
	if _, err := tmux.Pane(resolved); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	window := findCall(t, runner, "new-window")
	joined := strings.Join(window, " ")
	if !strings.Contains(joined, "-t=gwt-local-x:") {
		t.Fatalf("argv = %q, want the existing session targeted", window)
	}
	if !strings.Contains(joined, "-n topic") {
		t.Fatalf("argv = %q, want the window still named after the branch", window)
	}
}

// The same locally: the session the snapshot recorded can be gone by the time the
// action runs, and `new-window -t` on a name tmux no longer knows fails. Forgetting
// it puts the window where a window with no session goes — here, the session the
// caller is already in.
func TestPaneForgetsASessionThatDiedSinceTheSnapshot(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: true}
	resolved := action.Resolved{ID: "shell", Name: "topic", Session: "gwt-local-x",
		Argv: []string{"bash"}, Cwd: "/wt/topic"}
	if _, err := tmux.Pane(resolved); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	window := findCall(t, runner, "new-window")
	if strings.Contains(strings.Join(window, " "), "gwt-local-x") {
		t.Fatalf("argv = %q, want no target: the session is gone", window)
	}
}

// Attach creates nothing. It is the third verb because "connect me to what is
// already running" is not "run a command" — and it is the whole point of a tool
// built around focus-versus-launch.
func TestAttachSwitchesClientInsideTmux(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: true}
	out, err := tmux.Attach(action.Resolved{ID: "attach", Name: "topic", Session: "gwt-local-x"})
	if err != nil {
		t.Fatalf("Attach() error = %v", err)
	}
	wantArgv(t, findCall(t, runner, "switch-client"), []string{"tmux", "switch-client", "-t=gwt-local-x"})
	if !strings.Contains(out, "gwt-local-x") {
		t.Fatalf("message = %q, want it to name the session", out)
	}
}

func TestAttachAttachesWhenOutsideTmux(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: false}
	if _, err := tmux.Attach(action.Resolved{ID: "attach", Name: "topic", Session: "sess"}); err != nil {
		t.Fatalf("Attach() error = %v", err)
	}
	wantArgv(t, findCall(t, runner, "tmux attach"), []string{"tmux", "attach", "-t=sess"})
}

func TestAttachRefusesWithoutASession(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: true}
	if _, err := tmux.Attach(action.Resolved{ID: "attach", Name: "topic"}); err == nil {
		t.Fatal("Attach() error = nil, want a refusal when there is no session")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("calls = %q, want none", runner.calls)
	}
}

// A GUI app is neither a pane nor something to wait for. GUI emacs stays in the
// foreground until you close it, and a browser handed a URL should never make the
// picker wait — so "start it and forget it" is its own verb.
func TestBackgroundDetachesAndDoesNotCapture(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: true, Detach: func(cwd string, argv []string) error {
		runner.calls = append(runner.calls, recordedCall{cwd: cwd, argv: argv})
		return nil
	}}
	out, err := tmux.Background(action.Resolved{ID: "open-pr", Name: "topic",
		Argv: []string{"firefox", "https://example.com/x"}, Cwd: "/wt/topic"})
	if err != nil {
		t.Fatalf("Background() error = %v", err)
	}
	wantArgv(t, runner.argv(0), []string{"firefox", "https://example.com/x"})
	if runner.calls[0].cwd != "/wt/topic" {
		t.Fatalf("cwd = %q, want /wt/topic", runner.calls[0].cwd)
	}
	if !strings.Contains(out, "open-pr") {
		t.Fatalf("message = %q, want it to name the action", out)
	}
}

func TestBackgroundReportsAFailureToStart(t *testing.T) {
	tmux := Tmux{Runner: &recordRunner{}, Detach: func(string, []string) error {
		return errors.New("exec: \"firefox\": executable file not found in $PATH")
	}}
	if _, err := tmux.Background(action.Resolved{ID: "open-pr", Argv: []string{"firefox"}}); err == nil {
		t.Fatal("Background() error = nil, want the start failure")
	}
}

// The Firefox host runs outside tmux, so the old "no TMUX, make a detached
// session" path left the work in a session nobody was looking at and printed
// "attach with tmux attach -t …" into a browser popup. What it should do is put
// the window where your eyes already are: tmux tells us which client is attached
// and to what.
func TestPaneOutsideTmuxTargetsTheAttachedClientsSession(t *testing.T) {
	runner := &recordRunner{clients: "1785750608 work /dev/pts/8"}
	tmux := Tmux{Runner: runner, InsideTmux: false}
	out, err := tmux.Pane(action.Resolved{ID: "shell", Name: "topic",
		Argv: []string{"bash"}, Cwd: "/wt/topic"})
	if err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	all := ""
	for i := range runner.calls {
		all += strings.Join(runner.argv(i), " ") + "\n"
	}
	if strings.Contains(all, "new-session") {
		t.Fatalf("calls:\n%s\nwant no detached session when a client is attached", all)
	}
	if !strings.Contains(all, "new-window -t=work:") {
		t.Fatalf("calls:\n%s\nwant the window created in the attached client's session", all)
	}
	if !strings.Contains(out, "work") {
		t.Fatalf("message = %q, want it to name where the window went", out)
	}
}

// A window created in another session would still be invisible, so the client has
// to be moved to it.
func TestPaneOutsideTmuxSwitchesTheClientToTheWorkspaceSession(t *testing.T) {
	runner := &recordRunner{clients: "1785750608 zero /dev/pts/8", sessions: map[string]bool{"gwt-x": true}}
	tmux := Tmux{Runner: runner, InsideTmux: false}
	if _, err := tmux.Pane(action.Resolved{ID: "shell", Name: "topic", Session: "gwt-x",
		Argv: []string{"bash"}, Cwd: "/wt/topic"}); err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	all := ""
	for i := range runner.calls {
		all += strings.Join(runner.argv(i), " ") + "\n"
	}
	if !strings.Contains(all, "new-window -t=gwt-x:") {
		t.Fatalf("calls:\n%s\nwant the window in the workspace's own session", all)
	}
	if !strings.Contains(all, "switch-client -c /dev/pts/8 -t=gwt-x") {
		t.Fatalf("calls:\n%s\nwant the attached client moved to it", all)
	}
}

func TestPaneOutsideTmuxWithNoClientStillCreatesADetachedSession(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: false}
	out, err := tmux.Pane(action.Resolved{ID: "shell", Name: "topic", Argv: []string{"bash"}, Cwd: "/wt"})
	if err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	if !strings.Contains(out, "attach") {
		t.Fatalf("message = %q, want the attach hint when there is nowhere to show it", out)
	}
}

func TestAttachOutsideTmuxSwitchesTheAttachedClient(t *testing.T) {
	runner := &recordRunner{clients: "1785750608 zero /dev/pts/8"}
	tmux := Tmux{Runner: runner, InsideTmux: false}
	if _, err := tmux.Attach(action.Resolved{ID: "attach", Name: "topic", Session: "gwt-x"}); err != nil {
		t.Fatalf("Attach() error = %v", err)
	}
	wantArgv(t, runner.argv(len(runner.calls)-1),
		[]string{"tmux", "switch-client", "-c", "/dev/pts/8", "-t=gwt-x"})
}

// With no tmux client anywhere, a detached session is invisible — and from a
// browser popup the printed "attach with tmux attach -t …" cannot be acted on.
// A configured terminal is the one case where workdash spawns one.
func TestPaneWithNoClientOpensTheConfiguredTerminal(t *testing.T) {
	runner := &recordRunner{}
	spawned := [][]string{}
	tmux := Tmux{Runner: runner, InsideTmux: false,
		Terminal: []string{"kitty", "--single-instance", "-e"},
		Detach: func(cwd string, argv []string) error {
			spawned = append(spawned, argv)
			return nil
		}}
	out, err := tmux.Pane(action.Resolved{ID: "shell", Name: "topic", Argv: []string{"bash"}, Cwd: "/wt"})
	if err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	if len(spawned) != 1 {
		t.Fatalf("spawned = %v, want one terminal", spawned)
	}
	wantArgv(t, spawned[0], []string{"kitty", "--single-instance", "-e", "tmux", "attach", "-t=topic"})
	if !strings.Contains(out, "terminal") {
		t.Fatalf("message = %q, want it to say a terminal was opened", out)
	}
}

// Without one configured, the behaviour is unchanged: say where the work is.
func TestPaneWithNoClientAndNoTerminalStillReportsTheSession(t *testing.T) {
	runner := &recordRunner{}
	tmux := Tmux{Runner: runner, InsideTmux: false}
	out, err := tmux.Pane(action.Resolved{ID: "shell", Name: "topic", Argv: []string{"bash"}, Cwd: "/wt"})
	if err != nil {
		t.Fatalf("Pane() error = %v", err)
	}
	if !strings.Contains(out, "tmux attach -t topic") {
		t.Fatalf("message = %q, want the attach hint", out)
	}
}
