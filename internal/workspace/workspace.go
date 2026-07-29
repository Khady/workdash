// Package workspace holds the unit workdash reasons about: a branch on a repo
// on a host, plus whichever facets currently exist for it.
//
// Nothing here is authoritative. Worktrees live in git, sessions live in tmux,
// pull requests live on GitHub. A Workspace is a projection over those, so a
// missing facet is a normal state rather than an error: a branch whose tmux
// session died on reboot is a workspace without a session facet.
package workspace

import "fmt"

const (
	FacetWorktree = "worktree"
	FacetSession  = "session"
	FacetPR       = "pr"
)

// ID identifies a workspace. The branch is the stable part of the identity:
// worktree paths and session names come and go over a branch's lifetime.
type ID struct {
	Host   string `json:"host"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
}

func (id ID) String() string {
	return fmt.Sprintf("%s:%s@%s", id.Host, id.Repo, id.Branch)
}

type Worktree struct {
	Path   string `json:"path"`
	IsMain bool   `json:"is_main"`
	Head   string `json:"head"`
}

type Session struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Windows  int    `json:"windows"`
	Attached bool   `json:"attached"`
}

type PR struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"`
	Draft  bool   `json:"draft"`
}

// Sessions is a slice because one worktree legitimately hosts several: an agent
// in one, a shell in another. Collapsing them to a single session would report
// the extras as unrelated.
type Workspace struct {
	ID        ID        `json:"id"`
	RepoLabel string    `json:"repo_label"`
	Worktree  *Worktree `json:"worktree,omitempty"`
	Sessions  []Session `json:"sessions,omitempty"`
	PR        *PR       `json:"pr,omitempty"`
}

func (w Workspace) Has(facet string) bool {
	switch facet {
	case FacetWorktree:
		return w.Worktree != nil
	case FacetSession:
		return len(w.Sessions) > 0
	case FacetPR:
		return w.PR != nil
	}
	return false
}

func (w Workspace) Facets() []string {
	facets := []string{}
	for _, facet := range []string{FacetWorktree, FacetSession, FacetPR} {
		if w.Has(facet) {
			facets = append(facets, facet)
		}
	}
	return facets
}
