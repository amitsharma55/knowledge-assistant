#!/usr/bin/env bash
# Render the LaunchAgent plist with absolute paths and load it. The agent runs
# `day.sh reap` at 22:00 local time daily (see the plist template).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
daysh=$(cd "$here/.." && pwd)/day.sh
agents="$HOME/Library/LaunchAgents"
plist="$agents/com.ka.daily-reaper.plist"
mkdir -p "$agents" "$HOME/.ka"
sed -e "s|@DAYSH@|$daysh|g" -e "s|@HOME@|$HOME|g" \
  "$here/com.ka.daily-reaper.plist.tmpl" > "$plist"
launchctl unload "$plist" >/dev/null 2>&1 || true
launchctl load "$plist"
echo "installed reaper: $plist (fires 22:00 local, runs '$daysh reap')"
