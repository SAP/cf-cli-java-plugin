#!/usr/bin/env bash
# sample-apps.sh — manage CF Java plugin test apps
# Usage: ./sample-apps.sh [deploy|undeploy|busy|calm]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

APPS=(jdk11 sapmachine11 jdk17 sapmachine17 jdk21 sapmachine21 jdk25 sapmachine25)
BUSY_LOOP_PID_FILE="/tmp/sample-apps-busy-loop.pid"

# ── helpers ──────────────────────────────────────────────────────────────────

die() { echo "ERROR: $*" >&2; exit 1; }

require_cf() {
  command -v cf >/dev/null 2>&1 || die "cf CLI not found on PATH"
  cf target >/dev/null 2>&1 || die "Not logged in to CF. Run: cf login"
}

app_route() {
  local app="$1"
  cf app "$app" 2>/dev/null \
    | grep -E "^routes:" \
    | awk '{print $2}' \
    | head -1
}

http_post() {
  local url="$1"
  curl -s -o /dev/null -w "%{http_code}" -X POST "$url" 2>/dev/null
}

# ── deploy ────────────────────────────────────────────────────────────────────

cmd_deploy() {
  require_cf

  # Build JAR if missing
  if [[ ! -f "$SCRIPT_DIR/workload.jar" ]]; then
    echo "workload.jar not found — building..."
    "$SCRIPT_DIR/src/build.sh"
  fi

  echo "Deleting any existing sample apps..."
  for app in "${APPS[@]}"; do
    cf delete -f "$app" 2>/dev/null || true
  done

  echo "Pushing all apps (sequential to avoid staging queue saturation)..."
  for app in "${APPS[@]}"; do
    echo "  → pushing $app"
    (cd "$SCRIPT_DIR/$app" && cf push) || echo "  ✗ $app failed to push"
  done

  echo ""
  echo "App summary:"
  printf "%-20s %-8s %s\n" "NAME" "STATE" "ROUTE"
  printf "%-20s %-8s %s\n" "----" "-----" "-----"
  for app in "${APPS[@]}"; do
    state=$(cf app "$app" 2>/dev/null | grep "^requested state:" | awk '{print $3}' || echo "unknown")
    route=$(app_route "$app" 2>/dev/null || echo "(no route)")
    printf "%-20s %-8s %s\n" "$app" "$state" "$route"
  done
}

# ── undeploy ──────────────────────────────────────────────────────────────────

cmd_undeploy() {
  require_cf
  cmd_calm 2>/dev/null || true
  echo "Deleting all sample apps..."
  for app in "${APPS[@]}"; do
    echo "  → deleting $app"
    cf delete -f "$app" 2>/dev/null || true
  done
  echo "Done."
}

# ── busy ─────────────────────────────────────────────────────────────────────

cmd_busy() {
  require_cf

  echo "Activating all apps..."
  for app in "${APPS[@]}"; do
    route=$(app_route "$app" 2>/dev/null) || { echo "  ✗ $app: cannot get route (not deployed?)"; continue; }
    [[ -z "$route" ]] && { echo "  ✗ $app: no route found"; continue; }
    code=$(http_post "http://$route/activate")
    echo "  → $app: POST /activate → HTTP $code"
  done

  # Kill any existing busy loop
  if [[ -f "$BUSY_LOOP_PID_FILE" ]]; then
    old_pid=$(cat "$BUSY_LOOP_PID_FILE")
    kill "$old_pid" 2>/dev/null || true
    rm -f "$BUSY_LOOP_PID_FILE"
  fi

  echo "Starting local contention loop (10 concurrent GETs per app every 30s)..."
  echo "  Press Ctrl-C or run './sample-apps.sh calm' to stop."

  (
    trap 'exit 0' TERM INT
    while true; do
      sleep 30
      for app in "${APPS[@]}"; do
        route=$(cf app "$app" 2>/dev/null | grep "^routes:" | awk '{print $2}' | head -1) || continue
        [[ -z "$route" ]] && continue
        for i in $(seq 1 10); do
          curl -s -o /dev/null "http://$route/" &
        done
      done
      wait
    done
  ) &
  echo $! > "$BUSY_LOOP_PID_FILE"
  echo "Busy loop PID: $(cat "$BUSY_LOOP_PID_FILE")"
}

# ── calm ─────────────────────────────────────────────────────────────────────

cmd_calm() {
  require_cf

  # Stop local busy loop if running
  if [[ -f "$BUSY_LOOP_PID_FILE" ]]; then
    pid=$(cat "$BUSY_LOOP_PID_FILE")
    kill "$pid" 2>/dev/null && echo "Stopped local contention loop (PID $pid)"
    rm -f "$BUSY_LOOP_PID_FILE"
  fi

  echo "Deactivating all apps..."
  for app in "${APPS[@]}"; do
    route=$(app_route "$app" 2>/dev/null) || { echo "  ✗ $app: cannot get route (not deployed?)"; continue; }
    [[ -z "$route" ]] && { echo "  ✗ $app: no route found"; continue; }
    code=$(http_post "http://$route/deactivate")
    echo "  → $app: POST /deactivate → HTTP $code"
  done
}

# ── dispatch ──────────────────────────────────────────────────────────────────

case "${1:-}" in
  deploy)   cmd_deploy ;;
  undeploy) cmd_undeploy ;;
  busy)     cmd_busy ;;
  calm)     cmd_calm ;;
  *)
    echo "Usage: $0 [deploy|undeploy|busy|calm]"
    echo ""
    echo "  deploy    Build JAR (if needed), push all 8 apps to CF"
    echo "  undeploy  Delete all 8 apps from CF"
    echo "  busy      Activate elevated workload on all apps + start local contention loop"
    echo "  calm      Deactivate elevated workload (apps return to 5ms/1MB baseline)"
    exit 1
    ;;
esac
