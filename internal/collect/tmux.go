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
func Tmux(runner Runner, host string) ([]workspace.SessionRecord, error) {
	out, err := runner.Run("", tmuxArgv)
	if err != nil {
		if strings.Contains(out, "no server running") || strings.Contains(err.Error(), "exit status 1") {
			return nil, nil
		}
		return nil, err
	}
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
	return records, nil
}
