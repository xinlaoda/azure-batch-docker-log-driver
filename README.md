# Azure Batch Docker Log Driver

A Docker managed plugin that captures **stdout/stderr** from Azure Batch container tasks and sends them to **Azure Monitor Log Analytics** custom tables via the [Logs Ingestion API](https://learn.microsoft.com/en-us/azure/azure-monitor/logs/logs-ingestion-api-overview).

Each log entry is enriched with Azure Batch metadata (task ID, job ID, pool ID, node ID, account name) for structured querying and alerting in Log Analytics.

## Architecture

```
Docker Engine ──FIFO (protobuf)──► Plugin ──HTTPS (MI)──► Azure Monitor
                                    │                      DCE → DCR → Log Analytics
                                    │                      Custom Table
                                    ├─ Decode protobuf log stream
                                    ├─ Extract AZ_BATCH_* env vars
                                    ├─ Buffer & batch entries
                                    └─ Retry with exponential backoff
```

## Log Analytics Table Schema

| Column | Type | Description |
|---|---|---|
| `TimeGenerated` | datetime | Log entry timestamp |
| `BatchAccountName` | string | Azure Batch account name |
| `PoolId` | string | Batch pool ID |
| `NodeId` | string | Compute node ID |
| `JobId` | string | Batch job ID |
| `TaskId` | string | Batch task ID |
| `ContainerName` | string | Docker container name |
| `Stream` | string | `stdout` or `stderr` |
| `LogMessage` | string | Log line content |

## Prerequisites

### Azure Resources

1. **Log Analytics Workspace** with a custom table (e.g., `BatchLogs_CL`)
2. **Data Collection Endpoint (DCE)**
3. **Data Collection Rule (DCR)** configured with:
   - Input stream mapped to the custom table
   - Transformation KQL (can be passthrough: `source`)
4. **User-Assigned Managed Identity** assigned to the Batch pool VMs with the **Monitoring Metrics Publisher** role on the DCR

### Create Azure Resources (Azure CLI)

```bash
# Variables
RG="myResourceGroup"
WORKSPACE="myWorkspace"
DCE_NAME="myDCE"
DCR_NAME="myDCR"
LOCATION="eastus"
MI_NAME="batch-log-driver-mi"

# 1. Create Log Analytics workspace (if not exists)
az monitor log-analytics workspace create \
  --resource-group $RG --workspace-name $WORKSPACE --location $LOCATION

WORKSPACE_ID=$(az monitor log-analytics workspace show \
  --resource-group $RG --workspace-name $WORKSPACE --query id -o tsv)

# 2. Create custom table
az monitor log-analytics workspace table create \
  --resource-group $RG --workspace-name $WORKSPACE \
  --name BatchLogs_CL \
  --columns TimeGenerated=datetime BatchAccountName=string PoolId=string \
            NodeId=string JobId=string TaskId=string ContainerName=string \
            Stream=string LogMessage=string

# 3. Create Data Collection Endpoint
az monitor data-collection endpoint create \
  --resource-group $RG --name $DCE_NAME --location $LOCATION

DCE_ENDPOINT=$(az monitor data-collection endpoint show \
  --resource-group $RG --name $DCE_NAME \
  --query logsIngestion.endpoint -o tsv)

# 4. Create Data Collection Rule
az monitor data-collection rule create \
  --resource-group $RG --name $DCR_NAME --location $LOCATION \
  --data-collection-endpoint-id $(az monitor data-collection endpoint show \
      --resource-group $RG --name $DCE_NAME --query id -o tsv) \
  --data-flows '[{"streams":["Custom-BatchLogs_CL"],"destinations":["logAnalytics"],"transformKql":"source"}]' \
  --destinations '{"logAnalytics":[{"workspaceResourceId":"'$WORKSPACE_ID'","name":"logAnalytics"}]}' \
  --stream-declarations '{"Custom-BatchLogs_CL":{"columns":[
    {"name":"TimeGenerated","type":"datetime"},
    {"name":"BatchAccountName","type":"string"},
    {"name":"PoolId","type":"string"},
    {"name":"NodeId","type":"string"},
    {"name":"JobId","type":"string"},
    {"name":"TaskId","type":"string"},
    {"name":"ContainerName","type":"string"},
    {"name":"Stream","type":"string"},
    {"name":"LogMessage","type":"string"}
  ]}}'

DCR_IMMUTABLE_ID=$(az monitor data-collection rule show \
  --resource-group $RG --name $DCR_NAME --query immutableId -o tsv)

# 5. Create Managed Identity and assign role
az identity create --resource-group $RG --name $MI_NAME --location $LOCATION
MI_PRINCIPAL=$(az identity show --resource-group $RG --name $MI_NAME --query principalId -o tsv)
MI_CLIENT_ID=$(az identity show --resource-group $RG --name $MI_NAME --query clientId -o tsv)

DCR_ID=$(az monitor data-collection rule show --resource-group $RG --name $DCR_NAME --query id -o tsv)
az role assignment create \
  --assignee $MI_PRINCIPAL \
  --role "Monitoring Metrics Publisher" \
  --scope $DCR_ID

echo "DCE_ENDPOINT=$DCE_ENDPOINT"
echo "DCR_IMMUTABLE_ID=$DCR_IMMUTABLE_ID"
echo "DCR_STREAM_NAME=Custom-BatchLogs_CL"
echo "MI_CLIENT_ID=$MI_CLIENT_ID"
```

## Building

```bash
# Build and create the Docker managed plugin
make all

# Or step by step:
make build     # Build Go binary in Docker
make rootfs    # Create plugin rootfs
make create    # Register Docker plugin
make enable    # Enable the plugin
```

## Plugin Configuration

Configuration is set at plugin install/enable time via environment variables:

| Setting | Required | Default | Description |
|---|---|---|---|
| `DCE_ENDPOINT` | Yes | — | Data Collection Endpoint URL |
| `DCR_IMMUTABLE_ID` | Yes | — | Data Collection Rule immutable ID |
| `DCR_STREAM_NAME` | Yes | — | DCR stream name (e.g., `Custom-BatchLogs_CL`) |
| `MANAGED_IDENTITY_CLIENT_ID` | No | system-assigned | User-assigned MI client ID |
| `BUFFER_MAX_SIZE` | No | `1000` | Max entries before flush |
| `BUFFER_MAX_INTERVAL` | No | `5s` | Max time before flush |

## Installation

### Pushing to ACR

After building the plugin locally, push it to Azure Container Registry:

```bash
# Login to ACR
az acr login --name myacr

# Tag and push (plugin must be created with the ACR name)
docker plugin create myacr.azurecr.io/azure-batch-docker-log-driver:v1 ./plugin
docker plugin push myacr.azurecr.io/azure-batch-docker-log-driver:v1
```

The Managed Identity on your Batch pool VMs needs `AcrPull` role on the ACR to pull
the plugin image during installation.

### Azure Batch Deployment

There are two deployment strategies. Both install the plugin once per node; subsequent
task runs reference the locally cached plugin by name (no repeated ACR pulls).

---

#### Strategy A: Start Task + Per-Task Log Driver

**Best for**: Quick setup, testing, or when you cannot build custom OS images.

**How it works**:
1. Pool **start task** installs the plugin from ACR (runs once when node joins pool)
2. Each task specifies `--log-driver` in `containerRunOptions`

**Step 1 — Pool Configuration (ARM / Batch Management API)**

```json
{
  "identity": {
    "type": "UserAssigned",
    "userAssignedIdentities": {
      "/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ManagedIdentity/userAssignedIdentities/{mi-name}": {}
    }
  },
  "properties": {
    "vmSize": "Standard_D2s_v3",
    "deploymentConfiguration": {
      "virtualMachineConfiguration": {
        "imageReference": {
          "publisher": "microsoft-azure-batch",
          "offer": "ubuntu-server-container",
          "sku": "20-04-lts",
          "version": "latest"
        },
        "nodeAgentSkuId": "batch.node.ubuntu 20.04",
        "containerConfiguration": { "type": "DockerCompatible" }
      }
    },
    "startTask": {
      "commandLine": "/bin/bash -c 'echo <BASE64_ENCODED_SCRIPT> | base64 -d | bash'",
      "userIdentity": {
        "autoUser": { "scope": "Pool", "elevationLevel": "Admin" }
      },
      "maxTaskRetryCount": 2,
      "waitForSuccess": true
    }
  }
}
```

The start task script (`deploy/setup-batch-node.sh`) does the following:
1. Logs into ACR using Managed Identity (IMDS → ARM token → ACR refresh token)
2. Runs `docker plugin install` with all configuration env vars
3. Verifies the plugin is enabled

Required environment variables for the start task:

| Variable | Required | Description |
|---|---|---|
| `ACR_NAME` | Yes | ACR name without `.azurecr.io` |
| `DCE_ENDPOINT` | Yes | Data Collection Endpoint URL |
| `DCR_IMMUTABLE_ID` | Yes | Data Collection Rule immutable ID |
| `DCR_STREAM_NAME` | Yes | DCR stream name (e.g., `Custom-BatchLogs_CL`) |
| `MANAGED_IDENTITY_CLIENT_ID` | No | User-assigned MI client ID |
| `PLUGIN_TAG` | No | Plugin version (default: `latest`) |
| `DEBUG` | No | Enable verbose logging (default: `false`) |

**Step 2 — Task Configuration**

Each task must specify the log driver in `containerSettings.containerRunOptions`:

```json
{
  "id": "my-task",
  "commandLine": "/bin/bash -c 'echo Hello World'",
  "containerSettings": {
    "imageName": "myacr.azurecr.io/my-app:latest",
    "containerRunOptions": "--log-driver myacr.azurecr.io/azure-batch-docker-log-driver:v1 --log-opt env-regex=^AZ_BATCH_"
  }
}
```

> **Note**: The `--log-driver` value is the **local plugin name** (same as the ACR URL
> used during `docker plugin install`). Docker looks it up locally — it does NOT pull
> from ACR on every task run.

> **Why not daemon.json?** Setting the plugin as the default log driver in daemon.json
> requires `systemctl restart docker`. This creates a chicken-and-egg problem: Docker
> needs the plugin to start, but the plugin is a Docker-managed container that needs
> Docker to run. The restart fails because Docker cannot start the plugin fast enough.

**Step 3 — Managed Identity Roles**

The VM's Managed Identity needs:
- `AcrPull` on the ACR — to pull the plugin image during start task
- `Monitoring Metrics Publisher` on the DCR — to send logs to Azure Monitor

```bash
MI_PRINCIPAL=$(az identity show -g myRG -n myMI --query principalId -o tsv)
az role assignment create --assignee $MI_PRINCIPAL --role AcrPull --scope $ACR_ID
az role assignment create --assignee $MI_PRINCIPAL --role "Monitoring Metrics Publisher" --scope $DCR_ID
```

---

#### Strategy B: Custom OS Image + daemon.json

**Best for**: Production deployments where all tasks should automatically use the log driver.

**How it works**:
1. Build a custom VM image with the plugin pre-installed and daemon.json configured
2. On VM boot, Docker starts → loads the pre-registered plugin → daemon.json takes effect
3. ALL container tasks automatically use the log driver (no `containerRunOptions` needed)

**Step 1 — Build the Custom Image**

Start from an Azure Batch container-enabled base image and customize it using
[Azure Image Builder](https://learn.microsoft.com/en-us/azure/virtual-machines/image-builder-overview),
[Packer](https://www.packer.io/), or a manual VM snapshot.

```bash
#!/bin/bash
# build-custom-image.sh — Run on a base VM to prepare the custom image
# Base image: microsoft-azure-batch / ubuntu-server-container / 20-04-lts

set -ex

# Variables (replace with your values)
ACR_NAME="myacr"
ACR_LOGIN_SERVER="${ACR_NAME}.azurecr.io"
PLUGIN_TAG="v1"
PLUGIN_FULL="${ACR_LOGIN_SERVER}/azure-batch-docker-log-driver:${PLUGIN_TAG}"

DCE_ENDPOINT="https://my-dce.westus2-1.ingest.monitor.azure.com"
DCR_IMMUTABLE_ID="dcr-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
DCR_STREAM_NAME="Custom-BatchLogs_CL"
MANAGED_IDENTITY_CLIENT_ID=""  # Leave empty for system-assigned MI

# 1. Login to ACR (use az acr login, SP, or admin creds during image build)
az acr login --name ${ACR_NAME}

# 2. Install the plugin
docker plugin install ${PLUGIN_FULL} \
  --grant-all-permissions \
  DCE_ENDPOINT="${DCE_ENDPOINT}" \
  DCR_IMMUTABLE_ID="${DCR_IMMUTABLE_ID}" \
  DCR_STREAM_NAME="${DCR_STREAM_NAME}" \
  MANAGED_IDENTITY_CLIENT_ID="${MANAGED_IDENTITY_CLIENT_ID}" \
  BUFFER_MAX_SIZE="1000" \
  BUFFER_MAX_INTERVAL="5s" \
  DEBUG="false"

# 3. Verify plugin is installed and enabled
docker plugin ls

# 4. Configure daemon.json to use the plugin as default log driver
cat > /etc/docker/daemon.json <<EOF
{
  "log-driver": "${PLUGIN_FULL}",
  "log-opts": {
    "env-regex": "^AZ_BATCH_"
  }
}
EOF

echo "=== Custom image setup complete ==="
echo "Capture this VM as an image now."
echo "On next boot, Docker will auto-start the plugin and use it as default."
```

After running this script, capture the VM as a managed image or Shared Image Gallery version.

**Step 2 — Pool Configuration Using Custom Image**

```json
{
  "identity": {
    "type": "UserAssigned",
    "userAssignedIdentities": {
      "/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.ManagedIdentity/userAssignedIdentities/{mi-name}": {}
    }
  },
  "properties": {
    "vmSize": "Standard_D2s_v3",
    "deploymentConfiguration": {
      "virtualMachineConfiguration": {
        "imageReference": {
          "id": "/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Compute/galleries/{gallery}/images/{image}/versions/{version}"
        },
        "nodeAgentSkuId": "batch.node.ubuntu 20.04",
        "containerConfiguration": { "type": "DockerCompatible" }
      }
    }
  }
}
```

**Step 3 — Task Configuration (No containerRunOptions needed)**

With the custom image, the log driver is the default for all containers:

```json
{
  "id": "my-task",
  "commandLine": "/bin/bash -c 'echo Hello World'",
  "containerSettings": {
    "imageName": "myacr.azurecr.io/my-app:latest"
  }
}
```

No `containerRunOptions` needed — every container automatically uses the log driver.

> **Why does daemon.json work here but not with start task?**
> When building a custom image, the plugin is pre-registered in Docker's local plugin
> store. On next boot, Docker starts the plugin process before reading daemon.json.
> With a start task, `docker plugin install` + `systemctl restart docker` race against
> each other — Docker restarts but the plugin isn't ready yet, causing a deadlock.

---

### Strategy Comparison

| | Strategy A (Start Task) | Strategy B (Custom Image) |
|---|---|---|
| **Setup complexity** | Low — just configure start task | Medium — requires image build pipeline |
| **Task configuration** | Must add `containerRunOptions` to every task | No task changes needed |
| **Plugin updates** | Change start task → reboot nodes | Rebuild image → reimage pool |
| **daemon.json as default** | ❌ Not possible (chicken-and-egg) | ✅ Works |
| **Best for** | Dev/test, quick proof-of-concept | Production |

### Coexistence with Batch Local Logs

Azure Batch captures container stdout/stderr to local files (`stdout.txt`, `stderr.txt`)
via `docker attach`, which is **independent of the log driver**. Both work simultaneously:

- **Log driver** → sends structured logs with Batch metadata to Azure Monitor
- **Batch local files** → captured via `docker attach`, available via Batch API/Storage

There is currently no way to disable Batch's local log capture. If you don't need the
local files, simply don't configure `outputFiles` to upload them to Storage.

## Querying Logs

Once logs are flowing to Log Analytics, query them with KQL:

```kusto
// All logs for a specific job
BatchLogs_CL
| where JobId == "myjob-20250101"
| order by TimeGenerated asc

// Error logs across all tasks in a pool
BatchLogs_CL
| where PoolId == "gpu-pool" and Stream == "stderr"
| order by TimeGenerated desc
| take 100

// Task duration estimation
BatchLogs_CL
| summarize StartTime=min(TimeGenerated), EndTime=max(TimeGenerated) by JobId, TaskId
| extend Duration = EndTime - StartTime

// Log volume per task
BatchLogs_CL
| summarize LogCount=count(), TotalBytes=sum(strlen(LogMessage)) by JobId, TaskId
| order by TotalBytes desc
```

## Testing

```bash
go test -v -race ./...
```

## Development

The plugin communicates with Docker via a UNIX socket at `/run/docker/plugins/azure-batch-log-driver.sock`. Docker sends log messages through a FIFO as protobuf-encoded `LogEntry` messages (4-byte big-endian length prefix + protobuf payload).

Key source files:
- `main.go` — Plugin entry point and HTTP handler registration
- `driver.go` — Docker LogDriver interface (StartLogging/StopLogging/ReadLogs)
- `logpair.go` — Per-container FIFO consumer and protobuf decoder
- `azure.go` — Azure Monitor Ingestion API client with retry
- `buffer.go` — Log batching with size and time-based flush
- `batch_metadata.go` — AZ_BATCH_* environment variable extraction
- `config.go` — Plugin configuration parsing

## Limitations

- **Linux only** — Docker managed plugins are not supported on Windows
- **Ingestion latency** — Buffer flush interval + Log Analytics ingestion delay (~2-5 min)
- **Memory** — If Azure Monitor is unreachable, failed batches are re-enqueued up to 10× buffer size, then dropped
- **daemon.json** — Cannot set as default log driver via start task (chicken-and-egg problem with Docker restart). Use custom OS image for daemon-level default, or specify per-task.
- **Batch local logs** — Batch always captures stdout/stderr to local files via `docker attach`; this cannot be disabled and coexists with the log driver

## License

MIT
