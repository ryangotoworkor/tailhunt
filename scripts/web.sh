#!/usr/bin/env bash
# Start or stop the Vite dev server for the web UI on :5188.
# The PID goes to .runtime/vite.pid; stop kills exactly that PID (never by name).
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p .runtime
pidf=.runtime/vite.pid
case "${1:-start}" in
  start)
    if [ -f $pidf ] && kill -0 "$(cat $pidf)" 2>/dev/null; then echo "web UI already running (pid $(cat $pidf))"; exit 0; fi
    [ -d frontend/node_modules ] || (cd frontend && npm ci --no-audit --no-fund)
    node scripts/learn_anchors.mjs >/dev/null
    # cd inside the subshell first, then background only the node process,
    # so $! is vite's own PID.
    (cd frontend; nohup node node_modules/vite/bin/vite.js --port 5188 --strictPort > ../.runtime/vite.log 2>&1 &
     echo $! > ../$pidf)
    for _ in $(seq 1 60); do
      curl -sf -o /dev/null http://localhost:5188/ && { echo "web UI on http://localhost:5188 (pid $(cat $pidf))"; exit 0; }
      sleep 0.5
    done
    echo "vite did not come up; see .runtime/vite.log" >&2; exit 1 ;;
  stop)
    if [ -f $pidf ]; then kill "$(cat $pidf)" 2>/dev/null || true; rm -f $pidf; echo "web UI stopped"; fi ;;
  *) echo "usage: $0 start|stop" >&2; exit 2 ;;
esac
