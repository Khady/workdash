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
	"sync"

	"github.com/Khady/workdash/internal/action"
	"github.com/Khady/workdash/internal/collect"
	"github.com/Khady/workdash/internal/launch"
	"github.com/Khady/workdash/internal/wdconf"
	"github.com/Khady/workdash/internal/workspace"
)

const usage = `usage:
  wd ls [--json]              list workspaces
  wd actions <selector>       list actions available for a workspace
  wd run <selector> <action>  run an action on a workspace

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
	snap, warnings := loadSnapshot(cfg)
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
			executor := launch.Tmux{Runner: collect.LocalRunner{}, InsideTmux: os.Getenv("TMUX") != ""}
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

// hostResult is one host's contribution. Errors are per host so an unreachable
// machine degrades its own section of the list and nothing else.
type hostResult struct {
	host     string
	git      []workspace.GitRecord
	sessions []workspace.SessionRecord
	err      error
}

// hostsFromConfig puts the local machine first. It is implied rather than
// configured, which is why `local` is a reserved label.
func hostsFromConfig(cfg wdconf.Config) []collect.Host {
	hosts := []collect.Host{{
		Label:       wdconf.LocalHost,
		Repos:       toCollectRepos(cfg.Repos),
		TmuxEnabled: true,
	}}
	for _, host := range cfg.Hosts {
		hosts = append(hosts, collect.Host{
			Label:       host.Label,
			SSHTarget:   host.SSHTarget,
			Repos:       toCollectRepos(host.Repos),
			TmuxEnabled: host.TmuxEnabled,
		})
	}
	return hosts
}

func toCollectRepos(repos []wdconf.Repo) []collect.Repo {
	out := make([]collect.Repo, 0, len(repos))
	for _, repo := range repos {
		out = append(out, collect.Repo{Root: repo.Root, Label: repo.Label})
	}
	return out
}

// loadSnapshot collects every host concurrently. A slow or unreachable host
// costs its own latency, not the sum of all of them.
func loadSnapshot(cfg wdconf.Config) (workspace.Snapshot, []string) {
	hosts := hostsFromConfig(cfg)
	results := make([]hostResult, len(hosts))
	var wg sync.WaitGroup
	for i, host := range hosts {
		wg.Add(1)
		go func(i int, host collect.Host) {
			defer wg.Done()
			git, sessions, err := collect.CollectHost(transportFor(host), host)
			results[i] = hostResult{host: host.Label, git: git, sessions: sessions, err: err}
		}(i, host)
	}
	wg.Wait()
	return mergeHostResults(results)
}

// transportFor is the whole local/remote distinction: an empty ssh target runs
// sections directly, anything else runs the entire plan in one ssh.
func transportFor(host collect.Host) collect.Transport {
	if host.SSHTarget == "" {
		return collect.Local{Runner: collect.LocalRunner{}}
	}
	return collect.SSH{Runner: collect.LocalRunner{}, Target: host.SSHTarget}
}

func mergeHostResults(results []hostResult) (workspace.Snapshot, []string) {
	git := []workspace.GitRecord{}
	sessions := []workspace.SessionRecord{}
	warnings := []string{}
	for _, result := range results {
		if result.err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", result.host, result.err))
			continue
		}
		git = append(git, result.git...)
		sessions = append(sessions, result.sessions...)
	}
	return workspace.Index(git, sessions), warnings
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
