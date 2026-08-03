package collect

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestSSHScriptRunsEverySectionInOneCommand(t *testing.T) {
	plan := PlanFor(Host{Label: "asia", SSHTarget: "me@asia", TmuxEnabled: true,
		Repos: []Repo{{Root: "/srv/a", Label: "a"}}})
	script := sshScript(plan)
	if got := strings.Count(script, sectionMarker); got != len(plan.Sections) {
		t.Fatalf("script has %d markers, want %d:\n%s", got, len(plan.Sections), script)
	}
	if !strings.Contains(script, "/srv/a") {
		t.Fatalf("script never enters the repo:\n%s", script)
	}
	if !strings.Contains(script, "worktree list --porcelain") {
		t.Fatalf("script is missing the worktree command:\n%s", script)
	}
}

// The script is what actually runs on the remote host, so run it locally against
// a real shell: a script that a shell rejects would be a silent empty snapshot.
func TestSSHScriptIsValidShellAndEmitsParseableSections(t *testing.T) {
	plan := Plan{Sections: []Section{
		{Key: "a", Argv: []string{"printf", "%s\\n", "first"}},
		{Key: "b", Argv: []string{"printf", "%s\\n", "second value"}},
	}}
	out, err := exec.Command("bash", "-c", sshScript(plan)).Output()
	if err != nil {
		t.Fatalf("script failed to run: %v\n%s", err, sshScript(plan))
	}
	sections := splitSections(string(out))
	if sections["a"] != "first" {
		t.Fatalf("section a = %q, want first", sections["a"])
	}
	if sections["b"] != "second value" {
		t.Fatalf("section b = %q, want \"second value\"", sections["b"])
	}
}

// One unreadable repo must not take the rest of the host down with it.
func TestSSHScriptSurvivesAFailingSection(t *testing.T) {
	plan := Plan{Sections: []Section{
		{Key: "broken", Cwd: "/definitely/not/here", Argv: []string{"git", "status"}},
		{Key: "fine", Argv: []string{"printf", "%s\\n", "still here"}},
	}}
	out, err := exec.Command("bash", "-c", sshScript(plan)).Output()
	if err != nil {
		t.Fatalf("script aborted on a failing section: %v", err)
	}
	sections := splitSections(string(out))
	if sections["fine"] != "still here" {
		t.Fatalf("section fine = %q, want the later section to still run", sections["fine"])
	}
	if sections["broken"] != "" {
		t.Fatalf("section broken = %q, want empty", sections["broken"])
	}
}

func TestSplitSectionsFilesOutputUnderKeys(t *testing.T) {
	out := sectionMarker + " worktrees/0\nworktree /srv/a\nbranch refs/heads/main\n" +
		sectionMarker + " branches/0\nmain\ntopic\n"
	got := splitSections(out)
	if got["worktrees/0"] != "worktree /srv/a\nbranch refs/heads/main" {
		t.Fatalf("worktrees section = %q", got["worktrees/0"])
	}
	if got["branches/0"] != "main\ntopic" {
		t.Fatalf("branches section = %q", got["branches/0"])
	}
}

// A remote login shell may print a banner before our first marker.
func TestSplitSectionsIgnoresPreamble(t *testing.T) {
	out := "motd noise from the remote shell\n" + sectionMarker + " tmux\nname\t/path\t1\t0\n"
	got := splitSections(out)
	if got["tmux"] != "name\t/path\t1\t0" {
		t.Fatalf("tmux section = %q, want the preamble discarded", got["tmux"])
	}
}

func TestSSHRunIssuesExactlyOneCommand(t *testing.T) {
	runner := &recordingRunner{out: sectionMarker + " tmux\nwork\t/srv/a\t1\t0"}
	transport := SSH{Runner: runner, Target: "me@asia"}
	sections, err := transport.Run(PlanFor(Host{Label: "asia", TmuxEnabled: true}))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %d %q, want exactly one ssh invocation", len(runner.calls), runner.calls)
	}
	argv := runner.calls[0]
	if argv[0] != "ssh" || argv[len(argv)-3] != "bash" || argv[len(argv)-2] != "-c" {
		t.Fatalf("argv = %q, want ssh ... bash -c <script>", argv)
	}
	if !strings.Contains(argv[len(argv)-1], sectionMarker) {
		t.Fatalf("script argument = %q, want the whole plan in one element", argv[len(argv)-1])
	}
	if sections["tmux"] != "work\t/srv/a\t1\t0" {
		t.Fatalf("tmux section = %q", sections["tmux"])
	}
}

func TestSSHRunReportsConnectionFailure(t *testing.T) {
	runner := &recordingRunner{err: errors.New("ssh: connect to host asia port 22: Network is unreachable")}
	transport := SSH{Runner: runner, Target: "me@asia"}
	if _, err := transport.Run(PlanFor(Host{Label: "asia", TmuxEnabled: true})); err == nil {
		t.Fatal("Run() error = nil, want the connection failure")
	}
}

func TestLocalRunExecutesEachSection(t *testing.T) {
	runner := &recordingRunner{out: "output"}
	transport := Local{Runner: runner}
	plan := PlanFor(Host{Label: "local", Repos: []Repo{{Root: "/a", Label: "a"}}})
	sections, err := transport.Run(plan)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(runner.calls) != len(plan.Sections) {
		t.Fatalf("calls = %d, want %d", len(runner.calls), len(plan.Sections))
	}
	if sections[worktreeKey(0)] != "output" {
		t.Fatalf("section = %q", sections[worktreeKey(0)])
	}
}

type recordingRunner struct {
	calls [][]string
	out   string
	err   error
}

func (r *recordingRunner) Run(cwd string, argv []string) (string, error) {
	r.calls = append(r.calls, append([]string(nil), argv...))
	return r.out, r.err
}

// ssh does not preserve argv boundaries: it joins its command arguments with
// spaces and lets the remote login shell re-parse the result. This test
// reproduces that faithfully — join the remote part of the argv and run it
// through a real shell — because a script that survives exec but not ssh's
// re-parse silently loses whichever section is emitted first.
func TestSSHArgvSurvivesRemoteReparsing(t *testing.T) {
	runner := &recordingRunner{}
	plan := Plan{Sections: []Section{
		{Key: "first", Argv: []string{"printf", "%s\\n", "one"}},
		{Key: "second", Argv: []string{"printf", "%s\\n", "two"}},
	}}
	if _, err := (SSH{Runner: runner, Target: "me@asia"}).Run(plan); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	argv := runner.calls[0]
	remote := strings.Join(argv[indexOfTarget(argv, "me@asia")+1:], " ")
	out, err := exec.Command("bash", "-c", remote).Output()
	if err != nil {
		t.Fatalf("remote command %q failed: %v", remote, err)
	}
	sections := splitSections(string(out))
	if sections["first"] != "one" {
		t.Fatalf("first section = %q, want \"one\" — the first section is the one ssh re-parsing loses", sections["first"])
	}
	if sections["second"] != "two" {
		t.Fatalf("second section = %q, want \"two\"", sections["second"])
	}
}

func indexOfTarget(argv []string, target string) int {
	for i, arg := range argv {
		if arg == target {
			return i
		}
	}
	return -1
}

// The script's exit status is the last section's, which says nothing about the
// host. A dead tmux server made the final section exit 1, and the whole host —
// eleven repos of git facts that had been collected fine — was discarded with
// "ssh: exit status 1". The script must end deliberately.
func TestSSHScriptEndsSuccessfullyEvenWhenTheLastSectionFails(t *testing.T) {
	plan := Plan{Sections: []Section{
		{Key: "good", Argv: []string{"printf", "%s\\n", "collected"}},
		{Key: "broken", Argv: []string{"false"}},
	}}
	cmd := exec.Command("bash", "-c", sshScript(plan))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("script exited non-zero because its last section failed: %v", err)
	}
	if got := splitSections(string(out))["good"]; got != "collected" {
		t.Fatalf("good section = %q, want the earlier section still collected", got)
	}
}
