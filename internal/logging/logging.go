package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var ghTokenPattern = regexp.MustCompile(`'?GH_TOKEN=[^'\s]+'?`)
var githubTokenPattern = regexp.MustCompile(`\b(?:gh[opsu]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,})\b`)
var writeMu sync.Mutex

const (
	maxLogSize    int64 = 50 * 1024 * 1024
	maxLogBackups       = 1
)

func DefaultLogPath() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "workdash", "workdash.log")
}

func Printf(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	appendLine(line)
}

func CommandStart(scope, command string) {
	Printf("command start scope=%s command=%q", scope, Redact(command))
}

func CommandEnd(scope, command string, exitCode *int, stdout, stderr string, err error) {
	status := "ok"
	if err != nil {
		status = "error"
	}
	exit := ""
	if exitCode != nil {
		exit = fmt.Sprintf(" exit=%d", *exitCode)
	}
	Printf("command end status=%s scope=%s%s command=%q stdout=%q stderr=%q error=%q",
		status, scope, exit, Redact(command), trimForLog(stdout), trimForLog(stderr), errString(err))
}

func Redact(value string) string {
	value = ghTokenPattern.ReplaceAllStringFunc(value, func(match string) string {
		prefix := ""
		suffix := ""
		if strings.HasPrefix(match, "'") {
			prefix = "'"
		}
		if strings.HasSuffix(match, "'") {
			suffix = "'"
		}
		return prefix + "GH_TOKEN=<redacted>" + suffix
	})
	return githubTokenPattern.ReplaceAllString(value, "<github-token-redacted>")
}

func appendLine(line string) {
	path := DefaultLogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	rotateIfNeeded(path)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, "%s %s\n", time.Now().Format(time.RFC3339Nano), line)
}

func rotateIfNeeded(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxLogSize {
		return
	}
	if maxLogBackups <= 0 {
		_ = os.Remove(path)
		return
	}
	for i := maxLogBackups - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", path, i)
		newPath := fmt.Sprintf("%s.%d", path, i+1)
		_ = os.Rename(oldPath, newPath)
	}
	_ = os.Rename(path, path+".1")
}

func trimForLog(value string) string {
	const limit = 12000
	value = Redact(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "...<truncated>"
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return Redact(err.Error())
}
