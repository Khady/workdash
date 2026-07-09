#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bindir="${BINDIR:-$HOME/.local/bin}"
unitdir="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
manifestdir="$HOME/.mozilla/native-messaging-hosts"

mkdir -p "$bindir" "$unitdir" "$manifestdir"
bindir="$(cd "$bindir" && pwd)"
if [[ "$bindir" == *[[:space:]]* ]]; then
  printf 'BINDIR must not contain whitespace for the systemd unit.\n' >&2
  exit 1
fi
go -C "$root" build -o "$bindir/workdash" ./cmd/workdash
go -C "$root" build -o "$bindir/workdash-native-host" ./cmd/workdash-native-host

cat >"$unitdir/workdash.socket" <<EOF
[Unit]
Description=Workdash local API socket

[Socket]
ListenStream=%t/workdash.sock
SocketMode=0600
RemoveOnStop=true

[Install]
WantedBy=sockets.target
EOF

cat >"$unitdir/workdash.service" <<EOF
[Unit]
Description=Workdash state daemon
Requires=workdash.socket
After=workdash.socket

[Service]
Type=simple
ExecStart=$bindir/workdash daemon
Restart=on-failure
Nice=10
IOSchedulingClass=idle
CPUWeight=20
IOWeight=20
EOF

native_path="${bindir//\\/\\\\}/workdash-native-host"
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

systemctl --user daemon-reload
systemctl --user enable --now workdash.socket
systemctl --user try-restart workdash.service

printf 'Installed Workdash daemon and Firefox native host.\n'
printf 'Next: install build/workdash-firefox.xpi in Firefox.\n'
