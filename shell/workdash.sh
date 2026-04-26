#!/usr/bin/env bash

workdash() {
  local cmd
  local emit_path
  local first_line
  local last_action_status=0
  local script_dir
  local project_dir
  local relaunched=0
  local status

  script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  project_dir="$(cd "$script_dir/.." && pwd)"
  while true; do
    emit_path="$(mktemp "${TMPDIR:-/tmp}/workdash.XXXXXX")" || return 1

    if type -P workdash >/dev/null 2>&1; then
      command workdash --emit-path "$emit_path" "$@"
    else
      go -C "$project_dir" run ./cmd/workdash --emit-path "$emit_path" "$@"
    fi
    status=$?

    cmd="$(cat "$emit_path")"
    rm -f "$emit_path"

    if [ "$status" -ne 0 ]; then
      if [ "$relaunched" -eq 1 ] && [ "$status" -eq 130 ]; then
        return "$last_action_status"
      fi
      return "$status"
    fi

    if [ -z "$cmd" ]; then
      return 0
    fi

    first_line="${cmd%%$'\n'*}"
    if [ "$first_line" = "# workdash:relaunch=always" ]; then
      if [ "$cmd" = "$first_line" ]; then
        cmd=""
      else
        cmd="${cmd#"$first_line"$'\n'}"
      fi
    fi

    if [ -n "$cmd" ]; then
      printf '[workdash] %s\n' "$cmd"
      eval "$cmd"
      status=$?
    else
      status=0
    fi

    if [ "$first_line" = "# workdash:relaunch=always" ]; then
      last_action_status=$status
      relaunched=1
      continue
    fi

    return "$status"
  done
}
