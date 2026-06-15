package source

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/Khady/workdash/internal/actions"
	"github.com/Khady/workdash/internal/model"
)

var TmuxCommand = []string{"list-sessions", "-F", "#{session_name}\t#{session_windows}\t#{session_attached}\t#{session_path}"}
var TmuxAllWindowsCommand = []string{"list-windows", "-a", "-F", "#{session_name}\t#{window_index}:#{window_name}"}

func ParseTmuxSessions(text, hostLabel, sshTarget string) []model.TmuxSessionRecord {
	if hostLabel == "" {
		hostLabel = "local"
	}
	sessions := []model.TmuxSessionRecord{}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		windows, _ := strconv.Atoi(parts[1])
		attached, _ := strconv.Atoi(parts[2])
		path := ""
		if len(parts) > 3 {
			path = strings.TrimSpace(parts[3])
		}
		sessions = append(sessions, model.TmuxSessionRecord{
			Name:      parts[0],
			Windows:   windows,
			Attached:  attached,
			Path:      path,
			HostLabel: hostLabel,
			SSHTarget: sshTarget,
		})
	}
	return sessions
}

func ParseWindowListingBySession(text string) map[string][]string {
	out := map[string][]string{}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		session, window, ok := strings.Cut(line, "\t")
		if !ok || session == "" || strings.TrimSpace(window) == "" {
			continue
		}
		out[session] = append(out[session], strings.TrimSpace(window))
	}
	return out
}

func CollectTmuxItemsForHost(hostLabel, sshTarget string, runner TmuxRunner, insideTmux bool) ([]model.WorkItem, []string) {
	output, err := runner.RunTmux(TmuxCommand)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || isMissingTmuxServerError(err.Error()) {
			return nil, nil
		}
		return nil, []string{fmt.Sprintf("%s: %s", hostLabel, err)}
	}
	sessions := ParseTmuxSessions(output, hostLabel, sshTarget)
	windowsBySession := collectWindowNamesBySession(runner)
	items := make([]model.WorkItem, 0, len(sessions))
	for _, session := range sessions {
		if windows := windowsBySession[session.Name]; len(windows) > 0 {
			session.WindowNames = windows
		}
		if session.SSHTarget == "" && session.Path != "" {
			if _, err := os.Stat(session.Path); err != nil {
				session.PathMissing = true
			}
		}
		items = append(items, sessionToItem(session, insideTmux))
	}
	return items, nil
}

func collectWindowNamesBySession(runner TmuxRunner) map[string][]string {
	text, err := runner.RunTmux(TmuxAllWindowsCommand)
	if err != nil {
		return map[string][]string{}
	}
	return ParseWindowListingBySession(text)
}

func EnrichTmuxItems(items []model.WorkItem) []model.WorkItem {
	type pathKey struct {
		hostLabel string
		sshTarget string
		path      string
	}
	keyFor := func(item model.WorkItem) pathKey {
		return pathKey{hostLabel: item.HostLabel, sshTarget: item.SSHTarget, path: item.Path}
	}

	linkedByPath := map[pathKey]model.WorkItem{}
	tmuxByPath := map[pathKey]bool{}
	for _, item := range items {
		if (item.Kind == model.KindWorktree || item.Kind == model.KindBranch) && item.Path != "" {
			linkedByPath[keyFor(item)] = item
		}
		if item.Kind == model.KindTmux && item.Path != "" {
			tmuxByPath[keyFor(item)] = true
		}
	}
	out := make([]model.WorkItem, len(items))
	for i, item := range items {
		if item.Kind == model.KindWorktree {
			item.HasTmuxSession = tmuxByPath[keyFor(item)]
		}
		if item.Kind != model.KindTmux {
			out[i] = item
			continue
		}
		if item.SSHTarget == "" && item.Path != "" {
			if _, err := os.Stat(item.Path); err != nil {
				item.PathMissing = true
			}
		}
		if linked, ok := linkedByPath[keyFor(item)]; ok {
			item.PRNumber = linked.PRNumber
			item.PRURL = linked.PRURL
			item.PRIsDraft = linked.PRIsDraft
			item.PRStatus = linked.PRStatus
		}
		out[i] = item
	}
	return out
}

func sessionToItem(session model.TmuxSessionRecord, insideTmux bool) model.WorkItem {
	windows := session.Windows
	attached := session.Attached
	return model.WorkItem{
		Kind:            model.KindTmux,
		Title:           session.Name,
		Subtitle:        fmt.Sprintf("%s • %d windows • %d attached", session.HostLabel, session.Windows, session.Attached),
		Path:            session.Path,
		Session:         session.Name,
		SearchText:      strings.Join([]string{session.Name, "tmux", "session", session.HostLabel, session.Path}, " "),
		ScoreHint:       300 + session.Attached*25,
		Action:          actions.TmuxAction{Session: session.Name, InsideTmux: insideTmux, SSHTarget: session.SSHTarget},
		TmuxWindows:     &windows,
		TmuxAttached:    &attached,
		TmuxWindowNames: session.WindowNames,
		HostLabel:       session.HostLabel,
		SSHTarget:       session.SSHTarget,
		PathMissing:     session.PathMissing,
	}
}

func isMissingTmuxServerError(message string) bool {
	normalized := strings.ToLower(message)
	return strings.Contains(normalized, "no server running") ||
		strings.Contains(normalized, "failed to connect to server") ||
		(strings.Contains(normalized, "error connecting to") && strings.Contains(normalized, "no such file or directory"))
}
