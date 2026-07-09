#!/usr/bin/env bash
set -euo pipefail

bindir="${BINDIR:-$HOME/.local/bin}"
unitdir="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
manifestdir="$HOME/.mozilla/native-messaging-hosts"

systemctl --user disable --now workdash.socket 2>/dev/null || true
systemctl --user stop workdash.service 2>/dev/null || true
rm -f "$unitdir/workdash.socket" "$unitdir/workdash.service"
rm -f "$manifestdir/io.github.khady.workdash.json"
rm -f "$bindir/workdash-native-host"
systemctl --user daemon-reload

printf 'Removed Workdash daemon and Firefox native-host integration.\n'
