#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

command -v promtool >/dev/null || { echo "promtool is required" >&2; exit 1; }
python3 "${repo_root}/test/promtool/generate_kme_latency_tests.py"
promtool test rules "${repo_root}/test/promtool/kme_storage_latency_alert_tests.yml"
