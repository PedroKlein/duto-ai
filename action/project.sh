#!/usr/bin/env bash
set -euo pipefail

summary_limit=$((8 * 1024))

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

  fail "sha256 utility is required"
}

sanitize_output_value() {
  local value="$1"
  value="${value//$'\r'/}"
  value="${value//$'\n'/ }"
  printf '%s' "$value"
}

write_output() {
  local key="$1"
  local value="$2"
  printf '%s=%s\n' "$key" "$(sanitize_output_value "$value")" >>"$GITHUB_OUTPUT"
}

create_redacted_events() {
  local source="$1"
  local target="$2"

  if [[ ! -s "$source" ]]; then
    : >"$target"
    chmod 0600 "$target"
    return
  fi

  jq -c '
    (.payload // {}) as $payload
    | {
        version: ((.version | numbers) // 1),
        sequence: ((.sequence | numbers) // 0),
        time: ((.time | strings) // ""),
        run_id: ((.run_id | strings) // ""),
        kind: ((.kind | strings) // ""),
        status: ((.status | strings) // ""),
        payload: {
          class: (($payload.class | strings) // ""),
          output_digest: (($payload.output_digest | strings) // ""),
          usage: (if ($payload.usage | type) == "object" then $payload.usage else null end),
          correlations: (if ($payload.correlations | type) == "array" then [
            $payload.correlations[]
            | {
                kind: ((.kind | strings) // ""),
                id: ((.id | strings) // ""),
                tool: ((.tool | strings) // "")
              }
          ] else [] end)
        }
      }
  ' "$source" >"$target"
  chmod 0600 "$target"
}

require_env GITHUB_OUTPUT
require_env GITHUB_STEP_SUMMARY
require_env RUNNER_TEMP
require_env DUTO_ACTION_RUNTIME_EVIDENCE_DIR
require_env DUTO_ACTION_ACTION_EVIDENCE_DIR

result_file="${DUTO_ACTION_RESULT_FILE:-}"
runtime_evidence_dir="$DUTO_ACTION_RUNTIME_EVIDENCE_DIR"
action_evidence_dir="$DUTO_ACTION_ACTION_EVIDENCE_DIR"
retention_days="${INPUT_EVIDENCE_RETENTION_DAYS:-7}"

has_result=false
status=""
outcome=""
run_id=""
failed_step=""
clarification_required=""

if [[ -n "$result_file" && -s "$result_file" ]]; then
  if ! jq -e 'type == "object" and (.status | type == "string") and (.run_id | type == "string")' "$result_file" >/dev/null; then
    fail "typed result file is invalid"
  fi

  has_result=true
  status="$(jq -r '.status' "$result_file")"
  outcome="$(jq -r '(.outcome // "") | if type == "string" then . else "" end' "$result_file")"
  run_id="$(jq -r '.run_id' "$result_file")"
  failed_step="$(jq -r '(.failed_step // "") | if type == "string" then . else "" end' "$result_file")"

  if [[ "$outcome" == "awaiting_input" ]]; then
    clarification_required="true"
  else
    clarification_required="false"
  fi
fi

if [[ -e "$action_evidence_dir" ]]; then
  fail "action evidence directory already exists"
fi

parent_dir="$(dirname "$action_evidence_dir")"
mkdir -p "$parent_dir"
temporary_dir="$(mktemp -d "$parent_dir/.duto-action-evidence.XXXXXX")"

redacted_events_path="$temporary_dir/events.jsonl"
receipt_path="$temporary_dir/receipt.json"
summary_path="$temporary_dir/summary.md"
manifest_path="$temporary_dir/manifest.json"

create_redacted_events "$runtime_evidence_dir/events.jsonl" "$redacted_events_path"

summary_status="$status"
summary_outcome="$outcome"
summary_clarification="$clarification_required"
summary_failed_step="$failed_step"
summary_run_id="$run_id"

if [[ -z "$summary_status" ]]; then
  summary_status="n/a"
fi
if [[ -z "$summary_outcome" ]]; then
  summary_outcome="n/a"
fi
if [[ -z "$summary_clarification" ]]; then
  summary_clarification="n/a"
fi
if [[ -z "$summary_failed_step" ]]; then
  summary_failed_step="n/a"
fi
if [[ -z "$summary_run_id" ]]; then
  summary_run_id="n/a"
fi

cat >"$summary_path" <<EOF
# Duto Action result

- Status: \`$summary_status\`
- Outcome: \`$summary_outcome\`
- Clarification required: \`$summary_clarification\`
- Failed step: \`$summary_failed_step\`
- Run ID: \`$summary_run_id\`
EOF
chmod 0600 "$summary_path"

summary_size=$(wc -c <"$summary_path" | tr -d '[:space:]')
if (( summary_size > summary_limit )); then
  fail "summary exceeds 8 KiB project ceiling"
fi

has_result_json=false
if [[ "$has_result" == "true" ]]; then
  has_result_json=true
fi

jq -n \
  --arg schema_version "duto.action.receipt/v1" \
  --arg status "$status" \
  --arg outcome "$outcome" \
  --arg run_id "$run_id" \
  --arg failed_step "$failed_step" \
  --arg clarification_required "$clarification_required" \
  --arg retention_days "$retention_days" \
  --argjson has_result "$has_result_json" \
  '{
    schema_version: $schema_version,
    has_typed_result: $has_result,
    status: $status,
    outcome: $outcome,
    run_id: $run_id,
    failed_step: $failed_step,
    clarification_required: $clarification_required,
    retention_days: $retention_days
  }' >"$receipt_path"
printf '\n' >>"$receipt_path"
chmod 0600 "$receipt_path"

prefix_source="${DUTO_ACTION_WORKFLOW_DIGEST_PREFIX:-${GITHUB_WORKFLOW_SHA:-}}"
prefix="$(printf '%s' "$prefix_source" | tr -cd '[:alnum:]' | cut -c1-16)"
if [[ -z "$prefix" ]]; then
  prefix="unknown"
fi

artifact_run_id="$(printf '%s' "$run_id" | tr -cd '[:alnum:]-')"
if [[ -z "$artifact_run_id" ]]; then
  artifact_run_id="unknown"
fi

artifact_name="duto-m2-evidence-${artifact_run_id}-${prefix}"

files_json='[]'
for file_name in events.jsonl receipt.json summary.md; do
  file_path="$temporary_dir/$file_name"
  file_size=$(wc -c <"$file_path" | tr -d '[:space:]')
  file_sha256="$(sha256_file "$file_path")"

  files_json="$(jq -c \
    --arg name "$file_name" \
    --argjson size "$file_size" \
    --arg sha256 "$file_sha256" \
    '. + [{name: $name, size: $size, sha256: $sha256}]' <<<"$files_json")"
done

jq -n \
  --arg schema_version "duto.action.evidence.manifest/v1" \
  --arg artifact_name "$artifact_name" \
  --arg retention_days "$retention_days" \
  --arg run_id "$run_id" \
  --arg status "$status" \
  --arg outcome "$outcome" \
  --argjson files "$files_json" \
  '{
    schema_version: $schema_version,
    artifact_name: $artifact_name,
    retention_days: $retention_days,
    run_id: $run_id,
    status: $status,
    outcome: $outcome,
    files: $files
  }' >"$manifest_path"
printf '\n' >>"$manifest_path"
chmod 0600 "$manifest_path"

mv "$temporary_dir" "$action_evidence_dir"

if [[ -n "${GITHUB_ENV:-}" ]]; then
  printf 'DUTO_ACTION_ARTIFACT_NAME=%s\n' "$artifact_name" >>"$GITHUB_ENV"
  printf 'DUTO_ACTION_ACTION_EVIDENCE_DIR=%s\n' "$action_evidence_dir" >>"$GITHUB_ENV"
fi

if [[ "$has_result" == "true" ]]; then
  result_output_path="$result_file"
else
  result_output_path=""
fi

write_output "status" "$status"
write_output "outcome" "$outcome"
write_output "run-id" "$run_id"
write_output "result-path" "$result_output_path"
write_output "evidence-path" "$action_evidence_dir"
write_output "failed-step" "$failed_step"
write_output "clarification-required" "$clarification_required"

cp "$action_evidence_dir/summary.md" "$GITHUB_STEP_SUMMARY"
