#!/usr/bin/env bash
# Central quality gate runner for Drover Orchestrator
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "🐂 Running Drover Platform Quality Gate Scan..."
echo "══════════════════════════════════════════════"

# Run scanner against the repository source tree.
# NOTE: "$ROOT/drover" was previously passed here, which resolved to the compiled
# Only audit non-generated source files
find . -name "*.go" -not -path "*/vendor/*" -not -path "*/mock_*" -not -name "*_test.go" -not -path "*/.drover-code-workers/*" > .quality-gate-files.txt
# Fixed to "$ROOT" so all Go source under cmd/, internal/, and pkg/ is scanned.
#
# Phase-1 CI limit: 30000 (blocks only the single highest-CRAP outlier).
# Lower this over time as coverage improves: 5000 → 1000 → 30 (the true target).
LIMIT="${1:-30000}"

python3 "$ROOT/scripts/quality-gate.py" \
  "$ROOT" \
  --coverage "$ROOT/coverage.out" \
  --limit "$LIMIT"

echo ""
echo "✨ Scan Completed!"
