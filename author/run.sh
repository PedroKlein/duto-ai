#!/usr/bin/env bash
set -euo pipefail

for name in DUTO_ACTION_BIN INPUT_WORKFLOW INPUT_CONFIG INPUT_CORRELATION_KEY GITHUB_REPOSITORY GITHUB_REPOSITORY_ID GITHUB_REPOSITORY_OWNER_ID GITHUB_ACTOR_ID GITHUB_SHA GITHUB_REF GITHUB_WORKFLOW_SHA GITHUB_RUN_ID GITHUB_RUN_ATTEMPT GITHUB_WORKSPACE RUNNER_TEMP GITHUB_OUTPUT GITHUB_ENV; do
  [[ -n "${!name:-}" ]] || { echo "error: $name is required" >&2; exit 2; }
done
[[ "$INPUT_WORKFLOW" != /* && "$INPUT_CONFIG" != /* ]] || { echo 'error: paths must be relative' >&2; exit 2; }
[[ "$INPUT_WORKFLOW" != *..* && "$INPUT_CONFIG" != *..* ]] || { echo 'error: traversal is forbidden' >&2; exit 2; }

owner="${GITHUB_REPOSITORY%%/*}"
repository="${GITHUB_REPOSITORY#*/}"
control="${RUNNER_TEMP%/}/duto-m3-control.json"
bundle="${RUNNER_TEMP%/}/duto-m3-bundle"
result="${RUNNER_TEMP%/}/duto-m3-result.json"
issued_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
expires_at="$(date -u -d '+6 hours' '+%Y-%m-%dT%H:%M:%SZ')"

jq -n \
  --arg source github --arg repository_id "$GITHUB_REPOSITORY_ID" --arg owner_id "$GITHUB_REPOSITORY_OWNER_ID" \
  --arg owner "$owner" --arg repository "$repository" --arg actor_id "$GITHUB_ACTOR_ID" \
  --arg ref "$GITHUB_REF" --arg sha "$GITHUB_SHA" --arg workflow_sha "$GITHUB_WORKFLOW_SHA" \
  --arg run_id "$GITHUB_RUN_ID" --argjson attempt "$GITHUB_RUN_ATTEMPT" \
  --arg admission_id focused-m3 --arg correlation_key "$INPUT_CORRELATION_KEY" \
  --arg issued_at "$issued_at" --arg expires_at "$expires_at" \
  '{version:1,source:$source,repository:{id:$repository_id,owner_id:$owner_id,owner:$owner,name:$repository,default_branch:"main"},event:{name:"workflow_dispatch",actor_id:$actor_id,subject:{kind:"none",number:0},base:{repository_id:$repository_id,ref:$ref,sha:$sha},head:{repository_id:$repository_id,ref:$ref,sha:$sha}},run:{id:$run_id,attempt:$attempt,workflow_sha:$workflow_sha},checkout:{ref:$ref,sha:$sha},admission:{id:$admission_id,correlation_key:$correlation_key,issued_at:$issued_at,expires_at:$expires_at}}' > "$control"

rm -rf "$bundle"
"$DUTO_ACTION_BIN" run --format json --config "$GITHUB_WORKSPACE/$INPUT_CONFIG" --control-evidence "$control" --evidence-directory "$bundle" "$GITHUB_WORKSPACE/$INPUT_WORKFLOW" > "$result"
[[ -f "$bundle/manifest.json" ]] || { echo 'error: M3 manifest is missing' >&2; exit 1; }
jq -e '.version == 2 and .bundle_kind == "m3-authoring" and .completion == "succeeded"' "$bundle/manifest.json" >/dev/null
bundle_sha="$(shasum -a 256 "$bundle/manifest.json" | awk '{print $1}')"
status="$(jq -r '.status // empty' "$result")"
outcome="$(jq -r '.output.outcome // .outcome // empty' "$result")"
run_id="$(jq -r '.run_id' "$bundle/manifest.json")"
operation_set="$(jq -r '.operation_set' "$bundle/manifest.json")"
clarification=false
[[ "$outcome" == awaiting_input ]] && clarification=true
{
  printf 'status=%s\n' "$status"
  printf 'outcome=%s\n' "$outcome"
  printf 'run-id=%s\n' "$run_id"
  printf 'clarification-required=%s\n' "$clarification"
  printf 'operation-set=%s\n' "$operation_set"
  printf 'bundle-sha256=%s\n' "$bundle_sha"
  printf 'bundle-path=%s\n' "$bundle"
} >> "$GITHUB_OUTPUT"
printf 'DUTO_M3_BUNDLE_PATH=%s\n' "$bundle" >> "$GITHUB_ENV"
