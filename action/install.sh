#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "error: $*" >&2
  exit 1
}

require_env() {
  local name="$1"
  local value="${!name:-}"
  if [[ -z "$value" ]]; then
    fail "$name is required"
  fi
}

require_tool() {
  local name="$1"
  if ! command -v "$name" >/dev/null 2>&1; then
    fail "$name is required"
  fi
}

sha256_file() {
  local path="$1"

  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$path" | awk '{print $1}'
    return
  fi

  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$path" | awk '{print $1}'
    return
  fi

  if command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$path" | awk '{print $NF}'
    return
  fi

  fail "sha256 checksum utility is required"
}

require_env RUNNER_TEMP
require_env RUNNER_OS
require_env RUNNER_ARCH
require_env GITHUB_API_URL
require_env GITHUB_REPOSITORY
require_env GITHUB_TOKEN
require_env GITHUB_ENV
require_env INPUT_VERSION

require_tool curl
require_tool jq
require_tool tar

version="${INPUT_VERSION}"
if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  fail "version must be an exact vMAJOR.MINOR.PATCH tag"
fi

case "${RUNNER_OS}:${RUNNER_ARCH}" in
  Linux:X64)
    goos="linux"
    goarch="amd64"
    ;;
  Linux:ARM64)
    goos="linux"
    goarch="arm64"
    ;;
  macOS:X64)
    goos="darwin"
    goarch="amd64"
    ;;
  macOS:ARM64)
    goos="darwin"
    goarch="arm64"
    ;;
  Windows:*)
    fail "windows unsupported"
    ;;
  *:X86)
    fail "x86 unsupported"
    ;;
  *:ARM)
    fail "arm unsupported"
    ;;
  *)
    fail "unsupported runner platform: ${RUNNER_OS}/${RUNNER_ARCH}"
    ;;
esac

asset_name="duto-ai_${goos}_${goarch}.tar.gz"
api_url="${GITHUB_API_URL%/}"

work_dir="$(mktemp -d "${RUNNER_TEMP}/duto-action-install.XXXXXX")"
cleanup() {
  rm -rf "$work_dir"
}
trap cleanup EXIT

release_json="${work_dir}/release.json"
release_url="${api_url}/repos/${GITHUB_REPOSITORY}/releases/tags/${version}"

curl \
  --silent \
  --show-error \
  --fail \
  --header 'Accept: application/vnd.github+json' \
  --header "Authorization: Bearer ${GITHUB_TOKEN}" \
  --output "$release_json" \
  "$release_url"

if [[ "$(jq -er '.tag_name' "$release_json")" != "$version" ]]; then
  fail "release tag metadata does not match requested version"
fi

matching_assets="$(jq -c --arg name "$asset_name" '.assets | map(select((.name | type) == "string" and .name == $name))' "$release_json")"
asset_count="$(jq -r 'length' <<<"$matching_assets")"
if [[ "$asset_count" == "0" ]]; then
  fail "release asset metadata is missing expected ${asset_name}"
fi
if [[ "$asset_count" != "1" ]]; then
  fail "duplicate asset metadata for ${asset_name}"
fi

asset_id="$(jq -er '.[0].id | numbers' <<<"$matching_assets")"
asset_size="$(jq -er '.[0].size | numbers' <<<"$matching_assets")"
asset_digest="$(jq -er '.[0].digest | strings' <<<"$matching_assets")"

if [[ ! "$asset_digest" =~ ^sha256:[0-9a-fA-F]{64}$ ]]; then
  fail "release asset digest must be sha256"
fi

archive_path="${work_dir}/${asset_name}"
asset_url="${api_url}/repos/${GITHUB_REPOSITORY}/releases/assets/${asset_id}"

asset_headers="${work_dir}/asset-headers.txt"
asset_stage="${work_dir}/asset-download.bin"
asset_status="$(curl \
  --silent \
  --show-error \
  --fail \
  --header 'Accept: application/octet-stream' \
  --header "Authorization: Bearer ${GITHUB_TOKEN}" \
  --dump-header "$asset_headers" \
  --output "$asset_stage" \
  --write-out '%{http_code}' \
  "$asset_url")"

if [[ "$asset_status" == "200" ]]; then
  mv "$asset_stage" "$archive_path"
elif [[ "$asset_status" == "302" ]]; then
  redirect_url="$(awk 'BEGIN { IGNORECASE = 1 } /^Location:[[:space:]]*/ { sub(/\r$/, ""); sub(/^Location:[[:space:]]*/, ""); print; exit }' "$asset_headers")"
  if [[ -z "$redirect_url" ]]; then
    fail "asset redirect location is missing"
  fi
  if [[ ! "$redirect_url" =~ ^https:// ]]; then
    fail "asset redirect location is unsafe"
  fi

  curl \
    --silent \
    --show-error \
    --fail \
    --output "$archive_path" \
    "$redirect_url"
else
  fail "release asset download must return 200 or 302"
fi

actual_size="$(wc -c <"$archive_path" | tr -d '[:space:]')"
if [[ "$actual_size" != "$asset_size" ]]; then
  fail "size verification failed: expected ${asset_size}, got ${actual_size}"
fi

actual_digest="$(sha256_file "$archive_path")"
expected_digest="${asset_digest#sha256:}"
actual_digest_lower="$(printf '%s' "$actual_digest" | tr '[:upper:]' '[:lower:]')"
expected_digest_lower="$(printf '%s' "$expected_digest" | tr '[:upper:]' '[:lower:]')"
if [[ "$actual_digest_lower" != "$expected_digest_lower" ]]; then
  fail "sha256 verification failed"
fi

members_file="${work_dir}/archive-members.txt"
if ! tar -tzf "$archive_path" >"$members_file"; then
  fail "archive listing failed"
fi

member_count="$(wc -l <"$members_file" | tr -d '[:space:]')"
if [[ "$member_count" != "1" ]]; then
  fail "archive must contain exactly one file"
fi

member="$(sed -n '1p' "$members_file")"
member="${member#./}"
if [[ "$member" != "duto-ai" ]]; then
  fail "archive member traversal is not allowed: ${member}"
fi

extract_dir="${work_dir}/extract"
mkdir -p "$extract_dir"
tar -xzf "$archive_path" -C "$extract_dir"

candidate_path="${extract_dir}/duto-ai"
if [[ -L "$candidate_path" ]]; then
  fail "archive member must be regular file, symlink is not allowed"
fi
if [[ ! -f "$candidate_path" ]]; then
  fail "archive member must be regular file"
fi

install_dir="${RUNNER_TEMP}/duto-action-bin-${version}-${goos}-${goarch}"
mkdir -p "$install_dir"
install_path="${install_dir}/duto-ai"
cp "$candidate_path" "$install_path"
chmod 0755 "$install_path"

printf 'DUTO_ACTION_BIN=%s\n' "$install_path" >>"$GITHUB_ENV"
