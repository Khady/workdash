// Package shellquote turns argv into a string a POSIX shell splits back into the
// same argv.
//
// Two places genuinely need this — the SSH transport, which has to send one
// command string over the wire, and the executor's `shell: true` path, which
// runs commands through the user's shell so rc functions resolve. One
// implementation with one property test is easier to trust than two.
package shellquote

import "strings"

// unsafeChars is deliberately broad: anything that could be special to some
// shell in some context gets quoted. Over-quoting is invisible; under-quoting is
// a command that runs something other than what was meant.
const unsafeChars = " \t\n'\"\\$`*?[]|&;<>(){}#~=!"

func Quote(text string) string {
	if text != "" && !strings.ContainsAny(text, unsafeChars) {
		return text
	}
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}

func Join(argv []string) string {
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		quoted = append(quoted, Quote(arg))
	}
	return strings.Join(quoted, " ")
}
