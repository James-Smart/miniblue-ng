# Front Door

miniblue emulates the Azure Front Door Standard/Premium **control plane** under `Microsoft.Cdn/profiles`, plus the resource-group-scoped `Microsoft.Network/FrontDoorWebApplicationFirewallPolicies` resource. Profiles, endpoints, origin groups, origins, routes, rule sets, rules, custom domains, secrets, security policies and WAF policies can be created, read, updated, listed and deleted through ARM, so `azurerm_cdn_frontdoor_*` resources apply, refresh and destroy cleanly.

This is metadata emulation: no traffic is routed, no TLS certificate is issued and no WAF rule is evaluated. The responses carry the computed properties the `azurerm` provider reads back, so a plan immediately after an apply reports no drift.

## Supported resource tree

```
Microsoft.Cdn/profiles/{profile}
├── afdEndpoints/{endpoint}
│   └── routes/{route}
├── originGroups/{group}
│   └── origins/{origin}
├── ruleSets/{ruleSet}
│   └── rules/{rule}
├── customDomains/{domain}
├── secrets/{secret}
└── securityPolicies/{policy}

Microsoft.Network/FrontDoorWebApplicationFirewallPolicies/{policy}   (resource-group scoped)
```

WAF policies are a `Microsoft.Network` resource, not a child of the profile. They are listed and deleted per resource group, and a profile delete leaves them alone.

## API endpoints

Paths are abbreviated; the full prefix is
`/subscriptions/{sub}/resourceGroups/{rg}/providers`.

| Resource | Methods | Path |
|----------|---------|------|
| Profile | `PUT`, `GET`, `PATCH`, `DELETE` | `Microsoft.Cdn/profiles/{profile}` |
| Profiles (resource group) | `GET` | `Microsoft.Cdn/profiles` |
| Profiles (subscription) | `GET` | `/subscriptions/{sub}/providers/Microsoft.Cdn/profiles` |
| Endpoint | `PUT`, `GET`, `PATCH`, `DELETE` | `.../profiles/{profile}/afdEndpoints/{endpoint}` |
| Endpoints | `GET` | `.../profiles/{profile}/afdEndpoints` |
| Route | `PUT`, `GET`, `PATCH`, `DELETE` | `.../afdEndpoints/{endpoint}/routes/{route}` |
| Routes | `GET` | `.../afdEndpoints/{endpoint}/routes` |
| Origin group | `PUT`, `GET`, `PATCH`, `DELETE` | `.../profiles/{profile}/originGroups/{group}` |
| Origin groups | `GET` | `.../profiles/{profile}/originGroups` |
| Origin | `PUT`, `GET`, `PATCH`, `DELETE` | `.../originGroups/{group}/origins/{origin}` |
| Origins | `GET` | `.../originGroups/{group}/origins` |
| Rule set | `PUT`, `GET`, `PATCH`, `DELETE` | `.../profiles/{profile}/ruleSets/{ruleSet}` |
| Rule sets | `GET` | `.../profiles/{profile}/ruleSets` |
| Rule | `PUT`, `GET`, `PATCH`, `DELETE` | `.../ruleSets/{ruleSet}/rules/{rule}` |
| Rules | `GET` | `.../ruleSets/{ruleSet}/rules` |
| Custom domain | `PUT`, `GET`, `PATCH`, `DELETE` | `.../profiles/{profile}/customDomains/{domain}` |
| Custom domains | `GET` | `.../profiles/{profile}/customDomains` |
| Secret | `PUT`, `GET`, `PATCH`, `DELETE` | `.../profiles/{profile}/secrets/{secret}` |
| Secrets | `GET` | `.../profiles/{profile}/secrets` |
| Security policy | `PUT`, `GET`, `PATCH`, `DELETE` | `.../profiles/{profile}/securityPolicies/{policy}` |
| Security policies | `GET` | `.../profiles/{profile}/securityPolicies` |
| WAF policy | `PUT`, `GET`, `PATCH`, `DELETE` | `Microsoft.Network/frontDoorWebApplicationFirewallPolicies/{policy}` |
| WAF policies (resource group) | `GET` | `Microsoft.Network/frontDoorWebApplicationFirewallPolicies` |
| WAF policies (subscription) | `GET` | `/subscriptions/{sub}/providers/Microsoft.Network/frontDoorWebApplicationFirewallPolicies` |

Both `frontDoorWebApplicationFirewallPolicies` and
`FrontDoorWebApplicationFirewallPolicies` path spellings are registered, because
ARM callers differ in casing. WAF responses always use
`Microsoft.Network/FrontDoorWebApplicationFirewallPolicies`.

All `PUT` calls return `201` for a new resource and `200` for an existing one. `PATCH` applies merge semantics and returns `200`; it never creates a resource. `DELETE` returns `202` and cascades as described below.

## Create a profile and endpoint

```bash
curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Cdn/profiles/my-profile?api-version=2024-02-01" \
  -H "Content-Type: application/json" \
  -d '{
    "location": "global",
    "sku": { "name": "Standard_AzureFrontDoor" },
    "properties": { "originResponseTimeoutSeconds": 60 },
    "tags": { "environment": "test" }
  }'

curl -X PUT "http://localhost:4566/subscriptions/sub1/resourceGroups/myRG/providers/Microsoft.Cdn/profiles/my-profile/afdEndpoints/my-endpoint?api-version=2024-02-01" \
  -H "Content-Type: application/json" \
  -d '{"location":"global","properties":{"enabledState":"Enabled"}}'
```

The endpoint response contains a generated host name:

```json
{
  "name": "my-endpoint",
  "type": "Microsoft.Cdn/profiles/afdEndpoints",
  "properties": {
    "hostName": "my-endpoint-1a2b3c.z01.azurefd.net",
    "enabledState": "Enabled",
    "deploymentStatus": "NotStarted",
    "provisioningState": "Succeeded",
    "resourceState": "Active"
  }
}
```

`PUT` accepts a partial model: omitted fields are filled with the same defaults
Azure returns. `PATCH` merges recursively, so a tags-only or property-only update
does not require sending the whole resource.

## Computed values

Every `Microsoft.Cdn` resource gets `properties.provisioningState: "Succeeded"`,
`properties.resourceState: "Active"`, `etag: "W/\"miniblue\""`, an empty `tags`
map when none is supplied, and `location: "global"` when omitted. Resources with a
deployment status also get `properties.deploymentStatus: "NotStarted"`. WAF
policies use their own envelope: `location` is always `"Global"`, `tags`
defaults to `{}` and `provisioningState` is `Succeeded`.

| Resource | Computed / defaulted values |
|----------|-----------------------------|
| Profile | `properties.frontDoorId` — a stable UUID generated once (the provider exposes it as `resource_guid`); `originResponseTimeoutSeconds` defaults to `60` |
| Endpoint | `properties.hostName` — generated once as `<lowercased name>-<6 hex>.z01.azurefd.net` and preserved on every later read; `enabledState` defaults to `Enabled` |
| Origin group | `healthProbeSettings` defaults (`/`, `HEAD`, `Http`, `30s`), `loadBalancingSettings` defaults (`4` samples, `3` required, `0 ms`), `sessionAffinityState` `Disabled`, traffic restoration `10` minutes |
| Origin | `originHostHeader` defaults to `hostName`; ports `80`/`443`, `priority` `1`, `weight` `1000`, `enabledState` `Enabled`, `enforceCertificateNameCheck` `true` |
| Route | `supportedProtocols` `["Https"]`, `patternsToMatch` `["/*"]`, `forwardingProtocol` `MatchRequest`, `linkToDefaultDomain` `Enabled`, `httpsRedirect` `Enabled`, `enabledState` `Enabled`, empty `customDomains` and `ruleSets` arrays; `endpointName` is filled from the parent endpoint |
| Rule set / rule | `deploymentStatus`; rules also get `matchProcessingBehavior: "Continue"` and empty `conditions`/`actions` arrays. Condition and action bodies are echoed verbatim and never validated |
| Custom domain | `validationProperties.validationToken` — 32 lowercase hex characters, generated once; `expirationDate` — create time plus 7 days; `validationState` and `domainValidationState` — `Pending` |
| Secret | `CustomerCertificate` secrets get an `expirationDate` and an empty `subjectAlternativeNames` array when omitted |
| Security policy | Every association domain is stamped `isActive: true` when the caller did not supply it |
| WAF policy | `location` `Global`; `policySettings` defaults (`enabledState` `Enabled`, `mode` `Prevention`, `requestBodyCheck` `Enabled`); `customRules.rules` defaults to `[]`; `frontendEndpointLinks` is always `[]` |

Generated values are persisted, not recomputed. Repeated `GET`s, `PATCH`es and
full-model `PUT`s return the same host name, Front Door ID, validation token and
expiration date.

## Cascade and association behaviour

- Deleting a profile removes every endpoint, route, origin group, origin, rule set, rule, custom domain, secret and security policy beneath it. WAF policies survive because they belong to the resource group.
- Deleting an endpoint removes its routes; deleting an origin group removes its origins; deleting a rule set removes its rules.
- Custom-domain association is client-side in the provider: the domain ID lives in the route's `properties.customDomains` array. Deleting a custom domain scrubs its ID from every route in the profile, so a stale association cannot break the next plan.
- Deleting a secret does not chase custom-domain TLS references.
- Deleting the resource group removes the whole Front Door footprint, including WAF policies, with one prefix delete.

## Terraform

The `azurerm` provider resources for this surface are:

- `azurerm_cdn_frontdoor_profile`
- `azurerm_cdn_frontdoor_endpoint`
- `azurerm_cdn_frontdoor_origin_group`
- `azurerm_cdn_frontdoor_origin`
- `azurerm_cdn_frontdoor_route`
- `azurerm_cdn_frontdoor_rule_set`
- `azurerm_cdn_frontdoor_rule`
- `azurerm_cdn_frontdoor_custom_domain`
- `azurerm_cdn_frontdoor_secret`
- `azurerm_cdn_frontdoor_security_policy`
- `azurerm_cdn_frontdoor_firewall_policy`

A minimal profile tree:

```hcl
resource "azurerm_cdn_frontdoor_profile" "example" {
  name                     = "example-profile"
  resource_group_name      = azurerm_resource_group.example.name
  sku_name                 = "Standard_AzureFrontDoor"
  response_timeout_seconds = 60
}

resource "azurerm_cdn_frontdoor_endpoint" "example" {
  name                     = "example-endpoint"
  cdn_frontdoor_profile_id = azurerm_cdn_frontdoor_profile.example.id
}

resource "azurerm_cdn_frontdoor_origin_group" "example" {
  name                     = "example-origins"
  cdn_frontdoor_profile_id = azurerm_cdn_frontdoor_profile.example.id

  load_balancing {
    sample_size                 = 4
    successful_samples_required = 3
  }

  health_probe {
    interval_in_seconds = 30
    path                = "/"
    protocol            = "Http"
    request_type        = "HEAD"
  }
}

resource "azurerm_cdn_frontdoor_origin" "example" {
  name                           = "example-origin"
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
  name                     = "ExampleRuleSet"
  cdn_frontdoor_profile_id = azurerm_cdn_frontdoor_profile.example.id
}

resource "azurerm_cdn_frontdoor_rule" "example" {
  depends_on = [
    azurerm_cdn_frontdoor_origin_group.example,
    azurerm_cdn_frontdoor_origin.example,
  ]

  name                      = "ExampleResponseHeader"
  cdn_frontdoor_rule_set_id = azurerm_cdn_frontdoor_rule_set.example.id
  order                     = 1
  behavior_on_match         = "Continue"

  actions {
    response_header_action {
      header_action = "Append"
      header_name   = "X-Example"
      value         = "miniblue"
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
  name                     = "example-custom-domain"
  cdn_frontdoor_profile_id = azurerm_cdn_frontdoor_profile.example.id
  host_name                = "www.example.com"

  tls {
    certificate_type    = "ManagedCertificate"
    minimum_tls_version = "TLS12"
  }
}

resource "azurerm_cdn_frontdoor_route" "example" {
  name                            = "example-route"
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
}
```

Add a WAF policy and a security policy binding it to the custom domain:

```hcl
resource "azurerm_cdn_frontdoor_firewall_policy" "example" {
  name                       = "examplefdwaf"
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
}

resource "azurerm_cdn_frontdoor_security_policy" "example" {
  name                     = "example-security-policy"
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
```

See [examples/terraform/frontdoor](https://github.com/moabukar/miniblue/tree/main/examples/terraform/frontdoor) for the complete fixture, including a profile, endpoint, origin group, origin, route, rule set, rule, custom domain, security policy and WAF policy.

To run it against miniblue:

```bash
bash scripts/trust-cert.sh   # one-time certificate trust
cd examples/terraform/frontdoor
tofu init                    # or: terraform init
tofu apply -auto-approve
tofu plan -detailed-exitcode # expect exit code 0 (no drift)
tofu destroy -auto-approve
```

## Enabling only what you need

Front Door is a single filterable service named `frontdoor`. It is independent:
enabling it does not enable any other service. A fixture that creates its own
resource group through the API needs `resourcegroups` too:

```bash
SERVICES=resourcegroups,frontdoor ./bin/miniblue
```

## Limitations

- Control plane only. miniblue does not route traffic, terminate TLS, issue certificates or serve the generated `*.z01.azurefd.net` host names.
- DNS validation is never performed. Custom domains stay `Pending` (`validationState` and `domainValidationState`) and the provider does not wait for approval, so applies and destroys succeed.
- WAF policies are metadata only. Custom rules and managed rules are stored and returned, but no request is ever inspected or blocked. `frontendEndpointLinks` always returns an empty list.
- Secrets are metadata only. Certificate material is never stored, retrieved or used for TLS; `CustomerCertificate` secrets are echoed back with a synthetic expiration date.
- There is no traffic routing, origin health probing, caching, compression, URL rewrite/redirect execution or rules-engine evaluation, even though those settings are stored and echoed.
- Front Door actions not implemented: `checkNameAvailability`, `validateCustomDomain`, endpoint `purge` and `preload`, and log/analytics endpoints.
- Generated host names and Front Door IDs are synthetic and intentionally stable across reads; they do not correspond to real Azure resources.
- Only the global location is modelled. `api-version` is accepted but not enforced.
