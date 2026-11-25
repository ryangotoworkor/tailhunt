#!/usr/bin/env bash
# Re-run both analyses. Creates the venv on first use.
#   alert history  : analysis/gen_alerts.py -> analysis/analyze_alerts.py -> analysis/report.md
#   request logs   : var/logs/*.jsonl -> analysis/requestlog.py (skipped when there are no logs)
set -euo pipefail
cd "$(dirname "$0")/.."
PY=${PYTHON311:-/Library/Frameworks/Python.framework/Versions/3.11/bin/python3.11}
[ -x .venv/bin/python ] || { "$PY" -m venv .venv && .venv/bin/pip install -q -r analysis/requirements.txt; }
.venv/bin/python analysis/gen_alerts.py
.venv/bin/python analysis/analyze_alerts.py
if ls var/logs/*.jsonl >/dev/null 2>&1; then .venv/bin/python analysis/requestlog.py; else echo "no request logs yet (run the stack and scripts/bench.sh)"; fi
