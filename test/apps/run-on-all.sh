#!/usr/bin/env bash
# run-on-all.sh — run a cf java command against all (or filtered) sample apps,
# group results by similarity, and report.
#
# Usage: ./run-on-all.sh [--jdk-only|--sapmachine-only] [--keep-tmp] -- <cf java args...>
#
# Examples:
#   ./run-on-all.sh -- java --pid 1 --command status
#   ./run-on-all.sh --jdk-only -- heap-dump
#   ./run-on-all.sh --keep-tmp -- thread-dump
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

ALL_APPS=(jdk11 sapmachine11 jdk17 sapmachine17 jdk21 sapmachine21 jdk25 sapmachine25)
JDK_APPS=(jdk11 jdk17 jdk21 jdk25)
SAP_APPS=(sapmachine11 sapmachine17 sapmachine21 sapmachine25)

# ── parse flags ──────────────────────────────────────────────────────────────

FILTER="all"
KEEP_TMP=false
CF_ARGS=()
PARALLEL=true

while [[ $# -gt 0 ]]; do
  case "$1" in
    --jdk-only)       FILTER="jdk";        shift ;;
    --sapmachine-only) FILTER="sapmachine"; shift ;;
    --keep-tmp)       KEEP_TMP=true;       shift ;;
    --sequential)     PARALLEL=false;      shift ;;
    --)               shift; CF_ARGS=("$@"); break ;;
    *)
      echo "Unknown flag: $1" >&2
      echo "Usage: $0 [--jdk-only|--sapmachine-only] [--keep-tmp] [--sequential] -- <cf java args...>" >&2
      exit 1
      ;;
  esac
done

if [[ ${#CF_ARGS[@]} -eq 0 ]]; then
  echo "ERROR: no cf java arguments supplied after '--'" >&2
  echo "Usage: $0 [--jdk-only|--sapmachine-only] [--keep-tmp] [--sequential] -- <cf java args...>" >&2
  exit 1
fi

# ── select apps ──────────────────────────────────────────────────────────────

case "$FILTER" in
  jdk)        APPS=("${JDK_APPS[@]}") ;;
  sapmachine) APPS=("${SAP_APPS[@]}") ;;
  *)          APPS=("${ALL_APPS[@]}") ;;
esac

# ── scratch space ─────────────────────────────────────────────────────────────

WORK_DIR="$(mktemp -d)"
# shellcheck disable=SC2064
if [[ "$KEEP_TMP" == false ]]; then
  trap "rm -rf '$WORK_DIR'" EXIT
else
  echo "Tmp dir: $WORK_DIR"
fi

# ── run ───────────────────────────────────────────────────────────────────────

echo "Running: cf java ${CF_ARGS[*]}"
echo "Apps:    ${APPS[*]}"
echo ""

run_app() {
  local app="$1"
  local app_dir="$WORK_DIR/$app"
  mkdir -p "$app_dir"

  local out="$app_dir/stdout"
  local err="$app_dir/stderr"
  local rc_file="$app_dir/rc"

  # Run from the app's own tmp directory so any downloaded files land there
  (
    cd "$app_dir"
    cf java "$app" "${CF_ARGS[@]}" >"$out" 2>"$err"
    echo $? >"$rc_file"
  ) || echo $? >"$rc_file"
}

if [[ "$PARALLEL" == true ]]; then
  pids=()
  for app in "${APPS[@]}"; do
    run_app "$app" &
    pids+=($!)
  done
  # Wait for all and collect exit codes; don't let `wait` itself abort the script
  for pid in "${pids[@]}"; do
    wait "$pid" 2>/dev/null || true
  done
else
  for app in "${APPS[@]}"; do
    echo "  → $app ..."
    run_app "$app"
  done
fi

# Fill in missing rc files (process may have been killed)
for app in "${APPS[@]}"; do
  rc_file="$WORK_DIR/$app/rc"
  [[ -f "$rc_file" ]] || echo "1" >"$rc_file"
done

# ── collect results ───────────────────────────────────────────────────────────

declare -A SIGNATURE_TO_APPS   # signature → space-separated app list
declare -A SIGNATURE_TO_OUT    # signature → stdout content
declare -A SIGNATURE_TO_ERR    # signature → stderr content
declare -A SIGNATURE_TO_RC     # signature → exit code

for app in "${APPS[@]}"; do
  app_dir="$WORK_DIR/$app"
  stdout_content=""
  stderr_content=""
  rc=1
  [[ -f "$app_dir/stdout" ]] && stdout_content="$(cat "$app_dir/stdout")"
  [[ -f "$app_dir/stderr" ]] && stderr_content="$(cat "$app_dir/stderr")"
  [[ -f "$app_dir/rc"     ]] && rc="$(cat "$app_dir/rc")"

  # Build a normalized signature for grouping:
  # Strip app-specific parts (app name, timestamps, tmp paths, port numbers,
  # PIDs from status output) so structurally identical output hashes the same.
  sig="$(printf '%s\n%s\n%s' "$rc" "$stdout_content" "$stderr_content" \
    | sed "s|$app|APP_NAME|g" \
    | sed 's|[0-9]\{4\}-[0-9]\{2\}-[0-9]\{2\}T[0-9:\.Z-]*|TIMESTAMP|g' \
    | sed 's|/tmp/[^[:space:]]*|TMP_PATH|g' \
    | sed 's|pid=[0-9]*|pid=PID|g' \
    | sed 's|PID: [0-9]*|PID: PID|g' \
    | md5sum | awk '{print $1}')"

  if [[ -v "SIGNATURE_TO_APPS[$sig]" ]]; then
    SIGNATURE_TO_APPS["$sig"]+=" $app"
  else
    SIGNATURE_TO_APPS["$sig"]="$app"
    SIGNATURE_TO_OUT["$sig"]="$stdout_content"
    SIGNATURE_TO_ERR["$sig"]="$stderr_content"
    SIGNATURE_TO_RC["$sig"]="$rc"
  fi
done

# ── report ────────────────────────────────────────────────────────────────────

total_groups=${#SIGNATURE_TO_APPS[@]}
group_idx=0

for sig in "${!SIGNATURE_TO_APPS[@]}"; do
  group_idx=$((group_idx + 1))
  apps_in_group="${SIGNATURE_TO_APPS[$sig]}"
  rc="${SIGNATURE_TO_RC[$sig]}"
  stdout_content="${SIGNATURE_TO_OUT[$sig]}"
  stderr_content="${SIGNATURE_TO_ERR[$sig]}"

  rc_label="OK (rc=0)"
  [[ "$rc" != "0" ]] && rc_label="FAILED (rc=$rc)"

  echo "══════════════════════════════════════════════════════════════════"
  echo "Group $group_idx/$total_groups — $rc_label"
  echo "Apps: $apps_in_group"
  echo "──────────────────────────────────────────────────────────────────"

  if [[ -n "$stdout_content" ]]; then
    echo "$stdout_content"
  fi

  if [[ -n "$stderr_content" ]]; then
    echo "── stderr ─────────────────────────────────────────────────────────"
    echo "$stderr_content"
  fi

  echo ""
done

# ── summary line ──────────────────────────────────────────────────────────────

total_apps=${#APPS[@]}
failed=0
for app in "${APPS[@]}"; do
  rc_file="$WORK_DIR/$app/rc"
  [[ -f "$rc_file" ]] && rc="$(cat "$rc_file")" || rc=1
  [[ "$rc" != "0" ]] && failed=$((failed + 1))
done
ok=$((total_apps - failed))

echo "══════════════════════════════════════════════════════════════════"
echo "Summary: $ok/$total_apps succeeded, $failed failed, $total_groups unique result group(s)"
if [[ "$KEEP_TMP" == true ]]; then
  echo "Tmp dir preserved: $WORK_DIR"
fi
