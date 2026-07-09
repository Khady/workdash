package daemon

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Khady/workdash/internal/actions"
	"github.com/Khady/workdash/internal/config"
	"github.com/Khady/workdash/internal/model"
)

func TestCanonicalPRURL(t *testing.T) {
	got, err := CanonicalPRURL("https://github.com/Example/Repo/pull/0042/files?notification_referrer=x#discussion")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://github.com/example/repo/pull/42" {
		t.Fatalf("got %q", got)
	}
	for _, raw := range []string{
		"https://example.com/example/repo/pull/42",
		"https://github.com/example/repo/issues/42",
		"https://github.com/example/repo/pull/not-a-number",
	} {
		if _, err := CanonicalPRURL(raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestActionsForPRGroupsEveryMatchingItemAndUsesCapabilities(t *testing.T) {
	prURL := "https://github.com/example/repo/pull/42"
	state := NewState("")
	state.loadedAt = time.Now()
	state.cfg = config.DashboardConfig{
		TerminalLauncher: "terminal {command}",
		Commands: []config.CommandConfig{{
			ID: "shell", Label: "open shell", Command: "bash", Cwd: "{path}",
			Run: "shell", Scope: "both", Contexts: []string{"worktree"}, Requires: []string{"path"},
		}},
	}
	state.items = []model.WorkItem{
		{Kind: model.KindWorktree, HostLabel: "local", Path: "/repo/wt", PRURL: prURL, Action: actions.CdAction{Path: "/repo/wt"}},
		{Kind: model.KindTmux, HostLabel: "local", Path: "/repo/wt", Session: "feature", PRURL: prURL, Action: actions.TmuxAction{Session: "feature"}},
	}

	response, err := state.ActionsForPR(context.Background(), prURL+"?tab=checks")
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Groups) != 1 {
		t.Fatalf("got %d groups: %#v", len(response.Groups), response.Groups)
	}
	if len(response.Groups[0].Actions) != 3 {
		t.Fatalf("expected duplicate terminal actions to collapse, got %#v", response.Groups[0].Actions)
	}
	if len(state.tokens) == 0 {
		t.Fatal("expected server-side action capabilities")
	}
	for _, group := range response.Groups {
		for _, action := range group.Actions {
			if action.Enabled && action.Token == "" {
				t.Fatalf("enabled action has no token: %#v", action)
			}
		}
	}

	state.tokens = map[string]actionCapability{}
	summary, err := state.SummaryForPR(context.Background(), prURL)
	if err != nil {
		t.Fatal(err)
	}
	if summary.ActionCount != 3 || len(summary.Groups) != 0 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if len(state.tokens) != 0 {
		t.Fatal("summary lookup created action capabilities")
	}
}

func TestConfirmationAndOneUseTokens(t *testing.T) {
	state := NewState("")
	confirmedToken := newToken()
	state.tokens[confirmedToken] = actionCapability{
		action: actions.ConfiguredInlineAction{ConfiguredCommandAction: actions.ConfiguredCommandAction{
			Command: "true", Run: "inline",
		}},
		confirmation: &actions.ConfirmationPrompt{Title: "Confirm"},
	}
	if _, err := state.Execute(confirmedToken, false); err == nil {
		t.Fatal("expected confirmation to be required")
	}
	if _, err := state.Execute(confirmedToken, true); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Execute(confirmedToken, true); err == nil {
		t.Fatal("expected capability replay to fail")
	}
}

func TestMissingTerminalLauncherDisablesShellAction(t *testing.T) {
	state := NewState("")
	projected, capability := state.projectAction(actions.ItemActionOption{
		Label: "attach", Action: actions.TmuxAction{Session: "feature"},
	})
	if projected.Enabled || projected.UnavailableReason == "" || capability.action != nil {
		t.Fatalf("unexpected projection: %#v %#v", projected, capability)
	}
}

func TestSnapshotJSONRestoresRuntimeActions(t *testing.T) {
	items := []model.WorkItem{{
		Kind: model.KindTmux, Session: "feature", HostLabel: "local",
		Action: actions.TmuxAction{Session: "feature"},
	}}
	data, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []model.WorkItem
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded[0].Action != nil {
		t.Fatal("runtime action leaked into snapshot JSON")
	}
	decoded = RestoreActions(decoded, true)
	action, ok := decoded[0].Action.(actions.TmuxAction)
	if !ok || !action.InsideTmux || action.Session != "feature" {
		t.Fatalf("unexpected restored action: %#v", decoded[0].Action)
	}
}
