package collect

import (
	"strconv"
	"strings"

	"github.com/Khady/workdash/internal/workspace"
)

var tmuxArgv = []string{"tmux", "list-sessions", "-F",
	"#{session_name}\t#{session_path}\t#{session_windows}\t#{session_attached}"}

// Tmux lists sessions on a host. No tmux server is the common case on a fresh
// machine, not a failure, so it reports an empty list rather than an error.
//
// Everything else must surface. tmux exits 1 for unrelated faults too — an
// unwritable socket directory, for one — and reporting those as "no sessions"
// silently strips the session facet from every workspace, which the user reads
// as their sessions having died.
func Tmux(runner Runner, host string) ([]workspace.SessionRecord, error) {
	out, err := runner.Run("", tmuxArgv)
	if err != nil {
		if noServer(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseSessions(out, host), nil
}

// parseSessions reads `tmux list-sessions -F`. Fields are tab separated because
// session names routinely contain spaces.
func parseSessions(out, host string) []workspace.SessionRecord {
	records := []workspace.SessionRecord{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) < 4 || fields[0] == "" {
			continue
		}
		windows, _ := strconv.Atoi(fields[2])
		records = append(records, workspace.SessionRecord{
			Host: host,
			Session: workspace.Session{
				Name:     fields[0],
				Path:     fields[1],
				Windows:  windows,
				Attached: fields[3] != "0",
			},
		})
	}
	return records
}

// noServer reports whether a failed `tmux list-sessions` means only that there
// is no server to talk to. tmux says so on stderr — "no server running on
// <socket>", or a connect error naming the absent socket — never on stdout, and
// the exit status is 1 either way, so the message text is the only signal.
func noServer(err error) bool {
	text := Stderr(err)
	if text == "" {
		// A Runner that does not capture stderr, such as one tunnelling over
		// ssh, still folds the message into its error.
		text = err.Error()
	}
	text = strings.ToLower(text)
	return strings.Contains(text, "no server running") ||
		strings.Contains(text, "no such file or directory")
}
