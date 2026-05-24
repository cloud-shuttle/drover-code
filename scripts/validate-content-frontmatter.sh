#!/usr/bin/env bash
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Prefer org-workspace script when this repo sits beside ../docs/taxonomy.yaml.
if [[ -f "$SCRIPT_DIR/validate-content-frontmatter.py" ]]; then
  VALIDATOR="$SCRIPT_DIR/validate-content-frontmatter.py"
elif [[ -f "$ROOT/../scripts/validate-content-frontmatter.py" ]]; then
  VALIDATOR="$ROOT/../scripts/validate-content-frontmatter.py"
else
  echo "error: validate-content-frontmatter.py not found" >&2
  exit 1
fi

if ! python3 -c "import jsonschema, yaml" 2>/dev/null; then
  python3 -m pip install --quiet pyyaml jsonschema
fi

exec python3 "$VALIDATOR" --root "$ROOT" "$@"
