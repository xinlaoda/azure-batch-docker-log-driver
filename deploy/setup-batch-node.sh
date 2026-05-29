#!/bin/bash
# setup-batch-node.sh — Azure Batch pool start task script
# Installs the azure-batch-docker-log-driver plugin on the node.
#
# The plugin is NOT set as the default log driver in daemon.json to avoid
# a chicken-and-egg problem (Docker needs the plugin to start, but the plugin
# needs Docker to run). Instead, specify --log-driver per-task in
# containerRunOptions, or use a custom OS image with daemon.json pre-configured.
#
# Usage: Run as a Batch pool start task with elevated privileges.
#
# Required environment variables (set via Batch pool env settings):
#   ACR_NAME          - Azure Container Registry name (e.g., myacr) without .azurecr.io
#   DCE_ENDPOINT      - Azure Monitor Data Collection Endpoint URL
#   DCR_IMMUTABLE_ID  - Data Collection Rule immutable ID
#   DCR_STREAM_NAME   - DCR stream name (e.g., Custom-BatchLogs_CL)
#
# Optional:
#   PLUGIN_TAG                 - Plugin version tag (default: latest)
#   MANAGED_IDENTITY_CLIENT_ID - User-assigned MI client ID (omit for system-assigned)
#   BUFFER_MAX_SIZE            - Max entries before flush (default: 1000)
#   BUFFER_MAX_INTERVAL        - Max interval between flushes (default: 5s)
#   DEBUG                      - Enable debug logging (default: false)
#
# Prerequisites:
#   - The Batch pool VM must have a Managed Identity with:
#     * "AcrPull" role on the ACR (to pull the plugin image)
#     * "Monitoring Metrics Publisher" role on the DCR (to send logs)

set -euo pipefail

ACR_NAME="${ACR_NAME:?ACR_NAME is required}"
PLUGIN_TAG="${PLUGIN_TAG:-latest}"
DCE_ENDPOINT="${DCE_ENDPOINT:?DCE_ENDPOINT is required}"
DCR_IMMUTABLE_ID="${DCR_IMMUTABLE_ID:?DCR_IMMUTABLE_ID is required}"
DCR_STREAM_NAME="${DCR_STREAM_NAME:?DCR_STREAM_NAME is required}"
MANAGED_IDENTITY_CLIENT_ID="${MANAGED_IDENTITY_CLIENT_ID:-}"
BUFFER_MAX_SIZE="${BUFFER_MAX_SIZE:-1000}"
BUFFER_MAX_INTERVAL="${BUFFER_MAX_INTERVAL:-5s}"
DEBUG="${DEBUG:-false}"

ACR_LOGIN_SERVER="${ACR_NAME}.azurecr.io"
PLUGIN_FULL="${ACR_LOGIN_SERVER}/azure-batch-docker-log-driver:${PLUGIN_TAG}"

# --- ACR Login via Managed Identity ---
echo "=== Logging into ACR ${ACR_LOGIN_SERVER} via Managed Identity ==="

MI_PARAM=""
if [ -n "${MANAGED_IDENTITY_CLIENT_ID}" ]; then
  MI_PARAM="&client_id=${MANAGED_IDENTITY_CLIENT_ID}"
fi

ARM_TOKEN=$(curl -sf \
  "http://169.254.169.254/metadata/identity/oauth2/token?api-version=2018-02-01&resource=https://management.azure.com/${MI_PARAM}" \
  -H "Metadata:true" | python3 -c "import sys,json; print(json.load(sys.stdin)['access_token'])")

ACR_REFRESH_TOKEN=$(curl -sf -X POST \
  "https://${ACR_LOGIN_SERVER}/oauth2/exchange" \
  -d "grant_type=access_token&service=${ACR_LOGIN_SERVER}&access_token=${ARM_TOKEN}" \
  | python3 -c "import sys,json; print(json.load(sys.stdin)['refresh_token'])")

echo "${ACR_REFRESH_TOKEN}" | docker login "${ACR_LOGIN_SERVER}" \
  -u "00000000-0000-0000-0000-000000000000" --password-stdin

echo "=== ACR login successful ==="

# --- Install Plugin ---
echo "=== Installing Docker log driver plugin: ${PLUGIN_FULL} ==="

docker plugin rm -f "${PLUGIN_FULL}" 2>/dev/null || true

docker plugin install "${PLUGIN_FULL}" \
  --grant-all-permissions \
  DCE_ENDPOINT="${DCE_ENDPOINT}" \
  DCR_IMMUTABLE_ID="${DCR_IMMUTABLE_ID}" \
  DCR_STREAM_NAME="${DCR_STREAM_NAME}" \
  MANAGED_IDENTITY_CLIENT_ID="${MANAGED_IDENTITY_CLIENT_ID}" \
  BUFFER_MAX_SIZE="${BUFFER_MAX_SIZE}" \
  BUFFER_MAX_INTERVAL="${BUFFER_MAX_INTERVAL}" \
  DEBUG="${DEBUG}"

echo "=== Plugin installed ==="
docker plugin ls

echo "=== Setup complete ==="
echo "NOTE: The plugin is NOT set as default log driver."
echo "Use --log-driver in containerRunOptions per-task, or build a custom OS image."