#!/bin/sh
set -eu

GREPRULES_REPO="${GREPRULES_REPO:-greprules/greprules}"
GREPRULES_VERSION="${GREPRULES_VERSION:-latest}"
GREPRULES_INSTALL_DIR="${GREPRULES_INSTALL_DIR:-}"

log() {
  printf '%s\n' "$*" >&2
}

step() {
  number="$1"
  shift
  log "[$number] $*"
}

fail() {
  log "error: $*"
  exit 1
}

have() {
  command -v "$1" >/dev/null 2>&1
}

tmp_dir="$(mktemp -d 2>/dev/null || mktemp -d -t greprules)"
tmp_target=""

cleanup() {
  if [ -n "${tmp_target:-}" ] && [ -f "$tmp_target" ]; then
    rm -f "$tmp_target"
  fi
  rm -rf "$tmp_dir"
}
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM

download_file() {
  url="$1"
  destination="$2"
  token="${GH_TOKEN:-}"
  if [ -z "$token" ]; then
    token="${GITHUB_TOKEN:-}"
  fi

  if have curl; then
    if [ -n "$token" ]; then
      curl -fsSL -H "Authorization: Bearer ${token}" "$url" -o "$destination"
    else
      curl -fsSL "$url" -o "$destination"
    fi
    return
  fi

  if have wget; then
    if [ -n "$token" ]; then
      wget -q --header="Authorization: Bearer ${token}" -O "$destination" "$url"
    else
      wget -q -O "$destination" "$url"
    fi
    return
  fi

  if have python3; then
    GH_DOWNLOAD_TOKEN="$token" python3 - "$url" "$destination" <<'PY'
import os
import sys
import urllib.request

url, destination = sys.argv[1], sys.argv[2]
request = urllib.request.Request(url)
token = os.environ.get("GH_DOWNLOAD_TOKEN")
if token:
    request.add_header("Authorization", f"Bearer {token}")
with urllib.request.urlopen(request, timeout=120) as response:
    data = response.read()
with open(destination, "wb") as handle:
    handle.write(data)
PY
    return
  fi

  return 1
}

sha256_file() {
  file="$1"
  if have sha256sum; then
    sha256sum "$file" | awk '{print $1}'
    return
  fi
  if have shasum; then
    shasum -a 256 "$file" | awk '{print $1}'
    return
  fi
  if have openssl; then
    openssl dgst -sha256 "$file" | awk '{print $NF}'
    return
  fi
  if have python3; then
    python3 - "$file" <<'PY'
import hashlib
import sys

h = hashlib.sha256()
with open(sys.argv[1], "rb") as handle:
    for chunk in iter(lambda: handle.read(1024 * 1024), b""):
        h.update(chunk)
print(h.hexdigest())
PY
    return
  fi
  return 1
}

platform_os() {
  case "$(uname -s)" in
    Darwin) printf 'darwin' ;;
    Linux) printf 'linux' ;;
    *) return 1 ;;
  esac
}

platform_arch() {
  case "$(uname -m)" in
    arm64 | aarch64) printf 'arm64' ;;
    x86_64 | amd64) printf 'amd64' ;;
    *) return 1 ;;
  esac
}

resolve_version() {
  version="$GREPRULES_VERSION"
  if [ -z "$version" ] || [ "$version" = "latest" ]; then
    latest_json="$tmp_dir/latest.json"
    latest_url="https://api.github.com/repos/${GREPRULES_REPO}/releases/latest"
    download_file "$latest_url" "$latest_json" || fail "failed to fetch latest greprules release metadata"
    version="$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$latest_json" | head -n 1)"
    [ -n "$version" ] || fail "failed to parse latest greprules release tag"
  fi

  case "$version" in
    v*) printf '%s' "$version" ;;
    *) printf 'v%s' "$version" ;;
  esac
}

default_install_dir() {
  if [ -n "$GREPRULES_INSTALL_DIR" ]; then
    printf '%s' "$GREPRULES_INSTALL_DIR"
    return
  fi
  [ -n "${HOME:-}" ] || fail "HOME is not set; set GREPRULES_INSTALL_DIR"
  printf '%s' "$HOME/.local/bin"
}

run_setup_opengrep() {
  opengrep_setup_log="${tmp_dir}/opengrep-setup.log"
  if "$target" setup-opengrep >"$opengrep_setup_log" 2>&1; then
    cat "$opengrep_setup_log" >&2
  else
    cat "$opengrep_setup_log" >&2
    fail "failed to prepare managed OpenGrep"
  fi
}

configure_managed_opengrep() {
  opengrep_config_log="${tmp_dir}/opengrep-config.log"
  if "$target" agent-config set opengrep.mode managed --global >"$opengrep_config_log" 2>&1; then
    cat "$opengrep_config_log" >&2
  else
    cat "$opengrep_config_log" >&2
    fail "failed to configure managed OpenGrep"
  fi
}

os="$(platform_os)" || fail "unsupported OS: $(uname -s). curl install currently supports macOS and Linux"
arch="$(platform_arch)" || fail "unsupported architecture: $(uname -m). supported architectures are amd64 and arm64"

step "1/5" "Resolving greprules release for ${os}/${arch}"
version="$(resolve_version)"
archive_version="${version#v}"
archive="greprules_${archive_version}_${os}_${arch}.tar.gz"
base_url="https://github.com/${GREPRULES_REPO}/releases/download/${version}"
archive_url="${base_url}/${archive}"
checksums_url="${base_url}/checksums.txt"
archive_path="${tmp_dir}/${archive}"
checksums_path="${tmp_dir}/checksums.txt"
extract_dir="${tmp_dir}/extract"
install_dir="$(default_install_dir)"
target="${install_dir}/greprules"

step "2/5" "Downloading greprules ${version}"

download_file "$checksums_url" "$checksums_path" || fail "failed to download checksums.txt"
download_file "$archive_url" "$archive_path" || fail "failed to download ${archive}"

step "3/5" "Verifying archive checksum"
expected="$(awk -v file="$archive" '$2 == file {print $1}' "$checksums_path" | head -n 1)"
[ -n "$expected" ] || fail "checksum entry not found for ${archive}"
actual="$(sha256_file "$archive_path")" || fail "no SHA256 tool found; install sha256sum, shasum, openssl, or python3"
[ "$actual" = "$expected" ] || fail "checksum mismatch for ${archive}"

step "4/5" "Installing greprules into ${install_dir}"
mkdir -p "$extract_dir"
tar -xzf "$archive_path" -C "$extract_dir" || fail "failed to extract ${archive}"
[ -f "${extract_dir}/greprules" ] || fail "archive did not contain greprules binary"

mkdir -p "$install_dir" || fail "failed to create ${install_dir}"
tmp_target="${target}.tmp.$$"
cp "${extract_dir}/greprules" "$tmp_target" || fail "failed to write ${tmp_target}"
chmod 0755 "$tmp_target" || fail "failed to chmod ${tmp_target}"
mv "$tmp_target" "$target" || fail "failed to install ${target}"
tmp_target=""

installed_version="$("$target" --version 2>/dev/null || true)"
if [ -n "$installed_version" ]; then
  log "Installed greprules ${installed_version} to ${target}"
else
  log "Installed greprules to ${target}"
fi

step "5/5" "Preparing managed OpenGrep; this may take a minute"
run_setup_opengrep
configure_managed_opengrep

case ":${PATH:-}:" in
  *:"$install_dir":*) ;;
  *)
    log ""
    log "${install_dir} is not on PATH. Add it with:"
    log "  export PATH=\"${install_dir}:\$PATH\""
    ;;
esac

log ""
if [ -x "$target" ]; then
  case ":${PATH:-}:" in
    *:"$install_dir":*) log "Done. Run: greprules scan ." ;;
    *) log "Done. Run: ${target} scan ." ;;
  esac
fi
