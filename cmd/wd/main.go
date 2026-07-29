// Command wd is the first frontend over the workdash core: it lists workspaces
// and runs actions on them. It exists before any TUI on purpose — if the core
// is not pleasant to drive from a shell script, no amount of rendering will fix
// it, and keeping a CLI client first-class is what stops workdash from becoming
// the only way to do these things.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Khady/workdash/internal/action"
	"github.com/Khady/workdash/internal/collect"
	"github.com/Khady/workdash/internal/launch"
	"github.com/Khady/workdash/internal/snapshot"
	"github.com/Khady/workdash/internal/wdconf"
	"github.com/Khady/workdash/internal/workspace"
)

// prPolicy separates "old enough to refresh" from "too old to show".
//

const usage = `usage:
  wd ls [--json]              list workspaces
  wd actions <selector>       list actions available for a workspace
  wd run <selector> <action>  run an action on a workspace
  wd refresh                  refetch pull requests now

<selector> is a branch name, or repo:branch when a branch exists in several repos.`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "wd: "+err.Error())
		os.Exit(1)
	}
}

func run(argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("%s", usage)
	}
	cfg, err := wdconf.Load(wdconf.DefaultPath())
	if err != nil {
		return err
	}
	snap, warnings := snapshot.Load(cfg)
	for _, warning := range warnings {
		fmt.Fprintln(os.Stderr, "wd: "+warning)
	}
	if snapshotFailed(len(snap.Workspaces), warnings) {
		return fmt.Errorf("no workspaces collected; every source failed (see warnings above)")
	}

	switch argv[0] {
	case "ls":
		if len(argv) > 1 && argv[1] == "--json" {
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(snap)
		}
		printSnapshot(os.Stdout, snap)
		return nil

	case "refresh":
		// Refetch every account with the caller waiting. Run by hand when you have
		// just opened a pull request, and by the detached background refresh.
		fetched := 0
		for _, account := range snapshot.AccountsToFetch(cfg) {
			records, err := collect.PRs(collect.LocalRunner{}, account)
			if err != nil {
				fmt.Fprintf(os.Stderr, "wd: %s: %v\n", snapshot.AccountLabel(account), err)
				continue
			}
			snapshot.CacheFor(cfg).Store(account, records)
			fetched += len(records)
		}
		fmt.Printf("refreshed %d pull requests\n", fetched)
		return nil

	case "actions":
		if len(argv) < 2 {
			return fmt.Errorf("usage: wd actions <selector>")
		}
		ws, err := Select(snap, argv[1])
		if err != nil {
			return err
		}
		for _, resolved := range action.Resolve(ws, cfg.Actions) {
			fmt.Printf("%-12s %-24s %s\n", resolved.ID, resolved.Label, strings.Join(resolved.Argv, " "))
		}
		return nil

	case "run":
		if len(argv) < 3 {
			return fmt.Errorf("usage: wd run <selector> <action>")
		}
		ws, err := Select(snap, argv[1])
		if err != nil {
			return err
		}
		available := action.Resolve(ws, cfg.Actions)
		for _, resolved := range available {
			if resolved.ID != argv[2] {
				continue
			}
			executor, err := executorFor(cfg, resolved.Host)
			if err != nil {
				return err
			}
			out, err := execute(executor, resolved)
			if err != nil {
				return err
			}
			if out != "" {
				fmt.Println(out)
			}
			return nil
		}
		return fmt.Errorf("action %q is not available for %q (available: %s)",
			argv[2], argv[1], strings.Join(actionIDs(available), ", "))
	}
	return fmt.Errorf("unknown command %q\n\n%s", argv[0], usage)
}

// executorFor picks how to reach a workspace's host. The local machine runs tmux
// directly; a remote one creates the window over ssh and attaches to it from a
// local tmux window, so remote work lands in the same window list as local work.
func executorFor(cfg wdconf.Config, host string) (launch.Executor, error) {
	local := launch.Tmux{Runner: collect.LocalRunner{}, InsideTmux: os.Getenv("TMUX") != ""}
	if host == wdconf.LocalHost {
		return local, nil
	}
	for _, configured := range cfg.Hosts {
		if configured.Label != host {
			continue
		}
		return launch.Remote{
			Runner: collect.LocalRunner{},
			Target: configured.SSHTarget,
			Attach: configured.AttachArgv,
			Local:  local,
		}, nil
	}
	return nil, fmt.Errorf("unknown host %q", host)
}

// execute takes the executor as a parameter rather than building one, so the
// where-dispatch is testable without touching tmux.
func execute(executor launch.Executor, resolved action.Resolved) (string, error) {
	if resolved.Where == wdconf.WhereCapture {
		return executor.Capture(resolved)
	}
	return executor.Pane(resolved)
}

// snapshotFailed distinguishes "nothing to show" from "every collector broke".
// Without it `wd ls --json | jq` cannot tell an empty list from a broken git.
func snapshotFailed(workspaces int, warnings []string) bool {
	return workspaces == 0 && len(warnings) > 0
}

func actionIDs(resolved []action.Resolved) []string {
	ids := []string{}
	for _, item := range resolved {
		ids = append(ids, item.ID)
	}
	if len(ids) == 0 {
		return []string{"none"}
	}
	return ids
}

func printSnapshot(out io.Writer, snap workspace.Snapshot) {
	for _, ws := range snap.Workspaces {
		facets := strings.Join(ws.Facets(), ",")
		if facets == "" {
			facets = "-"
		}
		names := []string{}
		for _, session := range ws.Sessions {
			names = append(names, fmt.Sprintf("%s: %d windows", session.Name, session.Windows))
		}
		detail := ""
		if len(names) > 0 {
			detail = "  [" + strings.Join(names, "; ") + "]"
		}
		fmt.Fprintf(out, "%-8s %-20s %-34s %-18s%s\n", ws.ID.Host, ws.RepoLabel, ws.ID.Branch, facets, detail)
	}
	for _, orphan := range snap.Orphans {
		fmt.Fprintf(out, "%-8s %-20s %-34s %-18s\n", orphan.Host, "(unlinked)", orphan.Session.Name, "session")
	}
}
