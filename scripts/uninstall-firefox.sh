#!/usr/bin/env bash
set -euo pipefail

bindir="${BINDIR:-$HOME/.local/bin}"
manifestdir="$HOME/.mozilla/native-messaging-hosts"

rm -f "$manifestdir/io.github.khady.workdash.json"
rm -f "$bindir/wd-host"

printf 'Removed Workdash daemon and Firefox native-host integration.\n'
