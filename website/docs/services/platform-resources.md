# Platform Resources

miniblue provides minimal ARM create, read, list and delete support for a few
platform resources that are otherwise not modelled. The request body is stored
verbatim and echoed back with `id`, `name`, `type` and
`properties.provisioningState: Succeeded`.

| Resource | ARM type |
|----------|----------|
| Bastion hosts | `Microsoft.Network/bastionHosts` |
| Log Analytics workspaces | `Microsoft.OperationalInsights/workspaces` |
| Diagnostic settings | `Microsoft.Insights/diagnosticSettings` |
| Maintenance configurations | `Microsoft.Maintenance/maintenanceConfigurations` |

These resources have no behaviour beyond storage and echo. They are useful for
Terraform configurations that reference them without needing the underlying
Azure service.

## Endpoints

Resource-group scoped resources support:

| Method | Path |
|--------|------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/{namespace}/{type}/{name}` |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/{namespace}/{type}/{name}` |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/{namespace}/{type}/{name}` |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/{namespace}/{type}` |

Diagnostic settings can be attached to any resource ID:

```text
PUT /subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.KeyVault/vaults/{vault}/providers/Microsoft.Insights/diagnosticSettings/{name}
GET /subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.KeyVault/vaults/{vault}/providers/Microsoft.Insights/diagnosticSettings
DELETE /subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.KeyVault/vaults/{vault}/providers/Microsoft.Insights/diagnosticSettings/{name}
```

Diagnostic settings `PUT` always returns `200`, for both create and update, as
real Azure does; the diagnosticsettings SDK does not accept `201`. Other
platform resources return `201` on create and `200` on update.

### Log Analytics compatibility

`azurerm_log_analytics_workspace` checks for soft-deleted workspaces before
each create. Both of these return `{"value": []}` because workspace deletes are
immediate and nothing is recoverable:

```text
GET /subscriptions/{sub}/providers/Microsoft.OperationalInsights/deletedWorkspaces
GET /subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.OperationalInsights/deletedWorkspaces
```

`POST .../Microsoft.OperationalInsights/workspaces/{name}/sharedKeys` returns
deterministic base64 `primarySharedKey` and `secondarySharedKey` values for an
existing workspace.

## Example

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.OperationalInsights/workspaces/logs?api-version=2022-10-01" \
  -H "Content-Type: application/json" \
  -d '{"location":"eastus","properties":{"sku":{"name":"PerGB2018"}}}'
```

The response includes the stored body plus:

```json
{
  "id": "/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.OperationalInsights/workspaces/logs",
  "name": "logs",
  "type": "Microsoft.OperationalInsights/workspaces",
  "properties": {
    "sku": {"name": "PerGB2018"},
    "provisioningState": "Succeeded"
  }
}
```

Enable only these resources with `SERVICES=platform`.
