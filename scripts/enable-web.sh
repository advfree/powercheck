#!/usr/bin/env sh
set -eu

BINARY="/usr/local/bin/powercheck-pve"
WEB_ROOT="/usr/local/share/powercheck/web"
ACCOUNT_FILE="/etc/powercheck/web-account.json"
LEGACY_PASSWORD_FILE="/etc/powercheck/web-password"
UNIT_FILE="/etc/systemd/system/powercheck-pve-web.service"
LISTEN="${POWERCHECK_WEB_LISTEN:-0.0.0.0:8765}"
RESET_PASSWORD=0

say() {
  printf '%s\n' "$*"
}

fail() {
  printf 'powercheck web setup: %s\n' "$*" >&2
  exit 1
}

case "${1:-}" in
  "") ;;
  --reset-password) RESET_PASSWORD=1 ;;
  *) fail "usage: powercheck-web-enable [--reset-password]" ;;
esac
[ "$#" -le 1 ] || fail "usage: powercheck-web-enable [--reset-password]"

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
created_password=""
if [ ! -f "$ACCOUNT_FILE" ] || [ "$RESET_PASSWORD" -eq 1 ]; then
  umask 077
  if [ "$RESET_PASSWORD" -eq 0 ] && [ -f "$LEGACY_PASSWORD_FILE" ]; then
    created_password="$(sed -n '1p' "$LEGACY_PASSWORD_FILE")"
    [ -n "$created_password" ] || fail "legacy web password is empty"
  else
    created_password="$(od -An -N24 -tx1 /dev/urandom | tr -d ' \n')"
  fi
  password_hash="$(printf '%s' "$created_password" | "$BINARY" -action hash-web-password)"
  {
    printf '{\n'
    printf '  "username": "admin",\n'
    printf '  "password_hash": "%s"\n' "$password_hash"
    printf '}\n'
  } >"$ACCOUNT_FILE"
  if [ -f "$LEGACY_PASSWORD_FILE" ]; then
    rm -f "$LEGACY_PASSWORD_FILE"
  fi
fi
chmod 0600 "$ACCOUNT_FILE"

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
    "ExecStart=${BINARY} -action web -node ${node} -confirm-node ${node} -timeout 180 -listen ${LISTEN} -web-root ${WEB_ROOT} -web-account-file ${ACCOUNT_FILE} -execute" \
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
systemctl enable powercheck-pve-web.service
systemctl restart powercheck-pve-web.service

say "PowerCheck Web is running on ${LISTEN} for node ${node}."
say "Username: admin"
if [ -n "$created_password" ]; then
  say "Initial password: ${created_password}"
  say "Store this password now; only its salted hash is kept on the PVE node."
else
  say "Existing account preserved; its password was not changed."
fi
say "Keep this service behind your VPN or trusted reverse proxy; raw HTTP is not suitable for the public Internet."
