#!/usr/bin/env bash
set -euo pipefail

for name in DUTO_ACTION_BIN INPUT_CONFIG INPUT_ARTIFACT_DIGEST INPUT_BUNDLE_SHA256 INPUT_PERMISSION_PROFILE GITHUB_TOKEN GITHUB_REPOSITORY GITHUB_REPOSITORY_ID GITHUB_REPOSITORY_OWNER_ID GITHUB_ACTOR_ID GITHUB_SHA GITHUB_REF GITHUB_WORKFLOW_SHA GITHUB_RUN_ID GITHUB_RUN_ATTEMPT GITHUB_WORKSPACE RUNNER_TEMP GITHUB_OUTPUT; do
  [[ -n "${!name:-}" ]] || { echo "error: $name is required" >&2; exit 2; }
done
[[ "$INPUT_CONFIG" != /* && "$INPUT_CONFIG" != *..* ]] || { echo 'error: config path is invalid' >&2; exit 2; }
[[ "$INPUT_ARTIFACT_DIGEST" =~ ^(sha256:)?[0-9a-fA-F]{64}$ ]] || { echo 'error: artifact digest is invalid' >&2; exit 2; }
[[ "$INPUT_BUNDLE_SHA256" =~ ^[0-9a-f]{64}$ ]] || { echo 'error: bundle digest is invalid' >&2; exit 2; }
[[ "$INPUT_PERMISSION_PROFILE" == reply || "$INPUT_PERMISSION_PROFILE" == branch-pr ]] || { echo 'error: permission profile is invalid' >&2; exit 2; }

bundle="${RUNNER_TEMP%/}/duto-publish-bundle"
[[ -f "$bundle/manifest.json" && -f "$bundle/control.json" ]] || { echo 'error: downloaded bundle is incomplete' >&2; exit 3; }
owner="${GITHUB_REPOSITORY%%/*}"
repository="${GITHUB_REPOSITORY#*/}"
correlation="$(jq -er '.admission.correlation_key' "$bundle/control.json")"
admission_id="$(jq -er '.admission.id' "$bundle/control.json")"
control="${RUNNER_TEMP%/}/duto-m3-publish-control.json"
receipt="${RUNNER_TEMP%/}/duto-m3-publisher-receipt.json"
issued_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
expires_at="$(date -u -d '+6 hours' '+%Y-%m-%dT%H:%M:%SZ')"

jq -n \
  --arg repository_id "$GITHUB_REPOSITORY_ID" --arg owner_id "$GITHUB_REPOSITORY_OWNER_ID" \
  --arg owner "$owner" --arg repository "$repository" --arg actor_id "$GITHUB_ACTOR_ID" \
  --arg ref "$GITHUB_REF" --arg sha "$GITHUB_SHA" --arg workflow_sha "$GITHUB_WORKFLOW_SHA" \
  --arg run_id "$GITHUB_RUN_ID" --argjson attempt "$GITHUB_RUN_ATTEMPT" \
  --arg admission_id "$admission_id" --arg correlation_key "$correlation" \
  --arg issued_at "$issued_at" --arg expires_at "$expires_at" \
  '{version:1,source:"github",repository:{id:$repository_id,owner_id:$owner_id,owner:$owner,name:$repository,default_branch:"main"},event:{name:"workflow_dispatch",actor_id:$actor_id,subject:{kind:"none",number:0},base:{repository_id:$repository_id,ref:$ref,sha:$sha},head:{repository_id:$repository_id,ref:$ref,sha:$sha}},run:{id:$run_id,attempt:$attempt,workflow_sha:$workflow_sha},checkout:{ref:$ref,sha:$sha},admission:{id:$admission_id,correlation_key:$correlation_key,issued_at:$issued_at,expires_at:$expires_at}}' > "$control"

"$DUTO_ACTION_BIN" publish --format json --config "$GITHUB_WORKSPACE/$INPUT_CONFIG" --control-evidence "$control" --bundle "$bundle" --expected-bundle-sha256 "$INPUT_BUNDLE_SHA256" --permission-profile "$INPUT_PERMISSION_PROFILE" --receipt "$receipt" > "${RUNNER_TEMP%/}/duto-m3-publisher-stdout.json"

disposition="$(jq -r '.disposition' "$receipt")"
reply_url="$(jq -r '[.operations[] | select(.kind == "conversation.reply") | .resource][0] // empty' "$receipt")"
branch="$(jq -r '[.operations[] | select(.kind == "git.branch.publish") | .resource][0] // empty' "$receipt")"
pr_url="$(jq -r '[.operations[] | select(.kind == "pull_request.create_draft") | .resource][0] // empty' "$receipt")"
{
  printf 'disposition=%s\n' "$disposition"
  printf 'reply-url=%s\n' "$reply_url"
  printf 'branch=%s\n' "$branch"
  printf 'pull-request-url=%s\n' "$pr_url"
  printf 'receipt-path=%s\n' "$receipt"
} >> "$GITHUB_OUTPUT"
