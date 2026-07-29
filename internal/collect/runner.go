// Package collect turns the output of external tools — git, tmux — into facet
// records the workspace index can merge.
package collect

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Runner executes a command somewhere. The indirection is what lets collectors
// be tested without a real repo, and later lets the same collector run over ssh.
type Runner interface {
	Run(cwd string, argv []string) (string, error)
}

// CommandError reports a command that failed, carrying what it wrote to stderr.
// An exit status on its own is not diagnosable — git says 128 for everything —
// and collectors also have to classify failures: "no tmux server" is a normal
// state while "cannot create the socket directory" is not, and only the stderr
// text tells them apart.
type CommandError struct {
	Argv   []string
	Stderr string
	Err    error
}

func (e *CommandError) Error() string {
	name := "command"
	if len(e.Argv) > 0 {
		name = e.Argv[0]
	}
	if line := firstLine(e.Stderr); line != "" {
		return fmt.Sprintf("%s: %v: %s", name, e.Err, line)
	}
	return fmt.Sprintf("%s: %v", name, e.Err)
}

func (e *CommandError) Unwrap() error { return e.Err }

// Stderr returns what the failed command wrote to stderr, or "" when err did
// not come from a Runner that captures it.
func Stderr(err error) string {
	var cmdErr *CommandError
	if errors.As(err, &cmdErr) {
		return cmdErr.Stderr
	}
	return ""
}

// firstLine is the first non-empty line, which for git and tmux is the actual
// message; later lines are hints and advice that only bury it.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

type LocalRunner struct{}

func (LocalRunner) Run(cwd string, argv []string) (string, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimRight(stdout.String(), "\n")
	if err != nil {
		return out, &CommandError{Argv: argv, Stderr: stderr.String(), Err: err}
	}
	return out, nil
}
