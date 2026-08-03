package workspace

import (
	"sort"
	"time"
)

// GitRecord is one facet contribution from a git collector. A record with no
// Worktree still creates a workspace: a branch that exists but is not checked
// out anywhere is a legitimate thing to act on.
type GitRecord struct {
	ID           ID
	RepoLabel    string
	RepoFullName string
	SSHTarget    string
	Upstream     string
	Ahead        int
	Behind       int
	CommittedAt  *time.Time
	Worktree     *Worktree
}

// PRRecord is a pull request keyed the way GitHub describes it, by repository
// full name and head branch. It carries no host: a pull request is a fact about
// a branch, not about a machine.
type PRRecord struct {
	// FullName is the repository the pull request was opened against, and
	// HeadFullName is where the branch lives. They differ for a fork workflow:
	// a PR from Khady/dune to ocaml/dune has FullName "ocaml/dune" and
	// HeadFullName "Khady/dune", and the local clone's origin is the fork — so
	// matching only FullName would miss every fork-based pull request.
	FullName     string
	HeadFullName string
	Branch       string
	PR           PR
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
			byID[record.ID] = &Workspace{ID: record.ID, RepoLabel: record.RepoLabel,
				RepoFullName: record.RepoFullName, SSHTarget: record.SSHTarget,
				Upstream: record.Upstream, Ahead: record.Ahead, Behind: record.Behind,
				CommittedAt: record.CommittedAt,
				Worktree:    copyWorktree(record.Worktree)}
			order = append(order, record.ID)
			continue
		}
		if existing.Worktree == nil {
			existing.Worktree = copyWorktree(record.Worktree)
		}
		if existing.RepoLabel == "" {
			existing.RepoLabel = record.RepoLabel
		}
		if existing.RepoFullName == "" {
			existing.RepoFullName = record.RepoFullName
		}
		if existing.SSHTarget == "" {
			existing.SSHTarget = record.SSHTarget
		}
		if existing.Upstream == "" {
			existing.Upstream = record.Upstream
			existing.Ahead = record.Ahead
			existing.Behind = record.Behind
		}
		if record.CommittedAt != nil && (existing.CommittedAt == nil || record.CommittedAt.After(*existing.CommittedAt)) {
			existing.CommittedAt = record.CommittedAt
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
	// Recency first: the branch touched an hour ago belongs at the top, whatever
	// the alphabet says. Undated workspaces sort after dated ones so a source that
	// reports no timestamps still lists predictably.
	sort.SliceStable(workspaces, func(i, j int) bool {
		a, b := workspaces[i], workspaces[j]
		if (a.CommittedAt == nil) != (b.CommittedAt == nil) {
			return a.CommittedAt != nil
		}
		if a.CommittedAt != nil && !a.CommittedAt.Equal(*b.CommittedAt) {
			return a.CommittedAt.After(*b.CommittedAt)
		}
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

// copyWorktree keeps Index honest about being a pure merge: the returned
// snapshot must not alias facet structs the caller still holds.
func copyWorktree(worktree *Worktree) *Worktree {
	if worktree == nil {
		return nil
	}
	copied := *worktree
	return &copied
}

// AttachPRs adds the pull request facet to every workspace whose repository and
// branch a pull request names.
//
// It is separate from Index because pull requests come from a different source
// with a different cost: GitHub is the only network call in the system, so it
// must never gate the git and tmux facts, and a stale or failed PR fetch has to
// degrade to "no pr facet" rather than to no snapshot.
func AttachPRs(snap Snapshot, prs []PRRecord) Snapshot {
	for _, record := range prs {
		if record.Branch == "" || (record.FullName == "" && record.HeadFullName == "") {
			continue
		}
		for i := range snap.Workspaces {
			ws := &snap.Workspaces[i]
			if ws.ID.Branch != record.Branch || ws.RepoFullName == "" {
				continue
			}
			if ws.RepoFullName != record.FullName && ws.RepoFullName != record.HeadFullName {
				continue
			}
			pr := record.PR
			ws.PR = &pr
		}
	}
	return snap
}
