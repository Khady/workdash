#!/usr/bin/env bash
# Installs the daemon-free Firefox native-messaging host over the rewritten core.
#
# There is no socket and no systemd unit to install: Firefox spawns wd-host per
# message and it rebuilds the snapshot from git, tmux and the pull request cache
# the same way `wd` does. The host *name* is unchanged, so an extension already
# installed keeps working — only the binary the manifest points at moves.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bindir="${BINDIR:-$HOME/.local/bin}"
manifestdir="$HOME/.mozilla/native-messaging-hosts"

mkdir -p "$bindir" "$manifestdir"
bindir="$(cd "$bindir" && pwd)"
go -C "$root" build -o "$bindir/wd-host" ./cmd/wd-host

# The manifest holds a JSON string, so a backslash or a quote in the path has to
# be escaped rather than pasted.
native_path="${bindir//\\/\\\\}/wd-host"
native_path="${native_path//\"/\\\"}"
cat >"$manifestdir/io.github.khady.workdash.json" <<EOF
{
  "name": "io.github.khady.workdash",
  "description": "Workdash Firefox bridge",
  "path": "$native_path",
  "type": "stdio",
  "allowed_extensions": ["workdash@khady.github.io"]
}
EOF

printf 'Installed %s and pointed io.github.khady.workdash at it.\n' "$bindir/wd-host"
printf 'Next: install build/workdash-firefox.xpi in Firefox.\n'
