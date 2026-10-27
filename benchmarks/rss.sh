#!/usr/bin/env bash
# Measures server RSS (idle, then under a small request load) on the 10.000-item seed.
# Usage: benchmarks/rss.sh   (run from the repo root; requires go and curl)
set -euo pipefail
dir=$(mktemp -d)
trap 'kill "${pid:-}" 2>/dev/null || true; rm -rf "$dir"' EXIT

go build -o "$dir/atlas" ./cmd/atlas
go run ./cmd/atlas-seed -data-dir "$dir/data" -items 10000 >/dev/null

ATLAS_DATA_DIR="$dir/data" ATLAS_ADDR=127.0.0.1:18080 ATLAS_COOKIE_SECURE=false "$dir/atlas" >/dev/null 2>&1 &
pid=$!
sleep 3
rss() { ps -o rss= -p "$pid" | awk '{printf "%.1f MB", $1/1024}'; }
echo "idle RSS:        $(rss)"
for _ in $(seq 1 2000); do curl -s -o /dev/null http://127.0.0.1:18080/api/v1/health; done
echo "after 2000 reqs: $(rss)"
