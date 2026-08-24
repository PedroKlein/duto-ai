#!/usr/bin/env bash
set -euo pipefail

duto_bin="${1:-duto-ai}"

workspace="${GITHUB_WORKSPACE:?GITHUB_WORKSPACE is required}"
workflow_input="${INPUT_WORKFLOW:?INPUT_WORKFLOW is required}"
config_input="${INPUT_CONFIG:-duto.yaml}"
inputs_file="${DUTO_ACTION_INPUTS_FILE:?DUTO_ACTION_INPUTS_FILE is required}"

evidence_dir="${DUTO_ACTION_EVIDENCE_DIR:-${RUNNER_TEMP:?RUNNER_TEMP is required}/duto-evidence}"
mkdir -p "${evidence_dir}"

"${duto_bin}" run \
  --format json \
  --config "${workspace}/${config_input}" \
  --inputs "${inputs_file}" \
  --evidence-directory "${evidence_dir}" \
  "${workspace}/${workflow_input}"
