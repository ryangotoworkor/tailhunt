#!/usr/bin/env bash
# Prove the fix tests are alive: switch each protection off with
# TAILHUNT_SABOTAGE and require its test to FAIL, then run the same tests
# with nothing sabotaged and require them to PASS. Needs Postgres on
# :54389 (docker-compose -p tailhunt up -d postgres). Output goes to stdout
# and docs/results/failing-controls.txt.
set -uo pipefail
cd "$(dirname "$0")/.."
out=docs/results/failing-controls.txt
: > "$out"
log() { echo "$*" | tee -a "$out"; }
bad=0
# @learn:failing-controls
pairs=(
  "timeout:TestFix_AcquireTimeoutFailsFastInsteadOfQueueing"
  "bulkhead:TestFix_BulkheadReservesConnectionsForAvailability"
  "txscope:TestFix_QuoteOutsideTxReleasesTheConnection"
  "quoteguard:TestFix_QuoteGuardBoundsHoldLatencyAndOpensTheBreaker"
  "all:TestFix_AllLayersKeepAvailabilityFast"
)
for p in "${pairs[@]}"; do
  sab=${p%%:*}; test=${p#*:}
  for mode in sabotaged clean; do
    # An empty TAILHUNT_SABOTAGE means "nothing off". (Not an empty bash
    # array: macOS bash 3.2 treats "${arr[@]}" of an empty array as unbound
    # under set -u, and every clean run silently did not run at all.)
    val=""; [ "$mode" = sabotaged ] && val=$sab
    res=$(TAILHUNT_SABOTAGE="$val" TAILHUNT_REQUIRE_PG=1 go test -count=1 -run "^${test}\$" -v ./internal/inventory 2>&1)
    verdict=$(grep -oE -- "^--- (PASS|FAIL)" <<<"$res" | head -1 | cut -c5-)
    detail=$(grep -E "incident_test.go:[0-9]+: " <<<"$res" | sed -E 's/^ +//' | tail -1)
    want=PASS; [ "$mode" = sabotaged ] && want=FAIL
    mark=ok; [ "$verdict" = "$want" ] || { mark="UNEXPECTED"; bad=1; }
    log "$(printf '%-10s %-9s %-55s %-4s (%s)' "$sab" "$mode" "$test" "${verdict:-none}" "$mark")"
    log "           $detail"
  done
done
# The alert analysis has its own control: rank causes by incident count
# (the naive choice) and the test that pins the planted top cause must fail.
for mode in sabotaged clean; do
  val=""; [ "$mode" = sabotaged ] && val=count
  res=$(TAILHUNT_ANALYSIS_RANK="$val" .venv/bin/python -m pytest -q analysis -k planted_top_cause 2>&1)
  verdict=FAIL; grep -qE "^1 passed" <<<"$(tail -1 <<<"$res")" && verdict=PASS
  want=PASS; [ "$mode" = sabotaged ] && want=FAIL
  mark=ok; [ "$verdict" = "$want" ] || { mark="UNEXPECTED"; bad=1; }
  log "$(printf '%-10s %-9s %-55s %-4s (%s)' "rank=count" "$mode" "test_planted_top_cause_ranks_first_by_impact" "$verdict" "$mark")"
  log "           $(grep -E "^E +AssertionError" <<<"$res" | head -1 | sed -E 's/^E +//')"
done
# @learn:end failing-controls
if [ $bad = 0 ]; then log "all sabotaged runs failed and all clean runs passed"; else log "SOME CONTROLS DID NOT BEHAVE"; fi
exit $bad
