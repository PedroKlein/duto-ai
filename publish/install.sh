#!/usr/bin/env bash
set -euo pipefail

: "${DUTO_ACTION_PATH:?DUTO_ACTION_PATH is required}"
bash "${DUTO_ACTION_PATH}/author/install.sh"
