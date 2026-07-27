#!/usr/bin/env sh
set -eu

BINARY="/usr/local/bin/powercheck-pve"
WEB_ROOT="/usr/local/share/powercheck/web"
PASSWORD_FILE="/etc/powercheck/web-password"
UNIT_FILE="/etc/systemd/system/powercheck-pve-web.service"
LISTEN="${POWERCHECK_WEB_LISTEN:-0.0.0.0:8765}"

say() {
  printf '%s\n' "$*"
}

fail() {
  printf 'powercheck web setup: %s\n' "$*" >&2
  exit 1
}

[ "$(id -u)" -eq 0 ] || fail "run with sudo"
[ -x "$BINARY" ] || fail "missing ${BINARY}; run powercheck-update first"
[ -f "${WEB_ROOT}/index.html" ] || fail "missing web assets; run powercheck-update first"
command -v systemctl >/dev/null 2>&1 || fail "systemctl is required"
command -v od >/dev/null 2>&1 || fail "od is required"
command -v hostname >/dev/null 2>&1 || fail "hostname is required"

node="$(hostname -s)"
case "$node" in
  ""|*[!A-Za-z0-9_.-]*) fail "unsafe PVE node name returned by hostname: ${node}" ;;
esac
case "$LISTEN" in
  ""|*[!A-Za-z0-9.:_-]*) fail "unsafe listen address: ${LISTEN}" ;;
esac

mkdir -p /etc/powercheck
if [ ! -f "$PASSWORD_FILE" ]; then
  umask 077
  od -An -N24 -tx1 /dev/urandom | tr -d ' \n' >"$PASSWORD_FILE"
  printf '\n' >>"$PASSWORD_FILE"
fi
chmod 0600 "$PASSWORD_FILE"

unit_stage="$(mktemp)"
cleanup() {
  rm -f "$unit_stage"
}
trap cleanup EXIT INT TERM

{
  printf '%s\n' \
    '[Unit]' \
    'Description=PowerCheck PVE Web Console' \
    'After=network-online.target pve-cluster.service' \
    'Wants=network-online.target' \
    '' \
    '[Service]' \
    'Type=simple' \
    "ExecStart=${BINARY} -action web -node ${node} -confirm-node ${node} -timeout 180 -listen ${LISTEN} -web-root ${WEB_ROOT} -web-password-file ${PASSWORD_FILE} -execute" \
    'Restart=on-failure' \
    'RestartSec=5' \
    'User=root' \
    'UMask=0077' \
    'NoNewPrivileges=true' \
    'PrivateTmp=true' \
    'ProtectHome=true' \
    '' \
    '[Install]' \
    'WantedBy=multi-user.target'
} >"$unit_stage"

install -m 0644 "$unit_stage" "$UNIT_FILE"
systemctl daemon-reload
systemctl enable --now powercheck-pve-web.service

say "PowerCheck Web is running on ${LISTEN} for node ${node}."
say "Username: admin"
say "Password: $(sed -n '1p' "$PASSWORD_FILE")"
say "Keep this service behind your VPN or trusted reverse proxy; raw HTTP is not suitable for the public Internet."
