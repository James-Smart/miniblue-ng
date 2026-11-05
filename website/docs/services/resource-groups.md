# Resource Groups

Resource groups are the top-level container for Azure resources. miniblue implements the full CRUD lifecycle following the Azure ARM API.

## API endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{name}` | Create or update |
| `PATCH` | `/subscriptions/{sub}/resourceGroups/{name}` | Update tags |
| `GET` | `/subscriptions/{sub}/resourceGroups/{name}` | Get by name |
| `HEAD` | `/subscriptions/{sub}/resourceGroups/{name}` | Check existence |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{name}` | Delete |
| `GET` | `/subscriptions/{sub}/resourceGroups` | List all |

The lowercase `/resourcegroups` spelling is also accepted for older clients.
azurerm 5.x uses the camel-case `/resourceGroups` form.

## Create a resource group

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG?api-version=2020-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "eastus",
    "tags": {
      "env": "dev",
      "team": "platform"
    }
  }'
```

Response (`201 Created`):

```json
{
  "id": "/subscriptions/sub1/resourceGroups/myRG",
  "name": "myRG",
  "type": "Microsoft.Resources/resourceGroups",
  "location": "eastus",
  "tags": {
    "env": "dev",
    "team": "platform"
  },
  "properties": {
    "provisioningState": "Succeeded"
  }
}
```

!!! note
    If the resource group already exists, the same `PUT` returns `200 OK` and updates it.

## Get a resource group

```bash
curl "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG?api-version=2020-06-01"
```

## Check existence

```bash
curl -I "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG?api-version=2020-06-01"
```

Returns `204 No Content` if it exists, `404 Not Found` otherwise.

## Update tags

```bash
curl -X PATCH "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG?api-version=2020-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "tags": {
      "env": "staging"
    }
  }'
```

## List all resource groups

```bash
curl "http://localhost:4566/subscriptions/sub1/resourceGroups?api-version=2020-06-01"
```

Response:

```json
{
  "value": [
    {
      "id": "/subscriptions/sub1/resourceGroups/myRG",
      "name": "myRG",
      "type": "Microsoft.Resources/resourceGroups",
      "location": "eastus",
      "properties": {
        "provisioningState": "Succeeded"
      }
    }
  ]
}
```

## Delete a resource group

```bash
curl -X DELETE "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG?api-version=2020-06-01"
```

Returns `202 Accepted` with a `Location` header for async polling. Deleting a resource group also cleans up all resources inside it (VNets, subnets, DNS zones, ACR registries, functions, Event Grid topics).

## List resources in a group

```bash
curl "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/resources?api-version=2020-06-01"
```

!!! info
    This endpoint currently returns an empty array. It exists for Terraform compatibility (the azurerm provider calls it before deleting a resource group).

## azlocal

```bash
azlocal group create --name myRG --location eastus
azlocal group list
azlocal group show --name myRG
azlocal group delete --name myRG
```

See the [azlocal CLI reference](../guides/azlocal.md) for full details.
