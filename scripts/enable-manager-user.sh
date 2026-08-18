#!/usr/bin/env sh
set -eu

BINARY="${POWERCHECK_MANAGER_BINARY:-${HOME}/.local/bin/powercheck-manager}"
WEB_ROOT="${POWERCHECK_MANAGER_WEB_ROOT:-${HOME}/.local/share/powercheck-manager/web}"
UNIT_DIR="${XDG_CONFIG_HOME:-${HOME}/.config}/systemd/user"
UNIT_FILE="${UNIT_DIR}/powercheck-manager.service"
LISTEN="${POWERCHECK_MANAGER_LISTEN:-0.0.0.0:8765}"
PVE_URL="${POWERCHECK_PVE_URL:-}"
NUT_ADDRESS="${POWERCHECK_NUT_ADDRESS:-}"
NUT_UPS="${POWERCHECK_NUT_UPS:-}"
NUT_HISTORY_FILE="${POWERCHECK_NUT_HISTORY_FILE:-${HOME}/.local/state/powercheck/ups-history.jsonl}"
EVENT_FILE="${POWERCHECK_EVENT_FILE:-${HOME}/.local/state/powercheck/events.jsonl}"
UPS_SPEC="${POWERCHECK_UPS_SPEC:-}"
WOL_CONFIG="${POWERCHECK_WOL_CONFIG:-}"

fail() {
  printf 'powercheck manager setup: %s\n' "$*" >&2
  exit 1
}

[ "$(id -u)" -ne 0 ] || fail "run as the unprivileged Manager user, not root"
[ -x "$BINARY" ] || fail "missing executable ${BINARY}"
[ -f "${WEB_ROOT}/index.html" ] || fail "missing web assets in ${WEB_ROOT}"
[ -n "$PVE_URL" ] || fail "POWERCHECK_PVE_URL is required"
command -v systemctl >/dev/null 2>&1 || fail "systemctl is required"

case "$LISTEN" in
  ""|*[!A-Za-z0-9.:_-]*) fail "unsafe listen address: ${LISTEN}" ;;
esac
case "$PVE_URL" in
  http://*|https://*) ;;
  *) fail "POWERCHECK_PVE_URL must use http or https" ;;
esac
case "$PVE_URL" in
  *[!A-Za-z0-9.:/_-]*) fail "unsafe PVE URL: ${PVE_URL}" ;;
esac
case "${NUT_ADDRESS}:${NUT_UPS}" in
  *[!A-Za-z0-9.:_-]*) fail "unsafe NUT address or UPS name" ;;
esac
case "${BINARY}:${WEB_ROOT}:${NUT_HISTORY_FILE}:${EVENT_FILE}:${UPS_SPEC}:${WOL_CONFIG}" in
  *[!A-Za-z0-9./:_-]*) fail "binary and web paths must not contain spaces or special characters" ;;
esac

mkdir -p "$UNIT_DIR"
nut_flags=""
if [ -n "$NUT_ADDRESS" ]; then
  nut_flags=" -nut-address ${NUT_ADDRESS} -nut-history-file ${NUT_HISTORY_FILE}"
  if [ -n "$NUT_UPS" ]; then
    nut_flags="${nut_flags} -nut-ups ${NUT_UPS}"
  fi
  if [ -n "$UPS_SPEC" ]; then
    [ -f "$UPS_SPEC" ] || fail "missing UPS spec ${UPS_SPEC}"
    nut_flags="${nut_flags} -ups-spec ${UPS_SPEC}"
  fi
fi
wol_flags=""
if [ -n "$WOL_CONFIG" ]; then
  [ -f "$WOL_CONFIG" ] || fail "missing WOL config ${WOL_CONFIG}"
  wol_flags=" -wol-config ${WOL_CONFIG}"
fi
unit_stage="$(mktemp)"
cleanup() {
  rm -f "$unit_stage"
}
trap cleanup EXIT INT TERM

{
  printf '%s\n' \
    '[Unit]' \
    'Description=PowerCheck Manager Web Console' \
    'After=network-online.target' \
    'Wants=network-online.target' \
    '' \
    '[Service]' \
    'Type=simple' \
    "ExecStart=/usr/bin/systemd-cat --namespace=powercheck --identifier=powercheck-manager ${BINARY} -listen ${LISTEN} -pve-url ${PVE_URL} -web-root ${WEB_ROOT} -event-file ${EVENT_FILE}${nut_flags}${wol_flags}" \
    'Restart=on-failure' \
    'RestartSec=5' \
    'NoNewPrivileges=true' \
    'PrivateTmp=true' \
    '' \
    '[Install]' \
    'WantedBy=default.target'
} >"$unit_stage"

install -m 0644 "$unit_stage" "$UNIT_FILE"
systemctl --user daemon-reload
systemctl --user enable powercheck-manager.service
systemctl --user restart powercheck-manager.service

printf 'PowerCheck Manager is running on %s and proxying %s.\n' "$LISTEN" "$PVE_URL"
if command -v loginctl >/dev/null 2>&1 &&
   [ "$(loginctl show-user "$(id -un)" -p Linger --value 2>/dev/null || true)" != "yes" ]; then
  printf 'Warning: user lingering is disabled; an administrator must run: loginctl enable-linger %s\n' "$(id -un)"
fi
