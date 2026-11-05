# Front Door Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Azure Front Door Standard/Premium control-plane emulation under `Microsoft.Cdn/profiles`, plus the `Microsoft.Network/FrontDoorWebApplicationFirewallPolicies` resource, so the `azurerm` Front Door resources apply, refresh and destroy cleanly.

**Architecture:** One new `internal/services/frontdoor` package. Profiles get explicit CRUD; the nested tree is served by one generic `resourceSpec`/`nested` handler that builds IDs from the parent record, stores records under `fd:<sub>:<rg>:<kind>:...`, and applies per-resource computed fields. The Network-provider WAF policy lives in the same package but uses its own routes and response shape. Generated hostnames, validation tokens and Front Door IDs are persisted so they never drift.

**Tech Stack:** Go 1.26, `github.com/go-chi/chi/v5`, the existing `store.Store`, `github.com/google/uuid`, standard-library hex/rand.

**Spec:** `docs/superpowers/specs/2026-11-05-vm-gaps-frontdoor-design.md`

## Global Constraints

- Go 1.26, existing `chi` routing and `store.Store` patterns. No new dependency.
- `PUT` and `PATCH` both merge over existing records and preserve generated/read-only fields unless explicitly replaced.
- Generated hostnames, validation tokens, `frontDoorId` and resource IDs must be stable across GET/PUT/PATCH.
- Store keys are ordered `fd:<sub>:<rg>:<kind>:...` so resource-group cleanup is one prefix.
- Responses include the ARM envelope and `properties.provisioningState: "Succeeded"`, `resourceState: "Active"`, `deploymentStatus: "NotStarted"` where applicable.
- API status conventions: `PUT` 201 create / 200 update, `PATCH` 200, `DELETE` 202, list `{"value":[...]}`.
- Register both `frontDoorWebApplicationFirewallPolicies` and `FrontDoorWebApplicationFirewallPolicies` path spellings; responses use `Microsoft.Network/FrontDoorWebApplicationFirewallPolicies`.
- Verification command for every task: `go test ./tests -run 'TestFrontDoor' -v` (plus `go test ./...` and `go vet ./...` at the end).
- `SERVICES=frontdoor` is independent; no other service is implicitly enabled.

## Review Focus

- **Client-side custom-domain association:** a route's `properties.customDomains` is the source of truth, and deleting a custom domain must remove it from every route. Pinned by `TestFrontDoorCustomDomainAndAssociation` in Task 6.
- **Resource-group cascade:** `fd:<sub>:<rg>:` must remove every Front Door kind for that RG and nothing from another RG. Pinned by `TestFrontDoorCascadeAndResourceGroupCleanup` in Task 9.
- **Generated value stability:** endpoint hostnames, validation tokens and profile `frontDoorId` must not change on PATCH or full-model PUT. Pinned by Tasks 2 and 6.
- **Provider read-modify-write on nested resources:** GET followed by a full PUT must preserve computed and unmodelled fields. Pinned by `TestFrontDoorRouteLifecycle` in Task 5 and the update assertions in Tasks 3–5.
- **WAF response completeness:** `policySettings`, `customRules`, `managedRules` and `frontendEndpointLinks` must all be present in the shape the provider expects, with mandatory fields never omitted. Pinned by `TestFrontDoorFirewallPolicyLifecycle` in Task 8.

---

### Task 1: Shared helpers and profile CRUD

**Files:**
- Create: `internal/services/frontdoor/handler.go`
- Create: `internal/services/frontdoor/store.go`
- Create: `internal/services/frontdoor/computed.go`
- Modify: `internal/server/server.go` (register `frontdoor`)
- Test: `tests/frontdoor_profile_test.go`

**Interfaces:**
- Consumes: `store.Store`, `azerr`, `uuid`.
- Produces:
  - `type Handler struct { store *store.Store }`
  - `func NewHandler(s *store.Store) *Handler`
  - `func (h *Handler) Register(r chi.Router)`
  - `func profileKey(sub, rg, name string) string`
  - `func rgPrefix(sub, rg string) string`
  - `func mergeMaps(dst, patch map[string]interface{}) map[string]interface{}`
  - `func profileResponse(sub, rg, name string, input, existing map[string]interface{}) map[string]interface{}`
  - Profile routes from spec §10.

- [ ] **Step 1: Write the failing test**

Create `tests/frontdoor_profile_test.go` with `TestFrontDoorProfileLifecycle`.

```go
// PUT .../Microsoft.Cdn/profiles/p1
// {"location":"global","sku":{"name":"Standard_AzureFrontDoor"},
//  "tags":{"env":"test"},"properties":{"originResponseTimeoutSeconds":120}}
// -> 201; sku.name, tags and timeout echoed.
// properties.frontDoorId is a non-empty UUID; provisioningState Succeeded;
// resourceState Active.
// GET -> same frontDoorId and timeout.
// PATCH {"tags":{"env":"prod"}} -> 200; GET -> tags updated, id unchanged.
// GET list -> one profile.
// DELETE -> 202; GET -> 404.
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tests -run 'TestFrontDoorProfileLifecycle' -v`
Expected: FAIL with 404.

- [ ] **Step 3: Implement helpers and profile CRUD**

Create `store.go` with `profileKey`, `rgPrefix`, `mergeMaps`, `resourceID(parentID, segment, name)`, and `nestedKey`. Create `computed.go` with `profileResponse` per spec §12: preserve an existing `frontDoorId` and timeout, generate a UUID when absent, echo `sku`, `identity`, `tags`, `properties.logScrubbing`. Implement `Handler.Register` with the profile routes and register the service in `server.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./tests -run 'TestFrontDoorProfileLifecycle' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/frontdoor internal/server/server.go tests/frontdoor_profile_test.go
git commit -m "feat(frontdoor): add profile CRUD and shared helpers"
```

---

### Task 2: AFD endpoints and the generic nested handler

**Files:**
- Create: `internal/services/frontdoor/nested.go`
- Modify: `internal/services/frontdoor/handler.go` (register endpoint routes)
- Modify: `internal/services/frontdoor/computed.go` (`endpointResponse`)
- Test: `tests/frontdoor_endpoint_test.go`

**Interfaces:**
- Consumes: profile helpers from Task 1.
- Produces:
  - `type resourceSpec struct { kind, keyKind, segment, armType string; parents []string; computed func(parent, input, existing map[string]interface{}) map[string]interface{}; children []string }`
  - `type nested struct { store *store.Store; spec resourceSpec }`
  - `func (n *nested) create(w,r)`, `get`, `patch`, `delete`, `list`
  - `func endpointResponse(parent, input, existing map[string]interface{}) map[string]interface{}`
  - `GET/PUT/PATCH/DELETE` under `.../profiles/{profile}/afdEndpoints`.

- [ ] **Step 1: Write the failing test**

Create `tests/frontdoor_endpoint_test.go` with `TestFrontDoorEndpointLifecycle`.

```go
// create profile; PUT .../profiles/p1/afdEndpoints/e1
// {"location":"global","tags":{"env":"test"},"properties":{"enabledState":"Enabled"}}
// -> 201.
// properties.hostName matches ^e1-[0-9a-f]{6}\.z01\.azurefd\.net$
// properties.enabledState == "Enabled"; provisioningState Succeeded.
// GET -> same hostName.
// PUT the GET response with only tags changed -> 200; hostName unchanged.
// PATCH {"properties":{"enabledState":"Disabled"}} -> 200; enabledState Disabled,
//   hostName unchanged.
// GET list -> one endpoint.
// DELETE -> 202; GET -> 404.
// PUT under a missing profile -> 404.
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tests -run 'TestFrontDoorEndpointLifecycle' -v`
Expected: FAIL with 404.

- [ ] **Step 3: Implement the generic handler and endpoints**

Implement `nested` per spec §11: fetch parent by key, 404 if missing; ID = `<parent.id>/<segment>/<name>`; key = `fd:<sub>:<rg>:<keyKind>:<profile>:<name>`; PUT merges over existing and returns 201/200; GET/list apply `computed`; DELETE removes the record and child kinds; PATCH merges. Implement `endpointResponse` with a persisted hostname (`<lower name>-<6 hex>.z01.azurefd.net`) and `enabledState` default `Enabled`. Register the endpoint routes in `Register`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./tests -run 'TestFrontDoorEndpointLifecycle' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/frontdoor/nested.go internal/services/frontdoor/handler.go internal/services/frontdoor/computed.go tests/frontdoor_endpoint_test.go
git commit -m "feat(frontdoor): add endpoints and nested resource handler"
```

---

### Task 3: Origin groups and origins

**Files:**
- Modify: `internal/services/frontdoor/handler.go` (register origin routes)
- Modify: `internal/services/frontdoor/computed.go` (`originGroupResponse`, `originResponse`)
- Test: `tests/frontdoor_origin_test.go`

**Interfaces:**
- Consumes: `nested` and helpers from Task 2.
- Produces:
  - `func originGroupResponse(parent, input, existing map[string]interface{}) map[string]interface{}`
  - `func originResponse(parent, input, existing map[string]interface{}) map[string]interface{}`
  - `.../profiles/{profile}/originGroups/{group}` and `.../origins/{origin}` routes.

- [ ] **Step 1: Write the failing test**

Create `tests/frontdoor_origin_test.go` with `TestFrontDoorOriginGroupAndOrigin`.

```go
// create profile.
// PUT origin group with healthProbe {interval 240, path "/health", protocol Https,
// requestType GET}, loadBalancing {additionalLatency 0, sampleSize 16,
// successfulSamplesRequired 3}, sessionAffinityEnabled true,
// restoreTraffic... 10.
// -> 201; all fields echoed; provisioningState Succeeded.
// PUT origin with hostName "backend.example.com", originHostHeader
// "backend.example.com", httpPort 80, httpsPort 443, priority 1, weight 500,
// enabled true, certificateNameCheckEnabled true.
// -> 201; fields echoed; defaults for omitted values.
// GET/list each -> 200.
// PUT the origin GET body with only weight changed -> 200; ID and parent link stable.
// DELETE origin group -> 202; GET origin -> 404 (cascade).
// PUT origin under a missing group -> 404.
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tests -run 'TestFrontDoorOriginGroupAndOrigin' -v`
Expected: FAIL with 404.

- [ ] **Step 3: Implement origin responses and routes**

Add `originGroupResponse` and `originResponse` per spec §12 (defaults for health probe, load balancing, session affinity, restore time, ports, priority, weight, enabled state, certificate check). Register the nested routes with parent chains `[]string{"profileName"}` and `[]string{"profileName", "originGroupName"}`. Set the origin's `originGroupName` from the parent group name. Give the origin group `children: []string{"origin"}` so deleting it cascades origins.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./tests -run 'TestFrontDoorOriginGroupAndOrigin' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/frontdoor/handler.go internal/services/frontdoor/computed.go tests/frontdoor_origin_test.go
git commit -m "feat(frontdoor): add origin groups and origins"
```

---

### Task 4: Rule sets and rules

**Files:**
- Modify: `internal/services/frontdoor/handler.go` (register rule routes)
- Modify: `internal/services/frontdoor/computed.go` (`ruleSetResponse`, `ruleResponse`)
- Test: `tests/frontdoor_rule_test.go`

**Interfaces:**
- Consumes: `nested` from Task 2.
- Produces:
  - `func ruleSetResponse(parent, input, existing map[string]interface{}) map[string]interface{}`
  - `func ruleResponse(parent, input, existing map[string]interface{}) map[string]interface{}`
  - Routes under `.../profiles/{profile}/ruleSets/{ruleSet}/rules`.

- [ ] **Step 1: Write the failing test**

Create `tests/frontdoor_rule_test.go` with `TestFrontDoorRuleSetAndRule`.

```go
// create profile; PUT rule set -> 201, provisioningState Succeeded.
// PUT rule:
// {"properties":{"order":1,"matchProcessingBehavior":"Continue",
//  "conditions":[{"name":"RequestUri","parameters":{"operator":"Contains",
//    "matchValues":["/api"],"transforms":[]}}],
//  "actions":[{"name":"RouteConfigurationOverride","parameters":{}}]}}
// -> 201; order, conditions and actions round-trip exactly.
// GET -> same; list -> one.
// PATCH {"properties":{"order":2}} -> 200; order changed, conditions kept.
// DELETE rule set -> 202; GET rule -> 404.
// PUT rule under a missing rule set -> 404.
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tests -run 'TestFrontDoorRuleSetAndRule' -v`
Expected: FAIL with 404.

- [ ] **Step 3: Implement rule responses and routes**

Add `ruleSetResponse` (common fields only) and `ruleResponse` per spec §12. Default `matchProcessingBehavior` to `Continue`, `conditions`/`actions` to empty arrays, and echo them without validating their shape. Register nested routes with parent chains `[]string{"profileName"}` and `[]string{"profileName", "ruleSetName"}`. Give the rule set `children: []string{"rule"}`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./tests -run 'TestFrontDoorRuleSetAndRule' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/frontdoor/handler.go internal/services/frontdoor/computed.go tests/frontdoor_rule_test.go
git commit -m "feat(frontdoor): add rule sets and rules"
```

---

### Task 5: Routes

**Files:**
- Modify: `internal/services/frontdoor/handler.go` (register route routes)
- Modify: `internal/services/frontdoor/computed.go` (`routeResponse`)
- Test: `tests/frontdoor_route_test.go`

**Interfaces:**
- Consumes: `nested` from Task 2.
- Produces:
  - `func routeResponse(parent, input, existing map[string]interface{}) map[string]interface{}`
  - Routes under `.../profiles/{profile}/afdEndpoints/{endpoint}/routes`.

- [ ] **Step 1: Write the failing test**

Create `tests/frontdoor_route_test.go` with `TestFrontDoorRouteLifecycle`.

```go
// create profile, endpoint, origin group.
// PUT route:
// {"properties":{
//   "originGroup":{"id": originGroupID},
//   "supportedProtocols":["Http","Https"],
//   "patternsToMatch":["/*"],
//   "forwardingProtocol":"HttpsOnly",
//   "linkToDefaultDomain":"Enabled",
//   "httpsRedirect":"Enabled",
//   "enabledState":"Enabled",
//   "customDomains":[{"id":"/subscriptions/.../customDomains/www"}],
//   "ruleSets":[{"id": ruleSetID}],
//   "cacheConfiguration":{"queryStringCachingBehavior":"IgnoreQueryString",
//     "compressionSettings":{"isCompressionEnabled":true,
//       "contentTypesToCompress":["text/html"]}}}}
// -> 201; all fields echoed; endpointName == "e1";
//   deploymentStatus present.
// GET -> same, customDomains order preserved.
// PUT the GET body with only patternsToMatch changed -> 200; customDomains,
//   ruleSets, originGroup and generated fields preserved.
// PATCH {"properties":{"enabledState":"Disabled"}} -> 200.
// GET list -> one route.
// DELETE -> 202; GET -> 404.
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tests -run 'TestFrontDoorRouteLifecycle' -v`
Expected: FAIL with 404.

- [ ] **Step 3: Implement route response and routes**

Add `routeResponse` per spec §12 with defaults for protocols, patterns, forwarding, link-to-default, HTTPS redirect, enabled state, custom domains and rule sets. Set `endpointName` from the parent. Register nested routes with parent chain `[]string{"profileName", "endpointName"}`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./tests -run 'TestFrontDoorRouteLifecycle' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/frontdoor/handler.go internal/services/frontdoor/computed.go tests/frontdoor_route_test.go
git commit -m "feat(frontdoor): add routes"
```

---

### Task 6: Custom domains and association cleanup

**Files:**
- Modify: `internal/services/frontdoor/handler.go` (register custom-domain routes)
- Modify: `internal/services/frontdoor/computed.go` (`customDomainResponse`)
- Modify: `internal/services/frontdoor/store.go` (route cleanup helper)
- Test: `tests/frontdoor_customdomain_test.go`

**Interfaces:**
- Consumes: `nested`, `routeResponse`, store helpers.
- Produces:
  - `func customDomainResponse(parent, input, existing map[string]interface{}) map[string]interface{}`
  - `func removeCustomDomainFromRoutes(s *store.Store, sub, rg, profile, domainID string)`
  - Routes under `.../profiles/{profile}/customDomains`.

- [ ] **Step 1: Write the failing test**

Create `tests/frontdoor_customdomain_test.go` with `TestFrontDoorCustomDomainAndAssociation`.

```go
// create profile, endpoint, origin group, route with no custom domains.
// PUT custom domain:
// {"properties":{"hostName":"www.example.com",
//   "azureDnsZone":{"id":"/subscriptions/.../dnsZones/example.com"},
//   "tlsSettings":{"certificateType":"ManagedCertificate",
//     "minimumTlsVersion":"TLS12"}}}
// -> 201.
// validationProperties.validationToken matches ^[0-9a-f]{32}$
// validationProperties.validationState == "Pending"
// domainValidationState == "Pending"; expirationDate non-empty.
// GET -> same token (no regeneration).
// PUT the GET body with only tags changed -> token unchanged.
// PUT route with customDomains [{id: domainID}] -> 201; GET route shows it.
// DELETE custom domain -> 202; GET route -> customDomains is empty.
// GET custom domain -> 404.
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tests -run 'TestFrontDoorCustomDomainAndAssociation' -v`
Expected: FAIL with 404.

- [ ] **Step 3: Implement custom domains and route cleanup**

Add `customDomainResponse` per spec §12: persist `validationProperties` token/expiration and states; generate only when absent. Register the nested routes. On delete, scan `fd:<sub>:<rg>:route:<profile>:` records, remove any `properties.customDomains` entry whose `id` equals the deleted domain ID, and store the updated route. Do not call a dedicated association API; the provider's association resource is client-side.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./tests -run 'TestFrontDoorCustomDomainAndAssociation' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/frontdoor/handler.go internal/services/frontdoor/computed.go internal/services/frontdoor/store.go tests/frontdoor_customdomain_test.go
git commit -m "feat(frontdoor): add custom domains and association cleanup"
```

---

### Task 7: Secrets and security policies

**Files:**
- Modify: `internal/services/frontdoor/handler.go` (register secret/security-policy routes)
- Modify: `internal/services/frontdoor/computed.go` (`secretResponse`, `securityPolicyResponse`)
- Test: `tests/frontdoor_secret_security_test.go`

**Interfaces:**
- Consumes: `nested` from Task 2.
- Produces:
  - `func secretResponse(parent, input, existing map[string]interface{}) map[string]interface{}`
  - `func securityPolicyResponse(parent, input, existing map[string]interface{}) map[string]interface{}`
  - Routes under `.../profiles/{profile}/secrets` and `.../securityPolicies`.

- [ ] **Step 1: Write the failing test**

Create `tests/frontdoor_secret_security_test.go` with `TestFrontDoorSecretAndSecurityPolicy`.

```go
// create profile.
// PUT secret:
// {"properties":{"parameters":{"type":"CustomerCertificate",
//   "secretSource":{"id":"/subscriptions/.../vaults/kv/secrets/cert"},
//   "useLatestVersion":true}}}
// -> 201; parameters.type, secretSource.id and useLatestVersion round-trip;
//   parameters.expirationDate non-empty; subjectAlternativeNames is an array.
// GET -> same.
// PUT security policy:
// {"properties":{"parameters":{"type":"WebApplicationFirewall",
//   "wafPolicy":{"id":"/subscriptions/.../frontDoorWebApplicationFirewallPolicies/waf1"},
//   "associations":[{"domains":[{"id":"/subscriptions/.../customDomains/www"}],
//     "patternsToMatch":["/*"]}]}}}
// -> 201; parameters round-trip; associations[0].domains[0].isActive == true.
// GET -> same; list works for both.
// PATCH security policy {"properties":{"parameters":{...patterns changed...}}} -> 200.
// DELETE both -> 202.
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tests -run 'TestFrontDoorSecretAndSecurityPolicy' -v`
Expected: FAIL with 404.

- [ ] **Step 3: Implement secret and security-policy responses**

Add `secretResponse` per spec §12: echo `parameters`, add `expirationDate` and `subjectAlternativeNames` for `CustomerCertificate` when absent. Add `securityPolicyResponse`: echo `parameters`, set `isActive: true` on every association domain when absent, preserve patterns. Register both nested routes.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./tests -run 'TestFrontDoorSecretAndSecurityPolicy' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/frontdoor/handler.go internal/services/frontdoor/computed.go tests/frontdoor_secret_security_test.go
git commit -m "feat(frontdoor): add secrets and security policies"
```

---

### Task 8: Front Door WAF policy

**Files:**
- Modify: `internal/services/frontdoor/handler.go` (register WAF routes)
- Create: `internal/services/frontdoor/waf.go`
- Modify: `internal/services/frontdoor/computed.go` (`wafPolicyResponse`)
- Test: `tests/frontdoor_waf_test.go`

**Interfaces:**
- Consumes: `store.Store`, `azerr`, `uuid`.
- Produces:
  - `func wafKey(sub, rg, name string) string`
  - `func wafPolicyResponse(sub, rg, name string, input, existing map[string]interface{}) map[string]interface{}`
  - Routes under `.../Microsoft.Network/frontDoorWebApplicationFirewallPolicies` and the capitalised spelling.

- [ ] **Step 1: Write the failing test**

Create `tests/frontdoor_waf_test.go` with `TestFrontDoorFirewallPolicyLifecycle`.

```go
// PUT .../Microsoft.Network/frontDoorWebApplicationFirewallPolicies/waf1
// {"location":"Global","sku":{"name":"Standard_AzureFrontDoor"},
//  "tags":{"env":"test"},
//  "properties":{
//    "policySettings":{"enabledState":"Enabled","mode":"Prevention",
//      "requestBodyCheck":"Enabled"},
//    "customRules":{"rules":[{"name":"block-scanner","enabledState":"Enabled",
//      "priority":1,"ruleType":"MatchRule","matchConditions":[],
//      "action":"Block"}]}}}
// -> 201; sku, tags, policySettings, customRules echoed;
//   properties.frontendEndpointLinks is an array;
//   properties.provisioningState == "Succeeded"; location "Global".
// GET -> same.
// PATCH {"tags":{"env":"prod"},"properties":{"policySettings":{"mode":"Detection"}}}
// -> 200; mode Detection, customRules kept.
// GET list -> one policy.
// DELETE -> 202; GET -> 404.
// Register the capitalised path: GET with
// .../FrontDoorWebApplicationFirewallPolicies/waf1 -> 200.
// Premium SKU with managedRules round-trips.
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tests -run 'TestFrontDoorFirewallPolicyLifecycle' -v`
Expected: FAIL with 404.

- [ ] **Step 3: Implement the WAF policy**

Add `waf.go` with CRUD. Build the response per spec §12: `location` `Global`, `sku.name`, `tags`, `properties.policySettings` merged with defaults, `customRules` default `{"rules":[]}`, `managedRules` only when supplied, `frontendEndpointLinks: []`, `provisioningState: "Succeeded"`. Register both path spellings. `PATCH` merges tags and properties; preserve generated fields.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./tests -run 'TestFrontDoorFirewallPolicyLifecycle' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/frontdoor/waf.go internal/services/frontdoor/handler.go internal/services/frontdoor/computed.go tests/frontdoor_waf_test.go
git commit -m "feat(frontdoor): add WAF policies"
```

---

### Task 9: Cascade and resource-group cleanup

**Files:**
- Modify: `internal/services/frontdoor/handler.go` (profile delete cascade)
- Modify: `internal/services/frontdoor/store.go` (cascade helper)
- Modify: `internal/services/resourcegroups/handler.go` (add `fd:` prefix)
- Test: `tests/frontdoor_cascade_test.go`

**Interfaces:**
- Consumes: all Front Door store keys.
- Produces:
  - `func deleteProfileChildren(s *store.Store, sub, rg, profile string)`
  - RG cleanup for `fd:<sub>:<rg>:`.

- [ ] **Step 1: Write the failing test**

Create `tests/frontdoor_cascade_test.go` with `TestFrontDoorCascadeAndResourceGroupCleanup`.

```go
// create profile p1 with endpoint, origin group, origin, rule set, rule, route,
// custom domain, secret and security policy; create WAF waf1 in the same RG.
// DELETE profile p1 -> 202.
// GET every child -> 404. GET profile -> 404. GET waf1 -> 200.
// create a second RG with profile p2.
// DELETE the first RG -> 202.
// GET p2 -> 200 (other RG untouched); GET waf1 -> 404.
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tests -run 'TestFrontDoorCascadeAndResourceGroupCleanup' -v`
Expected: FAIL because child records remain.

- [ ] **Step 3: Implement cascade**

`deleteProfileChildren` removes every `fd:<sub>:<rg>:<kind>:<profile>` prefix for endpoint, route, origin group, origin, rule set, rule, custom domain, secret and security policy. Profile delete calls it before removing the profile. Add `h.store.DeleteByPrefix("fd:" + p)` in `resourcegroups.Delete`, where `p` is `<sub>:<rg>:`. Do not use a bare `fd:` prefix, which would cross subscriptions.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./tests -run 'TestFrontDoorCascadeAndResourceGroupCleanup' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/services/frontdoor internal/services/resourcegroups/handler.go tests/frontdoor_cascade_test.go
git commit -m "feat(frontdoor): cascade profile and resource group deletion"
```

---

### Task 10: Documentation, examples and final verification

**Files:**
- Create: `website/docs/services/front-door.md`
- Modify: `website/docs/services/parity.md`
- Modify: `website/mkdocs.yml`
- Modify: `README.md`
- Modify: `ROADMAP.md`
- Create: `examples/terraform/frontdoor/main.tf`, `provider.tf`, `versions.tf`

**Interfaces:**
- Consumes: all previous tasks.
- Produces: user-facing docs and a generic Front Door fixture.

- [ ] **Step 1: Write the docs**

Document the supported resource tree, API paths, an example Tofu/Terraform config, computed values (hostnames, validation token/state, Front Door ID), and limitations (no routing/TLS/DNS validation/WAF enforcement; secrets are metadata only; validation stays pending). Add the page to `mkdocs.yml`, update the parity matrix and README service list/count, and remove Front Door from `ROADMAP.md`'s "Next" table.

- [ ] **Step 2: Add the Front Door fixture**

Create a generic config with a profile, endpoint, origin group, origin, route, rule set, rule, custom domain, security policy and WAF policy. Use the standard miniblue provider block. Do not include anyone's private hostnames or DNS zones.

- [ ] **Step 3: Run the full verification**

```bash
gofmt -w internal tests
go test ./...
go vet ./...
```

Expected: all tests pass, no vet errors. If `tofu` or `terraform` is available, run `init`, `apply`, `plan -detailed-exitcode` and `destroy` against the fixture and note the result; otherwise the HTTP suite is the gate.

- [ ] **Step 4: Commit**

```bash
git add website/docs/services/front-door.md website/docs/services/parity.md website/mkdocs.yml README.md ROADMAP.md examples/terraform/frontdoor
git commit -m "docs: document Front Door support and add a Terraform fixture"
```

---

## Self-review notes

- Spec §10 (package/routes) → Tasks 1–8.
- Spec §11 (generic handler, keys, cascade) → Task 2 (handler), Task 9 (cascade).
- Spec §12 (computed properties) → Tasks 1–8, one response builder per resource.
- Spec §13 (tests) → tests live with each task; cascade in Task 9.
- Spec §14 (server registration) → Task 1, with health count covered by Task 9's test.
- Spec §15–16 (docs/acceptance) → Task 10.
- Review Focus items are each pinned by a named test: Task 6 (association), Task 9 (RG prefix), Tasks 2/6 (stability), Task 5 (read-modify-write), Task 8 (WAF shape).
