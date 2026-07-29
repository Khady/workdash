package main

// The palette is lifted from internal/ui so the two frontends read as the same
// tool: a green worktree, an amber tmux session and a rose pull request mean the
// same thing here as they do in the old dashboard.
const (
	// Facet identity. These are internal/ui.kindColor's worktree, tmux and PR
	// hues, which is what makes the badge column recognisable at a glance.
	colorWorktree = "#34d399"
	colorSession  = "#f59e0b"
	colorPR       = "#f43f5e"

	// Pull request state, from internal/ui.prStatusPart: green open, blue draft,
	// purple merged, red closed.
	colorPROpen   = "#22c55e"
	colorPRDraft  = "#60a5fa"
	colorPRMerged = "#a855f7"
	colorPRClosed = "#ef4444"

	// colorAbsent is the muted grey internal/ui used for facts that are context
	// rather than signal: an absent facet, an age, a window count.
	colorAbsent = "#9ca3af"
	colorMuted  = "#9ca3af"

	// colorText is the branch column: the one thing that is always meaningful, so
	// it gets full contrast and no hue of its own.
	colorText = "#ffffff"
)
