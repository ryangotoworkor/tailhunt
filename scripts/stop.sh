#!/usr/bin/env bash
# Stop what demo.sh started: the web UI by its PID file, then this
# project's containers only (compose project "tailhunt"; images are kept).
set -euo pipefail
cd "$(dirname "$0")/.."
DC=${DC:-$(command -v docker-compose || echo "docker compose")}
./scripts/web.sh stop
$DC -p tailhunt stop
