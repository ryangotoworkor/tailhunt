#!/usr/bin/env bash
# First version of the sabotage loop in scripts/failing-controls.sh.
# On macOS's bash 3.2, "${env[@]}" of an EMPTY array is an unbound
# variable under set -u, so every "clean" run died before go test started.
set -uo pipefail
# @learn:sabotage-loop-bash32
for mode in sabotaged clean; do
  env=(); [ "$mode" = sabotaged ] && env=(TAILHUNT_SABOTAGE="$sab")
  res=$(env "${env[@]}" TAILHUNT_REQUIRE_PG=1 go test -count=1 -run "^${test}\$" -v ./internal/inventory 2>&1)
  verdict=$(grep -oE -- "^--- (PASS|FAIL)" <<<"$res" | head -1 | cut -c5-)
  want=PASS; [ "$mode" = sabotaged ] && want=FAIL
  [ "$verdict" = "$want" ] || bad=1
done
# @learn:end sabotage-loop-bash32
