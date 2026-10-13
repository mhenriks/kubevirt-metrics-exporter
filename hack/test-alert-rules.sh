#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

command -v promtool >/dev/null || { echo "promtool is required" >&2; exit 1; }
python3 "${repo_root}/test/promtool/generate_kme_latency_tests.py"
promtool test rules "${repo_root}/test/promtool/kme_storage_latency_alert_tests.yml"

python3 "${repo_root}/test/promtool/generate_aggregation_tests.py"
promtool check rules "${repo_root}/test/promtool/aggregation-rules.yml"
promtool test rules "${repo_root}/test/promtool/aggregation-stage-tests.yml" "${repo_root}/test/promtool/aggregation-raw-tests.yml"

python3 "${repo_root}/test/promtool/test_replay_storage_alerts.py"
