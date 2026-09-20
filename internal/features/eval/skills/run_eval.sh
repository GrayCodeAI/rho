#!/usr/bin/env bash
set -euo pipefail
# Semi-automated skill eval runner
EVAL_DIR="$(cd "$(dirname "$0")" && pwd)"
DATE=$(date +%Y-%m-%d)
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

echo "=== Rho Skills Eval ==="
echo "Date: $DATE | Commit: $COMMIT"
echo ""
echo "Run each scenario in a fresh rho session and record results."
echo ""

ROUND_FILE="$EVAL_DIR/rounds/round-$DATE.md"
if [[ ! -f "$ROUND_FILE" ]]; then
    cat > "$ROUND_FILE" <<EOF
# Eval Round — $DATE

Commit: $COMMIT

| Scenario | Result | Notes |
|----------|--------|-------|
| 1 — Go review | | |
| 2 — Security scan | | |
| 3 — Namespaced invoke | | |
| 4 — Cross-skill chain | | |
| 5 — Reference loading | | |
| 6 — Negative boundary | | |

Aggregate: /6
Regressions: none
EOF
    echo "Created: $ROUND_FILE"
else
    echo "Round file exists: $ROUND_FILE"
fi
echo ""
echo "Edit $ROUND_FILE with results after running scenarios."
