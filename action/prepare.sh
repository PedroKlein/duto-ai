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

resolve_directory_path() {
  local dir="$1"
  (
    cd "$dir" 2>/dev/null
    pwd -P
  )
}

resolve_file_path() {
  local file="$1"
  local parent
  local base

  parent="$(dirname "$file")"
  base="$(basename "$file")"

  printf '%s/%s\n' "$(resolve_directory_path "$parent")" "$base"
}

assert_workspace_file() {
  local label="$1"
  local relative_path="$2"

  if [[ -z "$relative_path" ]]; then
    fail "$label path is required"
  fi

  if [[ "$relative_path" == /* ]]; then
    fail "$label path must be relative"
  fi

  local candidate="$workspace_abs/$relative_path"

  if [[ ! -e "$candidate" ]]; then
    fail "$label path is missing"
  fi

  if [[ -L "$candidate" ]]; then
    fail "$label path symlink is not allowed"
  fi

  if [[ ! -f "$candidate" ]]; then
    fail "$label path must be a regular file"
  fi

  local absolute_path
  absolute_path="$(resolve_file_path "$candidate")"

  if [[ "$absolute_path" != "$workspace_abs"/* ]]; then
    fail "$label path traversal outside workspace is not allowed"
  fi

  printf '%s\n' "$absolute_path"
}

collect_declared_inputs() {
  local workflow_path="$1"

  awk '
    /^inputs:[[:space:]]*$/ { in_inputs=1; next }
    in_inputs {
      if ($0 ~ /^[^[:space:]]/) { exit }
      if ($0 ~ /^  [^[:space:]][^:]*:[[:space:]]*$/) {
        key = $0
        sub(/^  /, "", key)
        sub(/:[[:space:]]*$/, "", key)
        print key
      }
    }
  ' "$workflow_path"
}

expected_checkout_revision() {
  case "$GITHUB_EVENT_NAME" in
    pull_request)
      jq -er '.pull_request.base.sha // empty' "$event_path"
      ;;
    push)
      jq -er '.after // empty' "$event_path"
      ;;
    *)
      printf '%s\n' "$GITHUB_SHA"
      ;;
  esac
}

require_env GITHUB_WORKSPACE
require_env GITHUB_EVENT_PATH
require_env GITHUB_EVENT_NAME
require_env GITHUB_SHA
require_env GITHUB_REPOSITORY
require_env GITHUB_REPOSITORY_OWNER
require_env GITHUB_REPOSITORY_ID
require_env GITHUB_ACTOR
require_env GITHUB_ACTOR_ID
require_env GITHUB_REF
require_env GITHUB_WORKFLOW_SHA
require_env GITHUB_RUN_ID
require_env RUNNER_TEMP
require_env GITHUB_ENV

workspace="$GITHUB_WORKSPACE"
if [[ ! -d "$workspace" ]]; then
  fail "GITHUB_WORKSPACE must resolve to an existing directory"
fi
workspace_abs="$(resolve_directory_path "$workspace")"

if [[ ! -e "$GITHUB_EVENT_PATH" ]]; then
  fail "GITHUB_EVENT_PATH must resolve to a readable file"
fi
if [[ -L "$GITHUB_EVENT_PATH" || ! -f "$GITHUB_EVENT_PATH" ]]; then
  fail "GITHUB_EVENT_PATH must be a regular file"
fi
event_path="$(resolve_file_path "$GITHUB_EVENT_PATH")"

workflow_input="${INPUT_WORKFLOW:-}"
config_input="${INPUT_CONFIG:-duto.yaml}"

workflow_path="$(assert_workspace_file workflow "$workflow_input")"
config_path="$(assert_workspace_file config "$config_input")"

checkout_head="$(git -C "$workspace_abs" rev-parse HEAD 2>/dev/null)" || fail "checkout head is unavailable"
expected_revision="$(expected_checkout_revision 2>/dev/null)" || fail "trusted revision evidence is missing"
if [[ -z "$expected_revision" ]]; then
  fail "trusted revision evidence is missing"
fi

if [[ "$checkout_head" != "$expected_revision" ]]; then
  if [[ "$GITHUB_EVENT_NAME" == "push" ]]; then
    fail "stale revision evidence: checkout HEAD does not match push revision"
  fi
  if [[ "$GITHUB_EVENT_NAME" == "pull_request" ]]; then
    fail "checkout head/base mismatch: checkout HEAD does not match pull_request base.sha"
  fi
  fail "checkout head mismatch: checkout HEAD does not match trusted revision"
fi

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
map_jq="$script_dir/map-inputs.jq"
if [[ ! -f "$map_jq" ]]; then
  fail "missing map-inputs.jq"
fi

candidate_json="$(mktemp "$RUNNER_TEMP/duto-action-candidate.XXXXXX.json")"
input_json="$(mktemp "$RUNNER_TEMP/duto-action-inputs.XXXXXX.json")"
trap 'rm -f "$candidate_json"' EXIT

DUTO_ACTION_CHECKOUT_HEAD="$checkout_head" jq -S -c -f "$map_jq" "$event_path" >"$candidate_json"

declared_inputs=()
while IFS= read -r declared; do
  declared_inputs+=("$declared")
done < <(collect_declared_inputs "$workflow_path")

closed_input_keys=(
  "event-name"
  "repository-owner"
  "repository-name"
  "repository-id"
  "actor"
  "actor-id"
  "revision"
  "ref"
  "workflow-revision"
  "host-run-id"
  "subject-kind"
  "subject-number"
  "base-revision"
  "head-revision"
  "base-repository-id"
  "head-repository-id"
  "fork"
  "comment-id"
)

for declared in "${declared_inputs[@]}"; do
  found=0
  for closed_key in "${closed_input_keys[@]}"; do
    if [[ "$declared" == "$closed_key" ]]; then
      found=1
      break
    fi
  done

  if [[ "$found" -ne 1 ]]; then
    fail "declared input \"$declared\" is not-in-closed-map"
  fi
done

if [[ ${#declared_inputs[@]} -eq 0 ]]; then
  declared_json='[]'
else
  declared_json="$(printf '%s\n' "${declared_inputs[@]}" | jq -R . | jq -s .)"
fi

jq -S -c --argjson declared "$declared_json" 'with_entries(select(.key as $k | $declared | index($k)))' "$candidate_json" >"$input_json"
chmod 0600 "$input_json"

printf 'DUTO_ACTION_INPUTS_FILE=%s\n' "$input_json" >>"$GITHUB_ENV"

# config_path is validated for trust admission in this phase.
: "$config_path"
