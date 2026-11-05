# Key Vault

miniblue emulates Azure Key Vault management-plane vaults and data-plane
secrets. azurerm 5.x can create, read, update and delete a vault, and the
vault's `vault_uri` points back to miniblue's local secret API.

## ARM management endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.KeyVault/vaults/{name}` | Create or update |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.KeyVault/vaults/{name}` | Get |
| `PATCH` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.KeyVault/vaults/{name}` | Update |
| `DELETE` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.KeyVault/vaults/{name}` | Soft-delete |
| `GET` | `/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.KeyVault/vaults` | List |
| `GET` | `/subscriptions/{sub}/providers/Microsoft.KeyVault/locations/{location}/deletedVaults/{name}` | Get soft-deleted vault |
| `POST` | `/subscriptions/{sub}/providers/Microsoft.KeyVault/locations/{location}/deletedVaults/{name}/purge` | Purge soft-deleted vault |

Vault `properties` are preserved across `PUT`/`PATCH`, including `tenantId`,
`sku`, access policies, network ACLs and tags. `provisioningState` is always
`Succeeded`, and `vaultUri` is generated as
`<scheme>://<metadata-host>/keyvault/<vault>`.

`DELETE` creates a location-scoped soft-delete tombstone. The azurerm default
`purge_soft_delete_on_destroy` feature calls the purge endpoint during destroy;
miniblue completes that operation synchronously.

## OpenTofu example

```hcl
resource "azurerm_key_vault" "example" {
  name                = "example-kv"
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name
  tenant_id           = "00000000-0000-0000-0000-000000000001"
  sku_name            = "standard"

  # Required by azurerm 5.x.
  rbac_authorization_enabled = false

  # Optional: skips the provider's data-plane readiness poll, so local applies
  # finish quickly. The ARM create/read path is the same either way.
  public_network_access_enabled = false
}
```

## Secret data-plane endpoints

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/keyvault/{vault}/secrets/{name}` | Set a secret |
| `GET` | `/keyvault/{vault}/secrets/{name}` | Get a secret |
| `DELETE` | `/keyvault/{vault}/secrets/{name}` | Delete a secret |
| `GET` | `/keyvault/{vault}/secrets` | List all secrets |

## Set a secret

```bash
curl -X PUT "http://localhost:4566/keyvault/myvault/secrets/db-password" \
  -H "Content-Type: application/json" \
  -d '{"value": "P@ssw0rd123!"}'
```

Response:

```json
{
  "id": "https://myvault.vault.azure.net/secrets/db-password",
  "value": "P@ssw0rd123!",
  "attributes": {
    "created": "2026-01-01T00:00:00Z",
    "enabled": "true",
    "updated": "2026-01-01T00:00:00Z"
  }
}
```

Setting the same secret name again overwrites the value.

## Get a secret

```bash
curl "http://localhost:4566/keyvault/myvault/secrets/db-password"
```

Returns the same JSON structure as above. Returns `404` if the secret does not exist.

## List all secrets in a vault

```bash
curl "http://localhost:4566/keyvault/myvault/secrets"
```

```json
{
  "value": [
    {
      "id": "https://myvault.vault.azure.net/secrets/db-password",
      "value": "P@ssw0rd123!",
      "attributes": {
        "created": "2026-01-01T00:00:00Z",
        "enabled": "true",
        "updated": "2026-01-01T00:00:00Z"
      }
    },
    {
      "id": "https://myvault.vault.azure.net/secrets/api-key",
      "value": "sk-abc123",
      "attributes": {
        "created": "2026-01-01T00:00:01Z",
        "enabled": "true",
        "updated": "2026-01-01T00:00:01Z"
      }
    }
  ]
}
```

## Delete a secret

```bash
curl -X DELETE "http://localhost:4566/keyvault/myvault/secrets/db-password"
```

Response: `200 OK`

## Multiple vaults

Vaults are separated by name. The data-plane API does not require explicit
vault creation -- secrets are scoped to whatever vault name you use in the
URL. ARM-created vaults and data-plane vault names share the same namespace.

```bash
# Different vaults, same secret name
curl -X PUT "http://localhost:4566/keyvault/prod-vault/secrets/api-key" \
  -H "Content-Type: application/json" \
  -d '{"value": "prod-key-123"}'

curl -X PUT "http://localhost:4566/keyvault/dev-vault/secrets/api-key" \
  -H "Content-Type: application/json" \
  -d '{"value": "dev-key-456"}'
```

## azlocal

```bash
# Set
azlocal keyvault secret set --vault myvault --name db-password --value "P@ssw0rd123!"

# Get
azlocal keyvault secret show --vault myvault --name db-password

# List
azlocal keyvault secret list --vault myvault

# Delete
azlocal keyvault secret delete --vault myvault --name db-password
```

## Full example

```bash
#!/bin/bash
set -e

VAULT="app-vault"

# Store application secrets
curl -X PUT "http://localhost:4566/keyvault/${VAULT}/secrets/db-host" \
  -H "Content-Type: application/json" \
  -d '{"value": "localhost"}'

curl -X PUT "http://localhost:4566/keyvault/${VAULT}/secrets/db-password" \
  -H "Content-Type: application/json" \
  -d '{"value": "supersecret"}'

curl -X PUT "http://localhost:4566/keyvault/${VAULT}/secrets/jwt-signing-key" \
  -H "Content-Type: application/json" \
  -d '{"value": "my-signing-key-256"}'

# List all secrets
curl -s "http://localhost:4566/keyvault/${VAULT}/secrets" | jq '.value[].id'

# Retrieve one
DB_PASS=$(curl -s "http://localhost:4566/keyvault/${VAULT}/secrets/db-password" | jq -r '.value')
echo "DB password: ${DB_PASS}"
```
