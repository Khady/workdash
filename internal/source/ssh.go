package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Khady/workdash/internal/actions"
	"github.com/Khady/workdash/internal/logging"
)

const SentinelPrefix = "__WORKDASH_REMOTE_EXIT__:"

var SSHFailFastOptions = []string{"-A", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5", "-o", "StrictHostKeyChecking=yes"}
var SSHTimeout = 10 * time.Second

type HostConnectionError struct {
	Detail   string
	ExitCode *int
	Stderr   string
	Stdout   string
}

func (e HostConnectionError) Error() string { return e.Detail }

type HostCommandError struct {
	Detail   string
	ExitCode int
	Stderr   string
	Stdout   string
}

func (e HostCommandError) Error() string { return e.Detail }

type GitRunner interface {
	RunGit(repoRoot string, args []string) (string, error)
	RunGH(repoRoot string, args []string, ghToken string) (string, error)
	RunGHAuth(args []string) (string, error)
}

type TmuxRunner interface {
	RunTmux(args []string) (string, error)
}

type LocalGitRunner struct{}

func (LocalGitRunner) RunGit(repoRoot string, args []string) (string, error) {
	return runLocal(append([]string{"git", "-C", repoRoot}, args...), "", nil)
}

func (LocalGitRunner) RunGH(repoRoot string, args []string, ghToken string) (string, error) {
	env := map[string]string(nil)
	if ghToken != "" {
		env = map[string]string{"GH_TOKEN": ghToken}
	}
	return runLocal(append([]string{"gh"}, args...), repoRoot, env)
}

func (LocalGitRunner) RunGHAuth(args []string) (string, error) {
	return runLocal(append([]string{"gh", "auth"}, args...), "", nil)
}

type LocalTmuxRunner struct{}

func (LocalTmuxRunner) RunTmux(args []string) (string, error) {
	return runLocal(append([]string{"tmux"}, args...), "", map[string]string{"TERM": getenvDefault("TERM", "dumb")})
}

type SSHRunner struct{ SSHTarget string }

func NewSSHRunner(target string) SSHRunner { return SSHRunner{SSHTarget: target} }

func (r SSHRunner) BuildGitCommand(repoRoot string, args []string) string {
	return joinShellWords(append([]string{"git", "-C", repoRoot}, args...))
}

func (r SSHRunner) BuildTmuxCommand(args []string) string {
	return joinShellWords(append([]string{"tmux"}, args...))
}

func (r SSHRunner) BuildGHCommand(repoRoot string, args []string, ghToken string) string {
	words := []string{"env"}
	if ghToken != "" {
		words = append(words, "GH_TOKEN="+ghToken)
	}
	if repoRoot != "" {
		words = append(words, "GIT_DIR="+repoRoot+"/.git", "GIT_WORK_TREE="+repoRoot)
	}
	words = append(words, "gh")
	words = append(words, args...)
	return joinShellWords(words)
}

func (r SSHRunner) RunGit(repoRoot string, args []string) (string, error) {
	return r.runRemote(r.BuildGitCommand(repoRoot, args))
}

func (r SSHRunner) RunTmux(args []string) (string, error) {
	return r.runRemote(r.BuildTmuxCommand(args))
}

func (r SSHRunner) RunGH(repoRoot string, args []string, ghToken string) (string, error) {
	return r.runRemote(r.BuildGHCommand(repoRoot, args, ghToken))
}

func (r SSHRunner) RunGHAuth(args []string) (string, error) {
	return r.runRemote(joinShellWords(append([]string{"gh", "auth"}, args...)))
}

func (r SSHRunner) runRemote(command string) (string, error) {
	wrapped := wrapRemoteCommand(command)
	ctx, cancel := context.WithTimeout(context.Background(), SSHTimeout)
	defer cancel()
	args := append([]string{}, SSHFailFastOptions...)
	args = append(args, r.SSHTarget, "env", "BASH_ENV=/dev/null", "bash", "--noprofile", "--norc", "-c", actions.Quote(wrapped))
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	scope := "ssh:" + r.SSHTarget
	logging.CommandStart(scope, command)
	out, err := cmd.Output()
	stderrText := stderr.String()
	if ctx.Err() != nil {
		logging.CommandEnd(scope, command, nil, string(out), stderrText, ctx.Err())
		return "", HostConnectionError{Detail: "ssh timed out", Stderr: stderrText, Stdout: string(out)}
	}
	if err != nil {
		exitCode := 1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		}
		detail := firstNonEmpty(strings.TrimSpace(stderrText), strings.TrimSpace(string(out)), "ssh failed")
		hostErr := HostConnectionError{Detail: detail, ExitCode: &exitCode, Stderr: stderrText, Stdout: string(out)}
		logging.CommandEnd(scope, command, &exitCode, string(out), stderrText, hostErr)
		return "", hostErr
	}
	payload, code, err := SplitSentinel(string(out), stderrText)
	if err != nil {
		logging.CommandEnd(scope, command, nil, string(out), stderrText, err)
		return "", err
	}
	if code != 0 {
		detail := firstNonEmpty(strings.TrimSpace(payload), strings.TrimSpace(stderrText), fmt.Sprintf("remote command failed with exit %d", code))
		hostErr := HostCommandError{Detail: detail, ExitCode: code, Stderr: stderrText, Stdout: payload}
		logging.CommandEnd(scope, command, &code, payload, stderrText, hostErr)
		return "", hostErr
	}
	logging.CommandEnd(scope, command, &code, payload, stderrText, nil)
	return payload, nil
}

func runLocal(command []string, cwd string, env map[string]string) (string, error) {
	cmd := exec.Command(command[0], command[1:]...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if cwd != "" {
		cmd.Dir = cwd
	}
	if env != nil {
		cmd.Env = os.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	commandText := strings.Join(command, " ")
	scope := firstNonEmpty(cwd, "local")
	logging.CommandStart(scope, commandText)
	out, err := cmd.Output()
	stderrText := stderr.String()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			logging.CommandEnd(scope, commandText, nil, string(out), stderrText, err)
			return "", err
		}
		exitCode := 1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		}
		runErr := fmt.Errorf("%s", firstNonEmpty(strings.TrimSpace(stderrText), strings.TrimSpace(string(out)), "command failed"))
		logging.CommandEnd(scope, commandText, &exitCode, string(out), stderrText, runErr)
		return "", runErr
	}
	exitCode := 0
	logging.CommandEnd(scope, commandText, &exitCode, string(out), stderrText, nil)
	return string(out), nil
}

func wrapRemoteCommand(command string) string {
	return `export PATH="/usr/local/bin:/usr/bin:/bin:${HOME}/.local/bin:${HOME}/bin:${PATH:-}"; export TERM=${TERM:-dumb}; ` +
		command + `; status=$?; printf ` + actions.Quote(SentinelPrefix+"%s\n") + ` "$status"; exit 0`
}

func SplitSentinel(stdout, stderr string) (string, int, error) {
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	idxs := []int{}
	for i, line := range lines {
		if strings.HasPrefix(line, SentinelPrefix) {
			idxs = append(idxs, i)
		}
	}
	if len(idxs) != 1 || len(lines) == 0 || idxs[0] != len(lines)-1 {
		return "", 0, HostConnectionError{Detail: "missing exit sentinel", Stderr: stderr, Stdout: stdout}
	}
	code, err := strconv.Atoi(strings.TrimPrefix(lines[len(lines)-1], SentinelPrefix))
	if err != nil {
		return "", 0, HostConnectionError{Detail: "invalid exit sentinel", Stderr: stderr, Stdout: stdout}
	}
	payloadLines := lines[:len(lines)-1]
	payload := strings.Join(payloadLines, "\n")
	if len(payloadLines) > 0 {
		payload += "\n"
	}
	return payload, code, nil
}

func joinShellWords(words []string) string {
	quoted := make([]string, len(words))
	for i, word := range words {
		quoted[i] = actions.Quote(word)
	}
	return strings.Join(quoted, " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func getenvDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
