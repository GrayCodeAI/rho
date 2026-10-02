#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ECO_DIR="$(cd "${ROOT_DIR}/.." && pwd)"

# The canonical inventory lives in GrayCodeAI/graycode-eco, not in a product.
# A tenant must not own the registry of its siblings: rho was renamed from hawk
# without signalling the other nine repositories, and the in-product copy of
# this list silently omitted beam. See graycode-eco/adr/0001.
#
# Prefer the roof. Fall back to the deprecated in-product copy only when the
# roof is not checked out alongside, which happens for a standalone `go build`
# of rho outside the ecosystem workspace. Callers do not need to know which one
# was used.
ROOF_MANIFEST="${ECO_DIR}/graycode-eco/ecosystem.yaml"
LEGACY_MANIFEST="${ROOT_DIR}/ecosystem.yaml"

if [[ -f "${ROOF_MANIFEST}" ]]; then
  MANIFEST="${ROOF_MANIFEST}"
elif [[ -f "${LEGACY_MANIFEST}" ]]; then
  MANIFEST="${LEGACY_MANIFEST}"
  echo "WARNING: graycode-eco is not checked out; falling back to the deprecated ${LEGACY_MANIFEST#"${ROOT_DIR}/"}" >&2
  echo "WARNING: it is incomplete (no beam) and must not be treated as canonical" >&2
else
  echo "manifest not found: looked for ${ROOF_MANIFEST} and ${LEGACY_MANIFEST}" >&2
  exit 1
fi

usage() {
  echo "usage: $0 validate | json | list <all|workspace|engines>" >&2
  exit 2
}

[[ -f "${MANIFEST}" ]] || { echo "manifest not found: ${MANIFEST}" >&2; exit 1; }

records() {
  awk '
    # Records are the entries under `repositories:`. The roof manifest also has
    # a `not_products:` block listing repositories in the GitHub organization
    # that are deliberately not ecosystem products; those entries have no
    # `directory:` key and are therefore skipped here, which is the intent.
    function flush() {
      if (directory != "") {
        if (module == "") module = "-"
        if (facade == "") facade = "-"
        printf "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", directory, github, product, kind, language, module, workspace ":" tracks, facade
      }
      directory=github=product=kind=language=module=workspace=tracks=facade=""
    }
    /^  - directory:/ { flush(); directory=$0; sub(/^  - directory:[[:space:]]*/, "", directory); next }
    /^    github_repo:/ { github=$0; sub(/^    github_repo:[[:space:]]*/, "", github); next }
    /^    product_name:/ { product=$0; sub(/^    product_name:[[:space:]]*/, "", product); next }
    /^    kind:/ { kind=$0; sub(/^    kind:[[:space:]]*/, "", kind); next }
    /^    language:/ { language=$0; sub(/^    language:[[:space:]]*/, "", language); next }
    /^    module:/ { module=$0; sub(/^    module:[[:space:]]*/, "", module); next }
    /^    workspace:/ { workspace=$0; sub(/^    workspace:[[:space:]]*/, "", workspace); next }
    /^    tracks_eagle:/ { tracks=$0; sub(/^    tracks_eagle:[[:space:]]*/, "", tracks); next }
    /^    facade:/ { facade=$0; sub(/^    facade:[[:space:]]*/, "", facade); next }
    END { flush() }
  ' "${MANIFEST}"
}

json() {
  records | awk -F '\t' '
    function quote(value) {
      gsub(/\\/, "\\\\", value)
      gsub(/"/, "\\\"", value)
      return "\"" value "\""
    }
    BEGIN { print "{\n  \"schemaVersion\": 1,\n  \"repositories\": [" }
    {
      split($7, flags, ":")
      if (flags[2] == "") flags[2] = "false"
      if (NR > 1) print ","
      printf "    {\"directory\":%s,\"githubRepo\":%s,\"productName\":%s,\"kind\":%s,\"language\":%s,\"module\":%s,\"workspace\":%s,\"tracksEagle\":%s,\"facade\":%s}",
        quote($1), quote($2), quote($3), quote($4), quote($5),
        ($6 == "-" ? "null" : quote($6)), flags[1], flags[2],
        ($8 == "-" ? "null" : quote($8))
    }
    END { print "\n  ]\n}" }
  '
}

list_records() {
  local selector="$1"
  records | awk -F '\t' -v selector="${selector}" '
    selector == "all" { print $1; next }
    selector == "workspace" && $7 ~ /^true:/ { print $1; next }
    selector == "engines" && $4 == "engine" { print $1; next }
  '
}

validate() {
  local failed=0 count=0
  local seen_dirs="" seen_modules=""
  while IFS=$'\t' read -r directory github product kind language module flags facade; do
    count=$((count + 1))
    for required in directory github product kind language flags; do
      if [[ -z "${!required}" ]]; then
        echo "${directory:-record-${count}}: missing ${required}" >&2
        failed=1
      fi
    done
    if grep -qxF "${directory}" <<<"${seen_dirs}"; then
      echo "duplicate directory: ${directory}" >&2
      failed=1
    fi
    seen_dirs+="${directory}"$'\n'

    # Absent workspace checkouts skip all filesystem checks below; the checkout
    # step already warned. Workspace repos may be git clones OR local module
    # directories (e.g. the restored engine modules), so require only that the
    # directory exists; go.mod/module validation below still runs when present.
    # Manifest-internal checks (fields, dupes, count) stay strict so manifest
    # drift still fails.
    if [[ "${flags%%:*}" == "true" && ! -d "${ECO_DIR}/${directory}" ]]; then
      echo "WARNING: ${directory}: workspace repository is not checked out; skipping filesystem checks" >&2
      if [[ "${language}" == "go" && -n "${module}" ]]; then
        if grep -qxF "${module}" <<<"${seen_modules}"; then
          echo "duplicate module: ${module}" >&2
          failed=1
        fi
        seen_modules+="${module}"$'\n'
      fi
      continue
    fi

    if [[ "${language}" == "go" ]]; then
      if [[ -z "${module}" ]]; then
        echo "${directory}: Go repository has no module" >&2
        failed=1
      elif [[ "${flags%%:*}" == "true" && ! -f "${ECO_DIR}/${directory}/go.mod" ]]; then
        echo "${directory}: missing go.mod" >&2
        failed=1
      elif [[ -f "${ECO_DIR}/${directory}/go.mod" ]]; then
        actual="$(awk '$1 == "module" { print $2; exit }' "${ECO_DIR}/${directory}/go.mod")"
        if [[ "${actual}" != "${module}" ]]; then
          echo "${directory}: manifest module ${module} != go.mod module ${actual}" >&2
          failed=1
        fi
      fi
      if grep -qxF "${module}" <<<"${seen_modules}"; then
        echo "duplicate module: ${module}" >&2
        failed=1
      fi
      seen_modules+="${module}"$'\n'
    fi

    if [[ "${kind}" == "engine" ]]; then
      if [[ -z "${facade}" || "${facade}" == "-" ]]; then
        echo "${directory}: engine has no supported facade" >&2
        failed=1
      elif [[ "${facade}" != "${module}" && "${facade}" != "${module}/"* ]]; then
        echo "${directory}: facade ${facade} is outside module ${module}" >&2
        failed=1
      else
        facade_relative="${facade#"${module}"}"
        facade_relative="${facade_relative#/}"
        if [[ -n "${facade_relative}" && ! -d "${ECO_DIR}/${directory}/${facade_relative}" ]]; then
          echo "${directory}: facade directory not found: ${facade_relative}" >&2
          failed=1
        fi
      fi
    fi

  done < <(records)

  if ((count < 4)); then
    echo "expected at least 4 repositories, found ${count}" >&2
    failed=1
  fi
  ((failed == 0)) || exit 1
  echo "ecosystem manifest valid (${count} repositories)"
}

case "${1:-}" in
  validate) validate ;;
  json) json ;;
  list)
    [[ $# -eq 2 ]] || usage
    case "$2" in all|workspace|engines) list_records "$2" ;; *) usage ;; esac
    ;;
  *) usage ;;
esac
