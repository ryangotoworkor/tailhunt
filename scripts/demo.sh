#!/usr/bin/env bash
# One command, from nothing to a running demo:
#   six Go services + Postgres + Jaeger + Prometheus + Grafana (compose project "tailhunt"),
#   then the web UI on :5188 in the background.
# Works on an empty volume: every service creates its own schema and seed on start.
# Stop everything with scripts/stop.sh.
set -euo pipefail
cd "$(dirname "$0")/.."
DC=${DC:-$(command -v docker-compose || echo "docker compose")}
mkdir -p var/logs .runtime
echo "building and starting the stack (the first build takes a minute or two)…"
$DC -p tailhunt up -d --build --wait
for _ in $(seq 1 60); do
  curl -sf -o /dev/null http://localhost:8880/healthz && break
  sleep 1
done
curl -sf http://localhost:8880/api/login -d '{"username":"oncall","password":"oncall-demo"}' >/dev/null \
  || { echo "gateway is up but login failed" >&2; exit 1; }
./scripts/web.sh start
cat <<MSG

  web UI      http://localhost:5188     oncall / oncall-demo  (教学模式: /learn)
  gateway     http://localhost:8880
  Jaeger      http://localhost:16698
  Prometheus  http://localhost:9198
  Grafana     http://localhost:3098     (dashboard "tailhunt")

  stop:       ./scripts/stop.sh
MSG
