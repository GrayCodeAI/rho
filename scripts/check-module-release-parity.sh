#!/usr/bin/env bash
set -euo pipefail

# Workspace release parity: verify every ecosystem module Rho requires
# resolves to a published, reachable commit. This is a multi-repository
# check; the repositories are sibling checkouts, not Git submodules.
# Run from the Rho repository root.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ECO_DIR="$(cd "${ROOT_DIR}/.." && pwd)"
# Prefer the roof's manifest; fall back to the deprecated in-product copy.
# See scripts/ecosystem-manifest.sh and graycode-eco/adr/0001.
if [[ -f "${ECO_DIR}/graycode-eco/ecosystem.yaml" ]]; then
  MANIFEST="${ECO_DIR}/graycode-eco/ecosystem.yaml"
else
  MANIFEST="${ROOT_DIR}/ecosystem.yaml"
fi
repos=()
while IFS= read -r repo; do
  [[ "${repo}" != "rho" && -n "${repo}" ]] && repos+=("${repo}")
done < <("${ROOT_DIR}/scripts/ecosystem-manifest.sh" list workspace)
failed=0

# Directory and module path differ when a repo is renamed (e.g. flux
# hosts module github.com/GrayCodeAI/flux), so resolve via the manifest.
module_for_repo() {
  awk -v dir="$1" '
    /^  - directory:/ { cur=$0; sub(/^  - directory:[[:space:]]*/, "", cur); next }
    /^    module:/ && cur == dir { mod=$0; sub(/^    module:[[:space:]]*/, "", mod); print mod; exit }
  ' "${MANIFEST}"
}

printf '%-24s %-14s %s\n' MODULE MODULE_COMMIT STATUS
for repo in "${repos[@]}"; do
  module="$(module_for_repo "${repo}")"

  version=$(GOWORK=off go list -m -f '{{.Version}}' "$module" 2>/dev/null || true)
  if [[ -z "$version" ]]; then
    printf '%-24s %-14s %s\n' "$repo" - NOT_REQUIRED
    continue
  fi

  metadata=$(GOWORK=off go mod download -json "${module}@${version}" 2>/dev/null || true)
  module_commit=$(printf '%s\n' "$metadata" | sed -n 's/.*"Hash": "\([0-9a-f]*\)".*/\1/p' | head -1)

  if [[ -z "$module_commit" ]]; then
    printf '%-24s %-14s %s\n' "$repo" unknown UNRESOLVED
    failed=1
  else
    printf '%-24s %-14s %s\n' "$repo" "${module_commit:0:12}" OK
  fi
done

if ((failed)); then
  echo "workspace/module release parity failed (unresolvable module version)" >&2
  exit 1
fi
