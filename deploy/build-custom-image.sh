#!/bin/bash
# build-custom-image.sh — Prepare a custom VM image with the log driver pre-installed
#
# Run this script on a VM created from the Azure Batch container-enabled base image
# (microsoft-azure-batch / ubuntu-server-container / 20-04-lts).
# After running, capture the VM as a managed image or Shared Image Gallery version.
#
# Required environment variables:
#   ACR_NAME          - ACR name without .azurecr.io
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
#   - Azure CLI installed and logged in (for ACR access during image build)
#   - Docker daemon running

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

echo "=== Building custom image with log driver plugin ==="
echo "Plugin: ${PLUGIN_FULL}"

# --- Login to ACR ---
echo "=== Logging into ACR ==="
az acr login --name "${ACR_NAME}"

# --- Install Plugin ---
echo "=== Installing plugin ==="
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

# --- Configure daemon.json ---
echo "=== Configuring daemon.json ==="
DAEMON_JSON="/etc/docker/daemon.json"

if [ -f "${DAEMON_JSON}" ]; then
  # Merge with existing daemon.json
  TEMP_JSON=$(mktemp)
  python3 -c "
import json
with open('${DAEMON_JSON}') as f:
    cfg = json.load(f)
cfg['log-driver'] = '${PLUGIN_FULL}'
cfg.setdefault('log-opts', {})
cfg['log-opts']['env-regex'] = '^AZ_BATCH_'
with open('${TEMP_JSON}', 'w') as f:
    json.dump(cfg, f, indent=2)
"
  mv "${TEMP_JSON}" "${DAEMON_JSON}"
else
  cat > "${DAEMON_JSON}" <<EOF
{
  "log-driver": "${PLUGIN_FULL}",
  "log-opts": {
    "env-regex": "^AZ_BATCH_"
  }
}
EOF
fi

echo "daemon.json:"
cat "${DAEMON_JSON}"

# --- Clean up for image capture ---
echo "=== Cleaning up for image capture ==="
# Remove ACR credentials (will use Managed Identity at runtime)
rm -f /root/.docker/config.json
rm -f /home/*/.docker/config.json 2>/dev/null || true

# Clear logs
truncate -s 0 /var/log/azure-batch-log-driver.log 2>/dev/null || true

echo ""
echo "=============================================="
echo "  Custom image setup complete!"
echo "=============================================="
echo ""
echo "Next steps:"
echo "  1. Deprovision the VM:  sudo waagent -deprovision+user -force"
echo "  2. Deallocate:          az vm deallocate -g <rg> -n <vm>"
echo "  3. Generalize:          az vm generalize -g <rg> -n <vm>"
echo "  4. Capture image:       az image create -g <rg> -n <image> --source <vm>"
echo "     Or use Shared Image Gallery for versioning."
echo ""
echo "On next boot, Docker will automatically:"
echo "  - Start the log driver plugin"
echo "  - Use it as the default log driver for ALL containers"
echo "  - Pass AZ_BATCH_* env vars to the log driver"
