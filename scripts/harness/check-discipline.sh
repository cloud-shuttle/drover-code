#!/usr/bin/env bash
# Fails if required harness discipline files are missing.
set -euo pipefail
root="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$root"
missing=0
require() {
  if [[ ! -e "$1" ]]; then
    echo "MISSING: $1" >&2
    missing=1
  else
    echo "OK: $1"
  fi
}
require internal/agent/lineage.go
require internal/harness/preflight.go
require internal/verification/verification.go
require internal/convo/budget.go
require internal/config/adr_loader.go
require internal/commands/skills.go
require .drover/skills/grill/SKILL.md
require .drover/skills/verify/SKILL.md
require docs/adr/0001-trusted-delegation-depth.md
require design/harness/README.md
if [[ "$missing" -ne 0 ]]; then
  echo "harness discipline check failed" >&2
  exit 1
fi
echo "harness discipline check passed"
