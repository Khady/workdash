package workspace

import "sort"

// GitRecord is one facet contribution from a git collector. A record with no
// Worktree still creates a workspace: a branch that exists but is not checked
// out anywhere is a legitimate thing to act on.
type GitRecord struct {
	ID        ID
	RepoLabel string
	Worktree  *Worktree
}

type SessionRecord struct {
	Host    string  `json:"host"`
	Session Session `json:"session"`
}

// Snapshot is the merged view. Orphans are sessions that match no known
// worktree — a session for a deleted worktree, or one started somewhere
// workdash does not track. They are reported rather than dropped.
type Snapshot struct {
	Workspaces []Workspace     `json:"workspaces"`
	Orphans    []SessionRecord `json:"orphans"`
}

// Index merges facet records into workspaces. Sessions are joined to
// workspaces by worktree path rather than by any workdash-owned bookkeeping,
// so a session created by hand is indistinguishable from one workdash started.
func Index(git []GitRecord, sessions []SessionRecord) Snapshot {
	order := []ID{}
	byID := map[ID]*Workspace{}
	for _, record := range git {
		existing, ok := byID[record.ID]
		if !ok {
			byID[record.ID] = &Workspace{ID: record.ID, RepoLabel: record.RepoLabel, Worktree: record.Worktree}
			order = append(order, record.ID)
			continue
		}
		if existing.Worktree == nil {
			existing.Worktree = record.Worktree
		}
		if existing.RepoLabel == "" {
			existing.RepoLabel = record.RepoLabel
		}
	}

	orphans := []SessionRecord{}
	for _, record := range sessions {
		var target *Workspace
		for _, id := range order {
			ws := byID[id]
			if ws.ID.Host != record.Host {
				continue
			}
			if ws.Worktree != nil && ws.Worktree.Path == record.Session.Path {
				target = ws
				break
			}
		}
		if target == nil {
			orphans = append(orphans, record)
			continue
		}
		target.Sessions = append(target.Sessions, record.Session)
	}

	workspaces := make([]Workspace, 0, len(order))
	for _, id := range order {
		workspaces = append(workspaces, *byID[id])
	}
	sort.SliceStable(workspaces, func(i, j int) bool {
		a, b := workspaces[i], workspaces[j]
		if a.ID.Host != b.ID.Host {
			return a.ID.Host < b.ID.Host
		}
		if a.RepoLabel != b.RepoLabel {
			return a.RepoLabel < b.RepoLabel
		}
		return a.ID.Branch < b.ID.Branch
	})
	sort.SliceStable(orphans, func(i, j int) bool {
		return orphans[i].Session.Name < orphans[j].Session.Name
	})
	return Snapshot{Workspaces: workspaces, Orphans: orphans}
}
