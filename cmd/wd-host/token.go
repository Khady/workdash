package main

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/Khady/workdash/internal/workspace"
)

// tokenVersion prefixes every token so a token minted by an older host is
// rejected outright rather than half-understood.
const tokenVersion = "wd1"

// tokenSeparator cannot appear in a host label, a repo root, a branch name, an
// action id or a URL, so joining on it needs no escaping and no field can eat
// the next one.
const tokenSeparator = "\x00"

// actionRef is everything needed to find one action again: which workspace, which
// action, and which pull request page the click came from.
type actionRef struct {
	ID     workspace.ID
	Action string
	PRURL  string
}

// The token is a *reference*, not a capability.
//
// The old daemon minted an opaque random token and kept the resolved command in
// memory. This host cannot: Firefox uses one-shot sendNativeMessage, so the
// process is spawned per message and exits before the user clicks anything. There
// is no memory to hold a token in.
//
// So execute_action re-loads the snapshot, re-resolves the workspace's actions and
// runs the action only if it is still applicable — see resolveRef. That is not a
// security downgrade, for two reasons:
//
//   - Nothing but the native-messaging channel reaches this host. Firefox spawns
//     it from a manifest that names one allowed extension, over that extension's
//     stdio; a token is never a bearer credential exposed to anything else, and
//     an attacker able to write to this stdin already runs as the user and can
//     simply run the command directly.
//   - Revalidating against current state is a *stronger* check than trusting a
//     token minted earlier. The daemon's token said "this command was legal 10
//     minutes ago" and ran it verbatim; this one says "this action is legal right
//     now", so a worktree deleted in the meantime, a pull request that has moved
//     off the branch, or a repo dropped from the config all make the click fail
//     instead of running a command against state that no longer exists.
//
// The token is deliberately reconstructible rather than secret: it names a
// workspace and an action id the extension is already shown in the same response.
func encodeToken(ref actionRef) string {
	return encodeFields(ref.ID.Host, ref.ID.Repo, ref.ID.Branch, ref.Action, ref.PRURL)
}

func encodeFields(fields ...string) string {
	joined := strings.Join(fields, tokenSeparator)
	return tokenVersion + "." + base64.RawURLEncoding.EncodeToString([]byte(joined))
}

func decodeToken(token string) (actionRef, error) {
	malformed := fmt.Errorf("this action button is stale; reopen the popup")
	body, ok := strings.CutPrefix(token, tokenVersion+".")
	if !ok || body == "" {
		return actionRef{}, malformed
	}
	decoded, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return actionRef{}, malformed
	}
	fields := strings.Split(string(decoded), tokenSeparator)
	if len(fields) != 5 {
		return actionRef{}, malformed
	}
	ref := actionRef{
		ID:     workspace.ID{Host: fields[0], Repo: fields[1], Branch: fields[2]},
		Action: fields[3],
		PRURL:  fields[4],
	}
	// A host, repo, branch or action id that is empty cannot identify anything, and
	// an empty pull request URL would skip the page check in resolveRef.
	if ref.ID.Host == "" || ref.ID.Repo == "" || ref.ID.Branch == "" || ref.Action == "" || ref.PRURL == "" {
		return actionRef{}, malformed
	}
	return ref, nil
}
