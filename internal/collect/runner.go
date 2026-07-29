// Package collect turns the output of external tools — git, tmux — into facet
// records the workspace index can merge.
package collect

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner executes a command somewhere. The indirection is what lets collectors
// be tested without a real repo, and later lets the same collector run over ssh.
type Runner interface {
	Run(cwd string, argv []string) (string, error)
}

// EnvRunner can also pass variables to a command. It exists for secrets: an
// argument is visible to every local user through /proc/<pid>/cmdline, while the
// environment in /proc/<pid>/environ is readable only by the process owner. A
// token therefore has to travel as env, never as argv.
type EnvRunner interface {
	Runner
	RunEnv(cwd string, env []string, argv []string) (string, error)
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

func (r LocalRunner) Run(cwd string, argv []string) (string, error) {
	return r.RunEnv(cwd, nil, argv)
}

// RunEnv adds env to the ambient environment rather than replacing it: the tools
// workdash shells out to need PATH, HOME and the rest to work at all, so a
// command started with only the extra variables would simply fail.
func (LocalRunner) RunEnv(cwd string, env []string, argv []string) (string, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
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
