# Storage Accounts

miniblue emulates Azure Storage Accounts via ARM endpoints with shared key authentication. Storage accounts provide the management layer on top of blob, queue, table and file storage.

## API endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Storage/storageAccounts/{name}` | Create or update |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Storage/storageAccounts/{name}` | Get |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Storage/storageAccounts/{name}` | Delete (`204`) |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Storage/storageAccounts` | List |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.Storage/storageAccounts/{name}/accountMigrations/default` | Migration status |

## Migration status

azurerm 5.x reads `accountMigrations/default` after fetching a storage account.
miniblue returns a completed migration record whose `targetSkuName` matches the
account SKU, so the read-back succeeds without a real migration.

## Endpoint suffix

Storage account endpoints use the same host suffix that
`/metadata/endpoints` advertises under `suffixes.storage`:

```text
https://{accountName}.blob.localhost:4566/
https://{accountName}.queue.localhost:4566/
https://{accountName}.table.localhost:4566/
https://{accountName}.file.localhost:4566/
```

The suffix follows `PORT`. azurerm 5.x parses these endpoints using the
metadata suffix when reading a container back, so keeping them aligned is
required for `azurerm_storage_container` read and destroy.

Set `MINIBLUE_STORAGE_ENDPOINT` to switch to path-style local endpoints for
direct data-plane SDK testing.

## Containers

ARM blob-container CRUD is available under
`.../storageAccounts/{account}/blobServices/default/containers/{container}`.
`DELETE` returns `204`, matching what azurerm 5.x expects for
`azurerm_storage_container` destroy.

## Create a storage account

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Storage/storageAccounts/mystorageacct?api-version=2023-05-01" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "eastus",
    "kind": "StorageV2",
    "sku": {"name": "Standard_LRS"}
  }'
```

## Terraform

```hcl
resource "azurerm_storage_account" "example" {
  name                     = "examplestorage"
  resource_group_name      = azurerm_resource_group.example.name
  location                 = azurerm_resource_group.example.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
}
```

## Shared key authentication

miniblue supports Azure shared key authentication for storage data plane operations. Keys are auto-generated when a storage account is created.

## Limitations

- No lifecycle management policies
- No private endpoints or firewall rules
- No geo-replication
- File storage data plane is not yet implemented
