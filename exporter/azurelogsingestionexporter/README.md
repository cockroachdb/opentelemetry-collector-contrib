# Azure Logs Ingestion Exporter

The Azure Logs Ingestion exporter sends OpenTelemetry logs to Azure Monitor Logs through the Logs Ingestion API using a Data Collection Endpoint (DCE), Data Collection Rule (DCR), and Microsoft Entra ID client credential authentication.

This exporter is logs-only. It expects upstream processors to shape each log record body into the schema accepted by the target DCR stream. When the log body is a map, the exporter passes those body fields through as top-level JSON fields and adds `TimeGenerated`. When the log body is not a map, it exports the body as `message`.

## Configuration

Required settings:

- `endpoint`: DCE logs ingestion endpoint, for example `https://<dce-name>.<region>.ingest.monitor.azure.com`.
- `rule_id`: Immutable DCR ID, for example `dcr-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx`.
- `stream_name`: DCR stream name, for example `Custom-CockroachDBLogs_CL`.
- `tenant_id`: Microsoft Entra ID tenant ID.
- `client_id`: App registration client ID.
- `client_secret`: App registration client secret.

Optional exporter helper settings:

- `timeout`
- `sending_queue`
- `retry_on_failure`

Example:

```yaml
exporters:
  azurelogsingestion:
    endpoint: https://my-dce.eastus-1.ingest.monitor.azure.com
    rule_id: dcr-00000000000000000000000000000000
    stream_name: Custom-CockroachDBLogs_CL
    tenant_id: ${env:AZURE_TENANT_ID}
    client_id: ${env:AZURE_CLIENT_ID}
    client_secret: ${env:AZURE_CLIENT_SECRET}
    timeout: 30s
    sending_queue:
      enabled: true
      queue_size: 1000
    retry_on_failure:
      enabled: true
      initial_interval: 5s
      max_interval: 30s
      max_elapsed_time: 120s
```

## Payload Shape

For a log record body like:

```json
{
  "severity": "ERROR",
  "channel": "SQL_EXEC",
  "message": "query failed",
  "node_id": 3,
  "cloud_cluster_id": "cluster-123"
}
```

the exporter sends:

```json
{
  "severity": "ERROR",
  "channel": "SQL_EXEC",
  "message": "query failed",
  "node_id": 3,
  "cloud_cluster_id": "cluster-123",
  "TimeGenerated": "2026-05-27T12:00:00Z"
}
```

## Limits

Azure Monitor Logs Ingestion API requests are limited to 1 MB. The exporter chunks batches below that limit using a 900 KB target payload size.

Records that exceed the target payload size by themselves are dropped individually because they can never fit in a valid API request. If every record in a batch is oversized, the exporter returns a permanent error so the collector does not retry the same invalid data.

Azure also truncates individual field values longer than 64 KB.
