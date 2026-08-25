#!/usr/bin/env bash
set -euo pipefail

fail() {
  echo "error: $*" >&2
  exit 1
}

write_run_exit_code() {
  local code="$1"
  if [[ -n "${GITHUB_ENV:-}" ]]; then
    printf 'DUTO_ACTION_RUN_EXIT_CODE=%s\n' "$code" >>"$GITHUB_ENV"
  fi
}

duto_bin="${1:-duto-ai}"

workspace="${GITHUB_WORKSPACE:?GITHUB_WORKSPACE is required}"
workflow_input="${INPUT_WORKFLOW:?INPUT_WORKFLOW is required}"
config_input="${INPUT_CONFIG:-duto.yaml}"
inputs_file="${DUTO_ACTION_INPUTS_FILE:?DUTO_ACTION_INPUTS_FILE is required}"

token_required="${DUTO_ACTION_NEEDS_GITHUB_TOKEN:-0}"
case "$token_required" in
  0|1) ;;
  *)
    fail "DUTO_ACTION_NEEDS_GITHUB_TOKEN is invalid"
    ;;
esac

if [[ "$token_required" == "1" ]]; then
  if [[ -z "${GITHUB_TOKEN:-}" && -z "${GH_TOKEN:-}" ]]; then
    fail "github token is required for admitted GitHub read tools"
  fi
else
  unset GITHUB_TOKEN
  unset GH_TOKEN
fi

runtime_evidence_dir="${DUTO_ACTION_RUNTIME_EVIDENCE_DIR:-${DUTO_ACTION_EVIDENCE_DIR:-${RUNNER_TEMP:?RUNNER_TEMP is required}/duto-evidence}}"
if [[ -e "$runtime_evidence_dir" || -L "$runtime_evidence_dir" ]]; then
  fail "runtime evidence directory must be fresh"
fi

result_file="${DUTO_ACTION_RESULT_FILE:-}"
if [[ -n "$result_file" ]]; then
  result_parent="$(dirname "$result_file")"
  mkdir -p "$result_parent"
  : >"$result_file"
  chmod 0600 "$result_file"
fi

set +e
if [[ -n "$result_file" ]]; then
  "${duto_bin}" run \
    --format json \
    --config "${workspace}/${config_input}" \
    --inputs "${inputs_file}" \
    --evidence-directory "${runtime_evidence_dir}" \
    "${workspace}/${workflow_input}" >"$result_file"
  exit_code=$?
else
  "${duto_bin}" run \
    --format json \
    --config "${workspace}/${config_input}" \
    --inputs "${inputs_file}" \
    --evidence-directory "${runtime_evidence_dir}" \
    "${workspace}/${workflow_input}"
  exit_code=$?
fi
set -e

if [[ "$exit_code" =~ ^[0-9]+$ ]]; then
  write_run_exit_code "$exit_code"
else
  fail "runtime exit code is invalid"
fi

exit "$exit_code"
