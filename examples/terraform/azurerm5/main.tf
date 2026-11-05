# Generic azurerm 5.x compatibility fixture for miniblue.
#
# Covers the four 5.x gaps:
#   - camel-case /resourceGroups routes
#   - public IP dnsSettings.fqdn derived from domain_name_label
#   - storage account accountMigrations/default read-back
#   - storage container read-back using the metadata storage suffix
#   - Key Vault ARM vault CRUD (including soft-delete purge on destroy)
#
# Run against miniblue:
#   bash scripts/trust-cert.sh
#   cd examples/terraform/azurerm5
#   tofu init
#   tofu apply -auto-approve
#   tofu plan -detailed-exitcode   # expect exit code 0 (no changes)
#   tofu destroy -auto-approve

resource "azurerm_resource_group" "example" {
  name     = "miniblue-az5-example"
  location = "eastus"
}

resource "azurerm_public_ip" "example" {
  name                = "miniblue-az5-pip"
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name
  allocation_method   = "Static"
  sku                 = "Standard"
  domain_name_label   = "miniblueaz5"
}

resource "azurerm_storage_account" "example" {
  name                     = "miniblueaz5sa"
  resource_group_name      = azurerm_resource_group.example.name
  location                 = azurerm_resource_group.example.location
  account_tier             = "Standard"
  account_replication_type = "LRS"
}

resource "azurerm_storage_container" "example" {
  name                  = "miniblue-container"
  storage_account_id    = azurerm_storage_account.example.id
  container_access_type = "private"
}

resource "azurerm_key_vault" "example" {
  name                = "miniblue-az5-kv"
  location            = azurerm_resource_group.example.location
  resource_group_name = azurerm_resource_group.example.name
  tenant_id           = "00000000-0000-0000-0000-000000000001"
  sku_name            = "standard"

  # Required by the azurerm 5.x schema.
  rbac_authorization_enabled = false

  # Disabling public access skips the provider's data-plane readiness poll,
  # which keeps local applies fast. ARM creation and read-back are identical.
  public_network_access_enabled = false
}

output "public_ip_fqdn" {
  value = azurerm_public_ip.example.fqdn
}

output "container_url" {
  value = azurerm_storage_container.example.url
}
