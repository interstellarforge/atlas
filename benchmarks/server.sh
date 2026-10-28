#!/usr/bin/env bash
# Runs the performance suite on a server that has Docker but no local Go toolchain.
set -euo pipefail

usage() {
	cat <<'EOF'
Usage: benchmarks/server.sh [all|benchmark|rss]

Environment overrides:
  ATLAS_BENCH_IMAGE   Go image (default: golang:1.27-bookworm)
  ATLAS_BENCH_COUNT   benchmark repetitions (default: 3)
  ATLAS_RSS_RUNS      RSS repetitions (default: 3)
  ATLAS_RSS_CONTEXT   context entries for RSS runs (default: 200)
EOF
}

mode="${1:-all}"
case "$mode" in
	all | benchmark | rss) ;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		usage >&2
		exit 2
		;;
esac

command -v docker >/dev/null || {
	echo "docker is required" >&2
	exit 1
}
command -v git >/dev/null || {
	echo "git is required" >&2
	exit 1
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
git_dir="$(git -C "$repo_root" rev-parse --absolute-git-dir)"
commit="$(git -C "$repo_root" rev-parse HEAD)"
image="${ATLAS_BENCH_IMAGE:-golang:1.27-bookworm}"
bench_count="${ATLAS_BENCH_COUNT:-3}"
rss_runs="${ATLAS_RSS_RUNS:-3}"
rss_context="${ATLAS_RSS_CONTEXT:-200}"

for value in "$bench_count" "$rss_runs" "$rss_context"; do
	[[ "$value" =~ ^[0-9]+$ ]] || {
		echo "benchmark counts must be non-negative integers" >&2
		exit 2
	}
done

for volume in atlas-bench-gomod atlas-bench-gocache atlas-bench-tmp; do
	docker volume create "$volume" >/dev/null
done

docker_args=(
	run --rm
	--memory="${ATLAS_BENCH_MEMORY:-1g}"
	--memory-swap="${ATLAS_BENCH_MEMORY_SWAP:-1536m}"
	-e "ATLAS_BENCH_COMMIT=$commit"
	-v "$git_dir:/repo.git:ro"
	-v atlas-bench-gomod:/go/pkg/mod
	-v atlas-bench-gocache:/root/.cache/go-build
	-v atlas-bench-tmp:/tmp
)

prepare_source='git config --global --add safe.directory /repo.git
mkdir -p /src
git --git-dir=/repo.git archive "$ATLAS_BENCH_COMMIT" | tar -x -C /src
cd /src
test -f /src/go.mod
mkdir -p web/dist
touch web/dist/.keep
export PATH="/usr/local/go/bin:$PATH"
go version'

run_benchmarks() {
	echo "Running latency and concurrency benchmarks for $commit"
	docker "${docker_args[@]}" \
		-e ATLAS_BENCH=1 \
		-e "ATLAS_BENCH_COUNT=$bench_count" \
		"$image" sh -ec "$prepare_source
go test -p 1 ./benchmarks \\
  -run '^(TestBaseline|TestConcurrentClients|TestContext)\$' \\
  -count=\"\$ATLAS_BENCH_COUNT\" -v"
}

run_rss() {
	echo "Running RSS measurements for $commit"
	docker "${docker_args[@]}" \
		-e "ATLAS_RSS_RUNS=$rss_runs" \
		-e "ATLAS_RSS_CONTEXT=$rss_context" \
		"$image" sh -ec "apt-get update -qq
apt-get install -y -qq curl procps >/dev/null
$prepare_source
run=1
while [ \"\$run\" -le \"\$ATLAS_RSS_RUNS\" ]; do
  echo \"RSS run \$run\"
  CONTEXT=\"\$ATLAS_RSS_CONTEXT\" benchmarks/rss.sh
  run=\$((run + 1))
done"
}

case "$mode" in
	all)
		run_benchmarks
		run_rss
		;;
	benchmark) run_benchmarks ;;
	rss) run_rss ;;
esac
