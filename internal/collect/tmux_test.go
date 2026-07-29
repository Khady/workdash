package collect

import (
	"errors"
	"strings"
	"testing"
)

type errRunner struct{ err error }

func (e errRunner) Run(string, []string) (string, error) {
	return "no server running on /tmp/tmux-1000/default", e.err
}

func TestTmuxCollectsSessions(t *testing.T) {
	runner := fakeRunner{out: map[string]string{
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
	records, err := Tmux(errRunner{err: errors.New("exit status 1")}, "local")
	if err != nil {
		t.Fatalf("Tmux() error = %v, want nil", err)
	}
	if len(records) != 0 {
		t.Fatalf("got %d records, want 0", len(records))
	}
}
