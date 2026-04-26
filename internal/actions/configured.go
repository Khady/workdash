package actions

import (
	"fmt"
	"strings"

	"github.com/Khady/workdash/internal/config"
	"github.com/Khady/workdash/internal/model"
)

type ConfiguredCommandAction struct {
	ID                string
	Label             string
	Detail            string
	Command           string
	Cwd               string
	Run               string
	Remote            bool
	RemoteInteractive bool
	Relaunch          string
	SSHTarget         string
}

type ConfiguredShellAction struct{ ConfiguredCommandAction }

func (a ConfiguredShellAction) ToShell() string { return a.ConfiguredCommandAction.ToShell() }

type ConfiguredInlineAction struct{ ConfiguredCommandAction }

func (a ConfiguredInlineAction) ToShell() string { return a.ConfiguredCommandAction.ToShell() }
func (ConfiguredInlineAction) inline()           {}

func (a ConfiguredCommandAction) ToShell() string {
	cmd := a.Command
	if a.Cwd != "" {
		cmd = fmt.Sprintf("cd -- %s && %s", Quote(a.Cwd), cmd)
	}
	if a.Remote && a.SSHTarget != "" {
		return WrapRemoteCommand(a.SSHTarget, cmd, a.RemoteInteractive)
	}
	return cmd
}

func ConfiguredActionsForItem(item model.WorkItem, commands []config.CommandConfig, terminalAvailable, terminalDefault bool) []ItemActionOption {
	options := []ItemActionOption{}
	for _, command := range commands {
		option, ok := configuredActionForItem(item, command)
		if !ok {
			continue
		}
		if command.Run == "shell" {
			options = append(options, terminalAlternatives(option, option.Label+" in terminal", terminalAvailable, terminalDefault)...)
			continue
		}
		options = append(options, option)
	}
	return options
}

func configuredActionForItem(item model.WorkItem, command config.CommandConfig) (ItemActionOption, bool) {
	if !commandAppliesToItem(item, command) {
		return ItemActionOption{}, false
	}
	values := templateValues(item)
	for _, required := range command.Requires {
		if values[required] == "" {
			return ItemActionOption{}, false
		}
	}
	shellCommand, ok := expandTemplate(command.Command, values)
	if !ok || strings.TrimSpace(shellCommand) == "" {
		return ItemActionOption{}, false
	}
	cwd, ok := expandTemplate(command.Cwd, values)
	if !ok {
		return ItemActionOption{}, false
	}
	detail, ok := expandTemplate(command.Detail, values)
	if !ok {
		return ItemActionOption{}, false
	}
	base := ConfiguredCommandAction{
		ID: command.ID, Label: command.Label, Detail: detail, Command: shellCommand, Cwd: cwd,
		Run: command.Run, Remote: command.Remote, RemoteInteractive: command.RemoteInteractive,
		Relaunch: command.Relaunch, SSHTarget: item.SSHTarget,
	}
	var action model.ShellAction
	switch command.Run {
	case "inline", "background":
		action = ConfiguredInlineAction{ConfiguredCommandAction: base}
	case "terminal":
		action = TerminalLaunchAction{Action: ConfiguredShellAction{ConfiguredCommandAction: base}}
	default:
		action = ConfiguredShellAction{ConfiguredCommandAction: base}
	}
	return ItemActionOption{Shortcut: command.Shortcut, Label: command.Label, Detail: detail, Action: action}, true
}

func commandAppliesToItem(item model.WorkItem, command config.CommandConfig) bool {
	switch command.Scope {
	case "local":
		if item.SSHTarget != "" {
			return false
		}
	case "remote":
		if item.SSHTarget == "" {
			return false
		}
	}
	if len(command.Contexts) == 0 {
		return true
	}
	kind := string(item.Kind)
	for _, context := range command.Contexts {
		if context == kind {
			return true
		}
	}
	return false
}

func templateValues(item model.WorkItem) map[string]string {
	values := map[string]string{
		"kind":       string(item.Kind),
		"title":      item.Title,
		"subtitle":   item.Subtitle,
		"path":       item.Path,
		"repo_root":  item.RepoRoot,
		"branch":     item.Branch,
		"session":    item.Session,
		"pr_url":     item.PRURL,
		"host":       item.HostLabel,
		"ssh_target": item.SSHTarget,
	}
	if item.SSHTarget != "" {
		values["remote_vscode"] = "ssh-remote+" + item.SSHTarget
		values["remote_ssh_url"] = "ssh://" + item.SSHTarget + item.Path
	}
	return values
}

func expandTemplate(template string, values map[string]string) (string, bool) {
	if template == "" {
		return "", true
	}
	var out strings.Builder
	for i := 0; i < len(template); {
		if template[i] != '{' {
			out.WriteByte(template[i])
			i++
			continue
		}
		end := strings.IndexByte(template[i+1:], '}')
		if end < 0 {
			return "", false
		}
		token := template[i+1 : i+1+end]
		name := token
		quoted := false
		if strings.HasSuffix(token, ":q") {
			name = strings.TrimSuffix(token, ":q")
			quoted = true
		}
		value, ok := values[name]
		if !ok {
			return "", false
		}
		if quoted {
			value = Quote(value)
		}
		out.WriteString(value)
		i += end + 2
	}
	return out.String(), true
}
