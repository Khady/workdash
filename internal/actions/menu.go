package actions

import (
	"github.com/Khady/workdash/internal/config"
	"github.com/Khady/workdash/internal/model"
)

type MenuAction interface{}

type ConfirmationPrompt struct {
	Title        string
	Detail       string
	ConfirmLabel string
}

type ItemActionOption struct {
	Shortcut     rune
	Label        string
	Detail       string
	Action       MenuAction
	Confirmation *ConfirmationPrompt
}

func ActionsForItem(item model.WorkItem, commands []config.CommandConfig, terminalLauncherAvailable, terminalLauncherDefault bool) []ItemActionOption {
	switch item.Kind {
	case model.KindPR:
		return prActions(item, commands, terminalLauncherAvailable, terminalLauncherDefault)
	case model.KindWorktree:
		return worktreeActions(item, commands, terminalLauncherAvailable, terminalLauncherDefault)
	case model.KindBranch:
		return branchActions(item, commands, terminalLauncherAvailable, terminalLauncherDefault)
	case model.KindTmux:
		return tmuxActions(item, commands, terminalLauncherAvailable, terminalLauncherDefault)
	default:
		return nil
	}
}

func prActions(item model.WorkItem, commands []config.CommandConfig, terminalAvailable, terminalDefault bool) []ItemActionOption {
	options := []ItemActionOption{}
	options = append(options, ConfiguredActionsForItem(item, commands, terminalAvailable, terminalDefault)...)
	return options
}

func worktreeActions(item model.WorkItem, commands []config.CommandConfig, terminalAvailable, terminalDefault bool) []ItemActionOption {
	options := []ItemActionOption{}
	options = append(options, ConfiguredActionsForItem(item, commands, terminalAvailable, terminalDefault)...)
	return options
}

func branchActions(item model.WorkItem, commands []config.CommandConfig, terminalAvailable, terminalDefault bool) []ItemActionOption {
	if item.RepoRoot == "" || item.Branch == "" {
		return nil
	}
	var primary ItemActionOption
	if item.Path != "" {
		return ConfiguredActionsForItem(item, commands, terminalAvailable, terminalDefault)
	} else {
		primary = ItemActionOption{Shortcut: 'w', Label: "checkout branch", Detail: item.Branch, Action: CheckoutAction{RepoRoot: item.RepoRoot, Branch: item.Branch, SSHTarget: item.SSHTarget}}
	}
	options := terminalAlternatives(primary, primary.Label+" in terminal", terminalAvailable, terminalDefault)
	options = append(options, ConfiguredActionsForItem(item, commands, terminalAvailable, terminalDefault)...)
	return options
}

func tmuxActions(item model.WorkItem, commands []config.CommandConfig, terminalAvailable, terminalDefault bool) []ItemActionOption {
	if item.Session == "" {
		return nil
	}
	options := terminalAlternatives(ItemActionOption{Shortcut: 'a', Label: "attach to session", Detail: item.Session, Action: item.Action}, "attach to session in terminal", terminalAvailable, terminalDefault)
	options = append(options, ItemActionOption{
		Shortcut: 'k', Label: "kill session", Detail: item.Session,
		Action:       KillTmuxSessionAction{Session: item.Session, SSHTarget: item.SSHTarget},
		Confirmation: &ConfirmationPrompt{Title: "Kill tmux session?", Detail: item.HostLabel + ": " + item.Session + " will be terminated.", ConfirmLabel: "Kill session"},
	})
	options = append(options, ConfiguredActionsForItem(item, commands, terminalAvailable, terminalDefault)...)
	return options
}

func terminalAlternatives(option ItemActionOption, terminalLabel string, terminalAvailable, terminalDefault bool) []ItemActionOption {
	action, ok := option.Action.(model.ShellAction)
	if !ok || !terminalAvailable || TerminalLaunchCommand(action) == "" {
		return []ItemActionOption{option}
	}
	launched := option
	launched.Label = terminalLabel
	launched.Action = TerminalLaunchAction{Action: action}
	shell := option
	if terminalDefault {
		shell.Shortcut = 0
		return []ItemActionOption{launched, shell}
	}
	launched.Shortcut = 0
	return []ItemActionOption{shell, launched}
}
