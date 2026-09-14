#!/usr/bin/env bash
# Unload and remove the reaper LaunchAgent.
set -euo pipefail
plist="$HOME/Library/LaunchAgents/com.ka.daily-reaper.plist"
launchctl unload "$plist" >/dev/null 2>&1 || true
rm -f "$plist"
echo "removed reaper LaunchAgent."
