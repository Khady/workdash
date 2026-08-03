// Package dispatch chooses how to reach a workspace's host and routes a
// resolved action to the right launch verb.
//
// It lives outside cmd/wd because every frontend has to run actions the same
// way: a TUI that grew its own copy of this dispatch would drift from the CLI,
// and "the picker does something slightly different" is exactly the class of bug
// the two-verb model exists to prevent.
package dispatch

import (
	"fmt"
	"os"

	"github.com/Khady/workdash/internal/action"
	"github.com/Khady/workdash/internal/collect"
	"github.com/Khady/workdash/internal/launch"
	"github.com/Khady/workdash/internal/wdconf"
)

// ExecutorFor picks how to reach a workspace's host. The local machine runs tmux
// directly; a remote one creates the window over ssh and attaches to it from a
// local tmux window, so remote work lands in the same window list as local work.
func ExecutorFor(cfg wdconf.Config, host string) (launch.Executor, error) {
	local := launch.Tmux{Runner: collect.LocalRunner{}, InsideTmux: os.Getenv("TMUX") != "", Terminal: cfg.Terminal}
	if host == wdconf.LocalHost {
		return local, nil
	}
	for _, configured := range cfg.Hosts {
		if configured.Label != host {
			continue
		}
		return launch.Remote{
			Runner:     collect.LocalRunner{},
			Target:     configured.SSHTarget,
			AttachArgv: configured.AttachArgv,
			Local:      local,
		}, nil
	}
	return nil, fmt.Errorf("unknown host %q", host)
}

// Host is where the command runs, which is not always where the workspace lives:
// a browser or a GUI editor belongs on this machine even for a remote workspace,
// which `hosts` cannot express — that says which workspaces an action applies to.
func Host(resolved action.Resolved) string {
	if resolved.RunOn == wdconf.RunOnLocal {
		return wdconf.LocalHost
	}
	return resolved.Host
}

// Execute takes the executor as a parameter rather than building one, so the
// where-dispatch is testable without touching tmux.
func Execute(executor launch.Executor, resolved action.Resolved) (string, error) {
	switch resolved.Where {
	case wdconf.WhereCapture:
		return executor.Capture(resolved)
	case wdconf.WhereAttach:
		return executor.Attach(resolved)
	case wdconf.WhereBackground:
		return executor.Background(resolved)
	}
	return executor.Pane(resolved)
}
