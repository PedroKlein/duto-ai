#!/usr/bin/env bash
set -euo pipefail

: "${INPUT_VERSION:?INPUT_VERSION is required}"
: "${DUTO_ACTION_REPOSITORY:?DUTO_ACTION_REPOSITORY is required}"
: "${RUNNER_OS:?RUNNER_OS is required}"
: "${RUNNER_ARCH:?RUNNER_ARCH is required}"
: "${RUNNER_TEMP:?RUNNER_TEMP is required}"
: "${GITHUB_ENV:?GITHUB_ENV is required}"

if [[ "${DUTO_ACTION_ALLOW_PREINSTALLED:-}" == 1 && -n "${DUTO_ACTION_BIN:-}" && -x "$DUTO_ACTION_BIN" ]]; then
  printf 'DUTO_ACTION_BIN=%s\n' "$DUTO_ACTION_BIN" >> "$GITHUB_ENV"
  exit 0
fi

[[ "$INPUT_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'error: invalid duto-ai version' >&2; exit 2; }
case "$RUNNER_OS" in Linux) os=linux ;; macOS) os=darwin ;; *) echo 'error: unsupported runner OS' >&2; exit 2 ;; esac
case "$RUNNER_ARCH" in X64) arch=amd64 ;; ARM64) arch=arm64 ;; *) echo 'error: unsupported runner architecture' >&2; exit 2 ;; esac

stage="$(mktemp -d "${RUNNER_TEMP%/}/duto-install.XXXXXX")"
trap 'rm -rf "$stage"' EXIT
base="https://github.com/${DUTO_ACTION_REPOSITORY}/releases/download/${INPUT_VERSION}"
archive="duto-ai_${os}_${arch}.tar.gz"
curl --proto '=https' --tlsv1.2 --location --silent --show-error --fail --output "$stage/$archive" "$base/$archive"
curl --proto '=https' --tlsv1.2 --location --silent --show-error --fail --output "$stage/checksums.txt" "$base/checksums.txt"
(
  cd "$stage"
  grep -E "^[0-9a-fA-F]{64}[[:space:]]+${archive}$" checksums.txt > selected.checksum
  [[ "$(wc -l < selected.checksum | tr -d ' ')" == 1 ]]
  shasum -a 256 -c selected.checksum
  tar -xzf "$archive" duto-ai
)
chmod 0755 "$stage/duto-ai"
install_dir="${RUNNER_TEMP%/}/duto-ai-${INPUT_VERSION}-${os}-${arch}"
rm -rf "$install_dir"
mkdir -p "$install_dir"
mv "$stage/duto-ai" "$install_dir/duto-ai"
printf 'DUTO_ACTION_BIN=%s\n' "$install_dir/duto-ai" >> "$GITHUB_ENV"
