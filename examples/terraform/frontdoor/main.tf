# Generic Azure Front Door Standard fixture for miniblue.
#
# Creates a resource group, Front Door profile, endpoint, origin group, origin,
# route, rule set, rule, custom domain, security policy and a WAF policy. It
# uses only public example values and no private hostnames or DNS zones.
#
# Run against miniblue:
#   bash scripts/trust-cert.sh
#   cd examples/terraform/frontdoor
#   tofu init                              # or: terraform init
#   tofu apply -auto-approve
#   tofu plan -detailed-exitcode           # expect exit code 0 (no changes)
#   tofu destroy -auto-approve
#
# Front Door is metadata-only in miniblue: the tree applies, refreshes and
# destroys, but no traffic is routed and no WAF rule is evaluated.

resource "azurerm_resource_group" "example" {
  name     = "miniblue-frontdoor-example"
  location = "eastus"

  tags = {
    environment = "example"
  }
}

resource "azurerm_cdn_frontdoor_profile" "example" {
  name                     = "miniblue-fd-profile"
  resource_group_name      = azurerm_resource_group.example.name
  sku_name                 = "Standard_AzureFrontDoor"
  response_timeout_seconds = 60

  tags = {
    environment = "example"
  }
}

resource "azurerm_cdn_frontdoor_endpoint" "example" {
  name                     = "miniblue-fd-endpoint"
  cdn_frontdoor_profile_id = azurerm_cdn_frontdoor_profile.example.id

  tags = {
    environment = "example"
  }
}

resource "azurerm_cdn_frontdoor_origin_group" "example" {
  name                                                      = "miniblue-fd-origins"
  cdn_frontdoor_profile_id                                  = azurerm_cdn_frontdoor_profile.example.id
  session_affinity_enabled                                  = false
  restore_traffic_time_to_healed_or_new_endpoint_in_minutes = 10

  load_balancing {
    additional_latency_in_milliseconds = 0
    sample_size                        = 4
    successful_samples_required        = 3
  }

  health_probe {
    interval_in_seconds = 30
    path                = "/"
    protocol            = "Http"
    request_type        = "HEAD"
  }
}

resource "azurerm_cdn_frontdoor_origin" "example" {
  name                           = "miniblue-fd-origin"
  cdn_frontdoor_origin_group_id  = azurerm_cdn_frontdoor_origin_group.example.id
  enabled                        = true
  certificate_name_check_enabled = true
  host_name                      = "origin.example.com"
  origin_host_header             = "origin.example.com"
  http_port                      = 80
  https_port                     = 443
  priority                       = 1
  weight                         = 1000
}

resource "azurerm_cdn_frontdoor_rule_set" "example" {
  name                     = "MiniblueRuleSet"
  cdn_frontdoor_profile_id = azurerm_cdn_frontdoor_profile.example.id
}

resource "azurerm_cdn_frontdoor_rule" "example" {
  depends_on = [
    azurerm_cdn_frontdoor_origin_group.example,
    azurerm_cdn_frontdoor_origin.example,
  ]

  name                      = "MiniblueResponseHeader"
  cdn_frontdoor_rule_set_id = azurerm_cdn_frontdoor_rule_set.example.id
  order                     = 1
  behavior_on_match         = "Continue"

  actions {
    response_header_action {
      header_action = "Append"
      header_name   = "X-Miniblue-Example"
      value         = "true"
    }
  }

  conditions {
    request_method_condition {
      match_values = ["GET"]
      operator     = "Equal"
    }
  }
}

resource "azurerm_cdn_frontdoor_custom_domain" "example" {
  name                     = "miniblue-fd-custom-domain"
  cdn_frontdoor_profile_id = azurerm_cdn_frontdoor_profile.example.id
  host_name                = "www.example.com"

  tls {
    certificate_type    = "ManagedCertificate"
    minimum_tls_version = "TLS12"
  }
}

resource "azurerm_cdn_frontdoor_route" "example" {
  name                            = "miniblue-fd-route"
  cdn_frontdoor_endpoint_id       = azurerm_cdn_frontdoor_endpoint.example.id
  cdn_frontdoor_origin_group_id   = azurerm_cdn_frontdoor_origin_group.example.id
  cdn_frontdoor_origin_ids        = [azurerm_cdn_frontdoor_origin.example.id]
  cdn_frontdoor_rule_set_ids      = [azurerm_cdn_frontdoor_rule_set.example.id]
  cdn_frontdoor_custom_domain_ids = [azurerm_cdn_frontdoor_custom_domain.example.id]

  forwarding_protocol    = "MatchRequest"
  https_redirect_enabled = true
  link_to_default_domain = true
  patterns_to_match      = ["/*"]
  supported_protocols    = ["Http", "Https"]

  cache {
    query_string_caching_behavior = "IgnoreQueryString"
    compression_enabled           = true
    content_types_to_compress     = ["text/html", "text/css", "application/javascript"]
  }
}

resource "azurerm_cdn_frontdoor_firewall_policy" "example" {
  name                       = "minibluefdwaf"
  resource_group_name        = azurerm_resource_group.example.name
  sku_name                   = azurerm_cdn_frontdoor_profile.example.sku_name
  enabled                    = true
  mode                       = "Prevention"
  request_body_check_enabled = true

  custom_rule {
    name     = "BlockNonGet"
    enabled  = true
    priority = 1
    type     = "MatchRule"
    action   = "Block"

    match_condition {
      match_variable     = "RequestMethod"
      operator           = "Equal"
      negation_condition = true
      match_values       = ["GET"]
    }
  }

  tags = {
    environment = "example"
  }
}

resource "azurerm_cdn_frontdoor_security_policy" "example" {
  name                     = "miniblue-fd-security-policy"
  cdn_frontdoor_profile_id = azurerm_cdn_frontdoor_profile.example.id

  security_policies {
    firewall {
      cdn_frontdoor_firewall_policy_id = azurerm_cdn_frontdoor_firewall_policy.example.id

      association {
        domain {
          cdn_frontdoor_domain_id = azurerm_cdn_frontdoor_custom_domain.example.id
        }
        patterns_to_match = ["/*"]
      }
    }
  }
}
