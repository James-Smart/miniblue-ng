provider "azurerm" {
  features {}

  # Point to the miniblue metadata endpoint (HTTPS).
  metadata_host = "localhost:4567"

  # azurerm 5.x no longer accepts skip_provider_registration; miniblue does not
  # need Azure resource-provider registration.

  # Mock credentials - miniblue accepts anything.
  subscription_id = "00000000-0000-0000-0000-000000000000"
  tenant_id       = "00000000-0000-0000-0000-000000000001"
  client_id       = "miniblue"
  client_secret   = "miniblue"
}
