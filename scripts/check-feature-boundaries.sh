#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

failed=0

if rg -n '"github\.com/GrayCodeAI/rho/cmd' internal/features --glob '*.go' --glob '!**/*_test.go'; then
  echo "feature packages must not import the CLI composition root" >&2
  failed=1
fi

if rg -n '"github\.com/GrayCodeAI/rho/internal/features/' internal/features --glob '*.go' --glob '!**/*_test.go'; then
  echo "feature packages must not import one another directly; use contracts or platform seams" >&2
  failed=1
fi

if rg -n '"charm\.land/(bubbletea|bubbles|lipgloss)' internal/features --glob '*.go' --glob '!**/*_test.go'; then
  echo "feature packages must not own Bubble Tea or terminal-rendering orchestration; keep it in cmd" >&2
  failed=1
fi

if ((failed)); then
  exit 1
fi

echo "Rho feature boundary guard passed"
