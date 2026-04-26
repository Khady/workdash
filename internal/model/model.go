package model

import (
	"fmt"
	"time"
)

type Mode string
type SortOrder string
type ItemKind string
type PRStatus string

const (
	ModeAll      Mode      = "all"
	ModePR       Mode      = "prs"
	ModeWorktree Mode      = "worktrees"
	ModeBranch   Mode      = "branches"
	ModeTmux     Mode      = "tmux"
	SortDefault  SortOrder = "default"
	SortRecent   SortOrder = "recent"
	KindPR       ItemKind  = "pr"
	KindWorktree ItemKind  = "worktree"
	KindBranch   ItemKind  = "branch"
	KindTmux     ItemKind  = "tmux"
	PROpen       PRStatus  = "open"
	PRMerged     PRStatus  = "merged"
	PRClosed     PRStatus  = "closed"
)

type ShellAction interface {
	ToShell() string
}

type WorktreeRecord struct {
	RepoRoot       string
	RepoLabel      string
	RepoFullName   string
	Path           string
	Branch         string
	Head           string
	IsMain         bool
	LastActivityAt *time.Time
	PRNumber       int
	PRURL          string
	PRIsDraft      bool
	PRStatus       PRStatus
	Upstream       string
	AheadCount     *int
	HostLabel      string
	SSHTarget      string
}

type BranchRecord struct {
	RepoRoot     string
	RepoLabel    string
	RepoFullName string
	Name         string
	CommittedAt  *time.Time
	ActivePath   string
	RecencyRank  int
	PRNumber     int
	PRURL        string
	PRIsDraft    bool
	PRStatus     PRStatus
	Upstream     string
	AheadCount   *int
	HostLabel    string
	SSHTarget    string
}

type TmuxSessionRecord struct {
	Name        string
	Windows     int
	Attached    int
	Path        string
	PathMissing bool
	WindowNames []string
	HostLabel   string
	SSHTarget   string
}

type WorkItem struct {
	Kind            ItemKind
	Title           string
	Subtitle        string
	RepoLabel       string
	RepoRoot        string
	RepoFullName    string
	Path            string
	Branch          string
	Session         string
	SearchText      string
	ScoreHint       int
	Action          ShellAction
	TmuxWindows     *int
	TmuxAttached    *int
	TmuxWindowNames []string
	PRNumber        int
	PRURL           string
	PRIsDraft       bool
	PRStatus        PRStatus
	LastActivityAt  *time.Time
	Upstream        string
	AheadCount      *int
	IsPrimary       bool
	HostLabel       string
	SSHTarget       string
	PathMissing     bool
}

func (i WorkItem) Key() string {
	switch {
	case i.Kind == KindWorktree && i.Path != "":
		return fmt.Sprintf("worktree:%s:%s", i.HostLabel, i.Path)
	case i.Kind == KindBranch && i.RepoRoot != "":
		return fmt.Sprintf("branch:%s:%s:%s", i.HostLabel, i.RepoRoot, i.Title)
	case i.Kind == KindPR && i.PRURL != "":
		return fmt.Sprintf("pr:%s:%s", i.HostLabel, i.PRURL)
	case i.Kind == KindTmux && i.Session != "":
		return fmt.Sprintf("tmux:%s:%s", i.HostLabel, i.Session)
	default:
		return fmt.Sprintf("%s:%s:%s", i.Kind, i.HostLabel, i.Title)
	}
}

type DisplayEntry struct {
	Label string
	Item  *WorkItem
}

func (d DisplayEntry) IsHeader() bool {
	return d.Item == nil
}
