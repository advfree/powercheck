#!/usr/bin/env sh
set -eu

REPOSITORY="${POWERCHECK_REPOSITORY:-advfree/powercheck}"
REQUESTED_VERSION="${POWERCHECK_VERSION:-latest}"
INSTALL_DIR="${POWERCHECK_INSTALL_DIR:-/usr/local/bin}"
UPDATE_COMMAND="${POWERCHECK_UPDATE_COMMAND:-/usr/local/sbin/powercheck-update}"
WEB_ENABLE_COMMAND="${POWERCHECK_WEB_ENABLE_COMMAND:-/usr/local/sbin/powercheck-web-enable}"
WEB_ROOT="${POWERCHECK_WEB_ROOT:-/usr/local/share/powercheck/web}"
STATE_DIR="${POWERCHECK_STATE_DIR:-/var/lib/powercheck}"
ROLLBACK_DIR="${STATE_DIR}/rollback"

say() {
  printf '%s\n' "$*"
}

fail() {
  printf 'powercheck install: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

github_get() {
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    curl -fsSL \
      -H "Authorization: Bearer ${GITHUB_TOKEN}" \
      -H "Accept: application/vnd.github+json" \
      -H "X-GitHub-Api-Version: 2022-11-28" \
      "$1"
  else
    curl -fsSL "$1"
  fi
}

if [ "$(id -u)" -ne 0 ]; then
  fail "run as root, for example: curl .../install.sh | sudo sh"
fi

require_command curl
require_command tar
require_command sha256sum
require_command install

[ "$(uname -s)" = "Linux" ] || fail "this installer currently supports Linux only"
[ "$WEB_ROOT" != "/" ] || fail "POWERCHECK_WEB_ROOT must not be /"

case "$(uname -m)" in
  x86_64|amd64) architecture="amd64" ;;
  aarch64|arm64) architecture="arm64" ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

if [ "$REQUESTED_VERSION" = "latest" ]; then
  # The "releases/latest" endpoint ignores prereleases. The project publishes
  # alpha builds first, so use the newest published release of either kind.
  release_json="$(github_get "https://api.github.com/repos/${REPOSITORY}/releases?per_page=1")"
  tag="$(printf '%s' "$release_json" | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
  [ -n "$tag" ] || fail "could not determine the latest release"
else
  tag="$REQUESTED_VERSION"
  case "$tag" in
    v*) ;;
    *) tag="v${tag}" ;;
  esac
fi

version="${tag#v}"
archive="powercheck_${version}_linux_${architecture}.tar.gz"
release_base="https://github.com/${REPOSITORY}/releases/download/${tag}"
work_dir="$(mktemp -d)"
backup_stamp="$(date -u +%Y%m%dT%H%M%SZ)"

cleanup() {
  rm -rf "$work_dir"
}
trap cleanup EXIT INT TERM

say "Downloading PowerCheck ${tag} for linux/${architecture}..."
github_get "${release_base}/${archive}" >"${work_dir}/${archive}"
github_get "${release_base}/checksums.txt" >"${work_dir}/checksums.txt"

expected="$(awk -v file="$archive" '$2 == file { print $1 }' "${work_dir}/checksums.txt")"
[ -n "$expected" ] || fail "release checksum does not contain ${archive}"
printf '%s  %s\n' "$expected" "${work_dir}/${archive}" | sha256sum -c - >/dev/null

mkdir -p "${work_dir}/extract" "$INSTALL_DIR" "$ROLLBACK_DIR" "$STATE_DIR"
tar -xzf "${work_dir}/${archive}" -C "${work_dir}/extract"

web_source="${work_dir}/extract/web-prototype/dist/client"
[ -f "${web_source}/index.html" ] || fail "release archive is missing the web console"

for binary in powercheck-sim powercheck-dryrun powercheck-pve; do
  [ -x "${work_dir}/extract/${binary}" ] || fail "release archive is missing ${binary}"
done

for binary in powercheck-sim powercheck-dryrun powercheck-pve; do
  source_path="${work_dir}/extract/${binary}"
  if [ -f "${INSTALL_DIR}/${binary}" ]; then
    mkdir -p "${ROLLBACK_DIR}/${backup_stamp}"
    cp -p "${INSTALL_DIR}/${binary}" "${ROLLBACK_DIR}/${backup_stamp}/${binary}"
  fi
  install -m 0755 "$source_path" "${INSTALL_DIR}/${binary}.new"
  mv -f "${INSTALL_DIR}/${binary}.new" "${INSTALL_DIR}/${binary}"
done

if ! "${INSTALL_DIR}/powercheck-sim" -version >/dev/null 2>&1 ||
   ! "${INSTALL_DIR}/powercheck-dryrun" -version >/dev/null 2>&1 ||
   ! "${INSTALL_DIR}/powercheck-pve" -version >/dev/null 2>&1; then
  say "Health check failed; restoring previous binaries..."
  for binary in powercheck-sim powercheck-dryrun powercheck-pve; do
    if [ -f "${ROLLBACK_DIR}/${backup_stamp}/${binary}" ]; then
      cp -p "${ROLLBACK_DIR}/${backup_stamp}/${binary}" "${INSTALL_DIR}/${binary}"
    else
      rm -f "${INSTALL_DIR}/${binary}"
    fi
  done
  fail "new binaries failed their version check"
fi

if [ -f "${work_dir}/extract/scripts/install.sh" ]; then
  mkdir -p "$(dirname "$UPDATE_COMMAND")"
  install -m 0755 "${work_dir}/extract/scripts/install.sh" "$UPDATE_COMMAND"
fi

if [ -f "${work_dir}/extract/scripts/enable-web.sh" ]; then
  mkdir -p "$(dirname "$WEB_ENABLE_COMMAND")"
  install -m 0755 "${work_dir}/extract/scripts/enable-web.sh" "$WEB_ENABLE_COMMAND"
fi

mkdir -p "$WEB_ROOT"
if [ -d "${web_source}/assets" ]; then
  mkdir -p "${WEB_ROOT}/assets"
  cp -R "${web_source}/assets/." "${WEB_ROOT}/assets/"
fi
install -m 0644 "${web_source}/index.html" "${WEB_ROOT}/index.html"

printf '%s\n' "$tag" >"${STATE_DIR}/installed-version"
say "PowerCheck ${tag} installed successfully."
say "Update later with: sudo powercheck-update"
say "Enable the authenticated PVE Web console with: sudo powercheck-web-enable"
say "Configuration files and outage state were not modified."
