// Command wd is the first frontend over the workdash core: it lists workspaces
// and runs actions on them. It exists before any TUI on purpose — if the core
// is not pleasant to drive from a shell script, no amount of rendering will fix
// it, and keeping a CLI client first-class is what stops workdash from becoming
// the only way to do these things.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

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

	switch argv[0] {
	case "ls":
		if len(argv) > 1 && argv[1] == "--json" {
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(snap)
		}
		printSnapshot(snap)
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
			return execute(resolved)
		}
		return fmt.Errorf("action %q is not available for %q (available: %s)",
			argv[2], argv[1], strings.Join(actionIDs(available), ", "))
	}
	return fmt.Errorf("unknown command %q\n\n%s", argv[0], usage)
}

func execute(resolved action.Resolved) error {
	executor := launch.Tmux{Runner: collect.LocalRunner{}, InsideTmux: os.Getenv("TMUX") != ""}
	var out string
	var err error
	if resolved.Where == wdconf.WhereCapture {
		out, err = executor.Capture(resolved)
	} else {
		out, err = executor.Pane(resolved)
	}
	if err != nil {
		return err
	}
	if out != "" {
		fmt.Println(out)
	}
	return nil
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

func loadSnapshot(cfg wdconf.Config) (workspace.Snapshot, []string) {
	runner := collect.LocalRunner{}
	warnings := []string{}
	records := []workspace.GitRecord{}
	for _, repo := range cfg.Repos {
		got, err := collect.Git(runner, "local", collect.Repo{Root: repo.Root, Label: repo.Label})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", repo.Label, err))
			continue
		}
		records = append(records, got...)
	}
	sessions, err := collect.Tmux(runner, "local")
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("tmux: %v", err))
	}
	return workspace.Index(records, sessions), warnings
}

func printSnapshot(snap workspace.Snapshot) {
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
		fmt.Printf("%-8s %-20s %-34s %-18s%s\n", ws.ID.Host, ws.RepoLabel, ws.ID.Branch, facets, detail)
	}
	for _, orphan := range snap.Orphans {
		fmt.Printf("%-8s %-20s %-34s %-18s\n", orphan.Host, "(unlinked)", orphan.Session.Name, "session")
	}
}
