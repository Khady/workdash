package collect

import (
	"errors"
	"strings"
	"testing"
)

// failRunner models how a real command fails: stdout is empty — tmux writes its
// diagnostics to stderr — and the stderr text travels in the error. A fake that
// returned the diagnostic as stdout would be testing something LocalRunner
// cannot produce.
type failRunner struct {
	stderr string
	err    error
}

func (f failRunner) Run(_ string, argv []string) (string, error) {
	return "", &CommandError{Argv: argv, Stderr: f.stderr, Err: f.err}
}

func TestTmuxCollectsSessions(t *testing.T) {
	runner := fakeRunner{t: t, out: map[string]string{
		strings.Join(tmuxArgv, " "): "topic\t/wt/topic\t3\t1\nidle\t/repo\t1\t0\n",
	}}
	records, err := Tmux(runner, "local")
	if err != nil {
		t.Fatalf("Tmux() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	if records[0].Session.Name != "topic" || records[0].Session.Windows != 3 || !records[0].Session.Attached {
		t.Fatalf("first session = %+v", records[0].Session)
	}
	if records[1].Session.Attached {
		t.Fatal("second session should be detached")
	}
}

func TestTmuxTreatsMissingServerAsEmpty(t *testing.T) {
	runner := failRunner{
		stderr: "no server running on /tmp/tmux-1000/default\n",
		err:    errors.New("exit status 1"),
	}
	records, err := Tmux(runner, "local")
	if err != nil {
		t.Fatalf("Tmux() error = %v, want nil", err)
	}
	if len(records) != 0 {
		t.Fatalf("got %d records, want 0", len(records))
	}
}

func TestTmuxTreatsMissingSocketAsEmpty(t *testing.T) {
	runner := failRunner{
		stderr: "error connecting to /tmp/tmux-1000/default (No such file or directory)\n",
		err:    errors.New("exit status 1"),
	}
	records, err := Tmux(runner, "local")
	if err != nil {
		t.Fatalf("Tmux() error = %v, want nil", err)
	}
	if len(records) != 0 {
		t.Fatalf("got %d records, want 0", len(records))
	}
}

// A tmux that cannot even reach its socket directory is broken, not idle.
// Reporting no sessions would strip the session facet from every workspace and
// read as "my sessions died".
func TestTmuxReportsGenuineFailure(t *testing.T) {
	runner := failRunner{
		stderr: "couldn't create directory /root/tmux-1000 (Permission denied)\n",
		err:    errors.New("exit status 1"),
	}
	records, err := Tmux(runner, "local")
	if err == nil {
		t.Fatalf("Tmux() error = nil, want a failure; records = %+v", records)
	}
	if !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("error = %q, want it to quote tmux's stderr", err)
	}
	if records != nil {
		t.Fatalf("got %+v, want no records alongside the error", records)
	}
}
