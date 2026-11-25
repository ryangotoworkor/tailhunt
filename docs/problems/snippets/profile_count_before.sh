#!/usr/bin/env bash
# First version of the request counter in scripts/profile.sh. awk splits
# on spaces, and the label route="GET /search" contains one, so $2 was the
# string '/search",svc="search"}' and the sum stayed 0. Every run reported
# "0 requests" and "71 MB per request".
# @learn:profile-count-before
count() { curl -sf "$SEARCH/metrics" | awk '/^tailhunt_http_server_duration_seconds_count\{.*route="GET \/search".*svc="search"/ {s+=$2} END {printf "%d", s}'; }
# @learn:end profile-count-before
