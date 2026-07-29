// Package collect turns the output of external tools — git, tmux — into facet
// records the workspace index can merge.
package collect

import (
	"os/exec"
	"strings"
)

// Runner executes a command somewhere. The indirection is what lets collectors
// be tested without a real repo, and later lets the same collector run over ssh.
type Runner interface {
	Run(cwd string, argv []string) (string, error)
}

type LocalRunner struct{}

func (LocalRunner) Run(cwd string, argv []string) (string, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	return strings.TrimRight(string(out), "\n"), err
}
