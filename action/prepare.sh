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

collect_workflow_tools() {
  local workflow_path="$1"

  awk '
    function trim(value) {
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      return value
    }

    {
      line = $0
      sub(/[[:space:]]+#.*$/, "", line)

      if (line ~ /^[[:space:]]*tools:[[:space:]]*\[[^]]*\][[:space:]]*$/) {
        inline = line
        sub(/^[^[]*\[/, "", inline)
        sub(/\][[:space:]]*$/, "", inline)
        count = split(inline, items, ",")
        for (i = 1; i <= count; i++) {
          item = trim(items[i])
          if (item != "") {
            print item
          }
        }
        next
      }

      if (line ~ /^[[:space:]]*tools:[[:space:]]*$/) {
        in_tools = 1
        tools_indent = match(line, /[^ ]/) - 1
        next
      }

      if (in_tools == 1) {
        if (line ~ /^[[:space:]]*$/) {
          next
        }

        current_indent = match(line, /[^ ]/) - 1
        if (current_indent <= tools_indent) {
          in_tools = 0
        }
      }

      if (in_tools == 1 && line ~ /^[[:space:]]*-[[:space:]]*/) {
        item = line
        sub(/^[[:space:]]*-[[:space:]]*/, "", item)
        item = trim(item)
        if (item != "") {
          print item
        }
      }
    }
  ' "$workflow_path" | LC_ALL=C sort -u
}

add_permission() {
  local permission="$1"
  local current

  for current in "${required_permissions[@]}"; do
    if [[ "$current" == "$permission" ]]; then
      return
    fi
  done

  required_permissions+=("$permission")
}

is_read_only_tool() {
  local tool_name="$1"

  case "$tool_name" in
    files.find|files.grep|files.read|git.read.blame|git.read.diff|git.read.log|git.read.show|github.read.changed-files|github.read.checks|github.read.comments|github.read.diff|github.read.issue|github.read.pr|github.read.reviews|github.read.search-issues|web.fetch)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

permission_probe_endpoint() {
  local permissions_csv="$1"
  local checkout_revision="$2"

  case ",$permissions_csv," in
    *,issues,*)
      printf '/repos/%s/issues?per_page=1\n' "$GITHUB_REPOSITORY"
      return
      ;;
    *,pull-requests,*)
      printf '/repos/%s/pulls?per_page=1\n' "$GITHUB_REPOSITORY"
      return
      ;;
    *,checks,*)
      printf '/repos/%s/commits/%s/check-runs?per_page=1\n' "$GITHUB_REPOSITORY" "$checkout_revision"
      return
      ;;
    *)
      printf '/repos/%s\n' "$GITHUB_REPOSITORY"
      return
      ;;
  esac
}

probe_github_permissions() {
  local permissions_csv="$1"
  local checkout_revision="$2"

  local token="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
  local api_url="${GITHUB_API_URL:-}"

  if [[ -z "$token" || -z "$api_url" ]]; then
    return
  fi

  local endpoint
  endpoint="$(permission_probe_endpoint "$permissions_csv" "$checkout_revision")"

  local http_code
  http_code="$(
    curl \
      --silent \
      --show-error \
      --output /dev/null \
      --write-out '%{http_code}' \
      --header 'Accept: application/vnd.github+json' \
      --header "Authorization: Bearer $token" \
      "${api_url}${endpoint}"
  )" || fail "protected API permission check request failed"

  if [[ ! "$http_code" =~ ^[0-9]{3}$ ]]; then
    fail "protected API permission check returned invalid status"
  fi

  if [[ "$http_code" == "401" || "$http_code" == "403" ]]; then
    fail "protected API permission check failed with HTTP ${http_code}"
  fi

  if ((10#$http_code >= 400)); then
    fail "protected API permission check failed with HTTP ${http_code}"
  fi
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

required_permissions=("contents")
needs_github_token=0
plan_is_read_only=1

while IFS= read -r tool_name; do
  if [[ -z "$tool_name" ]]; then
    continue
  fi

  case "$tool_name" in
    github.read.*)
      needs_github_token=1
      ;;
  esac

  case "$tool_name" in
    github.read.pr|github.read.diff|github.read.changed-files|github.read.reviews)
      add_permission "pull-requests"
      ;;
    github.read.issue|github.read.comments|github.read.search-issues)
      add_permission "issues"
      ;;
    github.read.checks)
      add_permission "checks"
      ;;
  esac

  if ! is_read_only_tool "$tool_name"; then
    plan_is_read_only=0
  fi
done < <(collect_workflow_tools "$workflow_path")

required_permissions_csv="$(printf '%s\n' "${required_permissions[@]}" | LC_ALL=C sort -u | paste -sd, -)"
printf 'DUTO_ACTION_REQUIRED_PERMISSIONS=%s\n' "$required_permissions_csv" >>"$GITHUB_ENV"
printf 'DUTO_ACTION_NEEDS_GITHUB_TOKEN=%s\n' "$needs_github_token" >>"$GITHUB_ENV"

trust_class="trusted"
if [[ "$GITHUB_EVENT_NAME" == "pull_request" ]]; then
  if [[ "$(jq -r '.fork // false' "$candidate_json")" == "true" ]]; then
    trust_class="fork"
  fi
elif [[ "$GITHUB_EVENT_NAME" == "issue_comment" ]]; then
  trust_class="unknown"
fi

if [[ "$trust_class" != "trusted" && "$plan_is_read_only" -ne 1 ]]; then
  fail "${trust_class} context is read-only: process-capable plan is not allowed"
fi

if [[ "$needs_github_token" -eq 1 ]]; then
  probe_github_permissions "$required_permissions_csv" "$checkout_head"
fi

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
