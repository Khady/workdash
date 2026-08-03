package main

import (
	"fmt"
	"strings"

	"github.com/Khady/workdash/internal/action"
	"github.com/Khady/workdash/internal/wdconf"
	"github.com/Khady/workdash/internal/workspace"
)

// group is one workspace's section in the popup. The field names are the ones
// popup.js already reads, so the rewritten host is a drop-in replacement for the
// daemon-backed one: only what fills them changed.
type group struct {
	ItemKey  string          `json:"item_key"`
	Kind     string          `json:"kind"`
	Title    string          `json:"title"`
	Subtitle string          `json:"subtitle"`
	Host     string          `json:"host"`
	Path     string          `json:"path,omitempty"`
	Actions  []browserAction `json:"actions"`
}

type browserAction struct {
	Token   string `json:"token,omitempty"`
	Label   string `json:"label"`
	Detail  string `json:"detail,omitempty"`
	Enabled bool   `json:"enabled"`
	// UnavailableReason and the confirmation fields are part of the shape popup.js
	// reads. Nothing sets them here: the core resolves only the actions that
	// currently apply, so an action that reaches the popup is runnable, and the new
	// action language has no confirmation flag yet. They stay so adding either is a
	// host change and not an extension change.
	UnavailableReason  string `json:"unavailable_reason,omitempty"`
	ConfirmationTitle  string `json:"confirmation_title,omitempty"`
	ConfirmationDetail string `json:"confirmation_detail,omitempty"`
	ConfirmLabel       string `json:"confirm_label,omitempty"`
}

// matchingWorkspaces lists every workspace carrying this pull request, in
// snapshot order — which is recency order, so the branch you touched last is the
// first section in the popup.
//
// A pull request is a fact about a branch rather than about a machine, so it
// legitimately matches several workspaces: the local clone and the devbox one both
// have it, and both are things you might want to act on from the PR page.
func matchingWorkspaces(snap workspace.Snapshot, canonical string) []workspace.Workspace {
	matches := []workspace.Workspace{}
	for _, ws := range snap.Workspaces {
		if !carriesPR(ws, canonical) {
			continue
		}
		matches = append(matches, ws)
	}
	return matches
}

// carriesPR compares canonicalized forms on both sides. The snapshot's URL comes
// from GitHub and the other from the address bar, and they differ in case and in
// the trailing tab segment for the same pull request.
func carriesPR(ws workspace.Workspace, canonical string) bool {
	if ws.PR == nil {
		return false
	}
	own, err := canonicalPRURL(ws.PR.URL)
	return err == nil && own == canonical
}

func groupsForPR(snap workspace.Snapshot, defs []wdconf.Action, canonical string) []group {
	groups := []group{}
	for _, ws := range matchingWorkspaces(snap, canonical) {
		actions := []browserAction{}
		for _, resolved := range action.Resolve(ws, defs) {
			actions = append(actions, browserAction{
				Token:   encodeToken(actionRef{ID: ws.ID, Action: resolved.ID, PRURL: canonical}),
				Label:   actionLabel(resolved),
				Detail:  actionDetail(resolved),
				Enabled: true,
			})
		}
		// A section with a heading and no buttons tells the reader nothing, so a
		// workspace with no applicable action is simply not a section.
		if len(actions) == 0 {
			continue
		}
		groups = append(groups, group{
			ItemKey:  ws.ID.String(),
			Kind:     groupKind(ws),
			Title:    ws.ID.Branch,
			Subtitle: ws.RepoLabel,
			Host:     ws.ID.Host,
			Path:     worktreePath(ws),
			Actions:  actions,
		})
	}
	return groups
}

// groupKind feeds the popup's heading, which renders "worktree" as "workspace".
// A branch checked out nowhere is reported as a branch, because that is the state
// the offered actions follow from.
func groupKind(ws workspace.Workspace) string {
	if ws.Worktree != nil {
		return workspace.FacetWorktree
	}
	return "branch"
}

func worktreePath(ws workspace.Workspace) string {
	if ws.Worktree == nil {
		return ""
	}
	return ws.Worktree.Path
}

func actionLabel(resolved action.Resolved) string {
	if resolved.Label != "" {
		return resolved.Label
	}
	return resolved.ID
}

// actionDetail is the second line on the button: the command that will actually
// run, so a click is never a mystery. `attach` has no argv by definition.
func actionDetail(resolved action.Resolved) string {
	if len(resolved.Argv) == 0 {
		if resolved.Session != "" {
			return "attach to " + resolved.Session
		}
		return resolved.Where
	}
	return strings.Join(resolved.Argv, " ")
}

func countActions(groups []group) int {
	count := 0
	for _, group := range groups {
		count += len(group.Actions)
	}
	return count
}

// summaryForPR is the one-line answer to "what does workdash know about this pull
// request": which host and repo have the branch, and which facets each one has.
// The badge only needs the count, so this exists for the popup and for anyone
// driving the host by hand.
func summaryForPR(snap workspace.Snapshot, canonical string) string {
	matches := matchingWorkspaces(snap, canonical)
	if len(matches) == 0 {
		return "workdash knows no workspace for this pull request"
	}
	places := []string{}
	for _, ws := range matches {
		facets := strings.Join(ws.Facets(), ", ")
		if facets == "" {
			facets = "no facets"
		}
		places = append(places, fmt.Sprintf("%s:%s [%s]", ws.ID.Host, ws.RepoLabel, facets))
	}
	return fmt.Sprintf("workdash knows PR #%d as branch %q on %s",
		matches[0].PR.Number, matches[0].ID.Branch, strings.Join(places, ", "))
}

// resolveRef turns a token back into a runnable action against the *current*
// snapshot. Every refusal here is a state change since the popup was drawn, and
// each one is worth its own message because they mean different things to the
// reader: the repo left the config, the pull request moved off the branch, or the
// facet the action needed is gone.
func resolveRef(snap workspace.Snapshot, defs []wdconf.Action, ref actionRef) (action.Resolved, error) {
	for _, ws := range snap.Workspaces {
		if ws.ID != ref.ID {
			continue
		}
		if !carriesPR(ws, ref.PRURL) {
			return action.Resolved{}, fmt.Errorf("that pull request is no longer on %s; reload the page", selectorOf(ws))
		}
		for _, resolved := range action.Resolve(ws, defs) {
			if resolved.ID == ref.Action {
				return resolved, nil
			}
		}
		return action.Resolved{}, fmt.Errorf("action %q no longer applies to %s: its facets changed since the popup was opened",
			ref.Action, selectorOf(ws))
	}
	return action.Resolved{}, fmt.Errorf("workdash no longer knows a %q branch in %s on %s",
		ref.ID.Branch, ref.ID.Repo, ref.ID.Host)
}

// selectorOf spells a workspace the way `wd` names it, so an error message from
// the popup is something you can paste into a shell.
func selectorOf(ws workspace.Workspace) string {
	return ws.ID.Host + ":" + ws.RepoLabel + ":" + ws.ID.Branch
}
