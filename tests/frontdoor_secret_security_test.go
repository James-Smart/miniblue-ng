package tests

import (
	"testing"
)

func TestFrontDoorSecretAndSecurityPolicy(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	profileBase := frontDoorProfileBase(ts.URL)
	resp := doRequest(t, "PUT", profileBase+"/p1"+frontDoorAPIVersion, `{"location":"global"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	secrets := profileBase + "/p1/secrets"
	secretBody := `{
		"properties": {
			"parameters": {
				"type": "CustomerCertificate",
				"secretSource": {"id": "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.KeyVault/vaults/kv/secrets/cert"},
				"useLatestVersion": true
			}
		}
	}`

	resp = doRequest(t, "PUT", secrets+"/s1"+frontDoorAPIVersion, secretBody)
	expectStatus(t, resp, 201)
	secret := decodeJSON(t, resp)
	resp.Body.Close()

	wantSecretID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cdn/profiles/p1/secrets/s1"
	if secret["id"] != wantSecretID {
		t.Fatalf("expected secret id=%s, got %v", wantSecretID, secret["id"])
	}
	if secret["type"] != "Microsoft.Cdn/profiles/secrets" {
		t.Fatalf("expected secret type, got %v", secret["type"])
	}

	secretProps, _ := secret["properties"].(map[string]interface{})
	params, _ := secretProps["parameters"].(map[string]interface{})
	if params["type"] != "CustomerCertificate" {
		t.Fatalf("expected parameters.type=CustomerCertificate, got %v", params["type"])
	}
	source, _ := params["secretSource"].(map[string]interface{})
	wantSourceID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.KeyVault/vaults/kv/secrets/cert"
	if source["id"] != wantSourceID {
		t.Fatalf("expected secretSource.id=%s, got %v", wantSourceID, source["id"])
	}
	if params["useLatestVersion"] != true {
		t.Fatalf("expected useLatestVersion=true, got %v", params["useLatestVersion"])
	}
	expiration, _ := params["expirationDate"].(string)
	if expiration == "" {
		t.Fatalf("expected non-empty parameters.expirationDate")
	}
	if sans, ok := params["subjectAlternativeNames"].([]interface{}); !ok {
		t.Fatalf("expected subjectAlternativeNames array, got %v", params["subjectAlternativeNames"])
	} else if len(sans) != 0 {
		t.Fatalf("expected empty subjectAlternativeNames, got %v", sans)
	}
	if secretProps["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState=Succeeded, got %v", secretProps["provisioningState"])
	}
	if secretProps["resourceState"] != "Active" {
		t.Fatalf("expected resourceState=Active, got %v", secretProps["resourceState"])
	}

	// GET returns the same secret, with the generated values unchanged.
	resp = doRequest(t, "GET", secrets+"/s1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	secret = decodeJSON(t, resp)
	resp.Body.Close()
	secretProps, _ = secret["properties"].(map[string]interface{})
	params, _ = secretProps["parameters"].(map[string]interface{})
	if params["type"] != "CustomerCertificate" || params["useLatestVersion"] != true {
		t.Fatalf("secret parameters not stable on GET: %v", params)
	}
	if params["expirationDate"] != expiration {
		t.Fatalf("expected expirationDate stable on GET, got %v", params["expirationDate"])
	}
	source, _ = params["secretSource"].(map[string]interface{})
	if source["id"] != wantSourceID {
		t.Fatalf("secretSource not stable on GET: %v", params["secretSource"])
	}

	securityPolicies := profileBase + "/p1/securityPolicies"
	policyBody := `{
		"properties": {
			"parameters": {
				"type": "WebApplicationFirewall",
				"wafPolicy": {"id": "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/frontDoorWebApplicationFirewallPolicies/waf1"},
				"associations": [
					{
						"domains": [{"id": "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cdn/profiles/p1/customDomains/www"}],
						"patternsToMatch": ["/*"]
					}
				]
			}
		}
	}`

	resp = doRequest(t, "PUT", securityPolicies+"/sp1"+frontDoorAPIVersion, policyBody)
	expectStatus(t, resp, 201)
	policy := decodeJSON(t, resp)
	resp.Body.Close()

	wantPolicyID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cdn/profiles/p1/securityPolicies/sp1"
	if policy["id"] != wantPolicyID {
		t.Fatalf("expected security policy id=%s, got %v", wantPolicyID, policy["id"])
	}
	if policy["type"] != "Microsoft.Cdn/profiles/securityPolicies" {
		t.Fatalf("expected security policy type, got %v", policy["type"])
	}

	policyProps, _ := policy["properties"].(map[string]interface{})
	params, _ = policyProps["parameters"].(map[string]interface{})
	if params["type"] != "WebApplicationFirewall" {
		t.Fatalf("expected parameters.type=WebApplicationFirewall, got %v", params["type"])
	}
	wafPolicy, _ := params["wafPolicy"].(map[string]interface{})
	wantWAFID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/frontDoorWebApplicationFirewallPolicies/waf1"
	if wafPolicy["id"] != wantWAFID {
		t.Fatalf("expected wafPolicy.id=%s, got %v", wantWAFID, wafPolicy["id"])
	}
	associations, ok := params["associations"].([]interface{})
	if !ok || len(associations) != 1 {
		t.Fatalf("expected 1 association, got %v", params["associations"])
	}
	association, _ := associations[0].(map[string]interface{})
	domains, _ := association["domains"].([]interface{})
	if len(domains) != 1 {
		t.Fatalf("expected 1 association domain, got %v", association["domains"])
	}
	domain, _ := domains[0].(map[string]interface{})
	if domain["isActive"] != true {
		t.Fatalf("expected association domain isActive=true, got %v", domain["isActive"])
	}
	if patterns, ok := association["patternsToMatch"].([]interface{}); !ok || len(patterns) != 1 || patterns[0] != "/*" {
		t.Fatalf("expected patternsToMatch=[/*], got %v", association["patternsToMatch"])
	}

	// GET returns the same security policy.
	resp = doRequest(t, "GET", securityPolicies+"/sp1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	policy = decodeJSON(t, resp)
	resp.Body.Close()
	policyProps, _ = policy["properties"].(map[string]interface{})
	params, _ = policyProps["parameters"].(map[string]interface{})
	associations, _ = params["associations"].([]interface{})
	association, _ = associations[0].(map[string]interface{})
	domains, _ = association["domains"].([]interface{})
	domain, _ = domains[0].(map[string]interface{})
	if domain["isActive"] != true {
		t.Fatalf("expected isActive=true stable on GET, got %v", domain["isActive"])
	}
	if patterns, ok := association["patternsToMatch"].([]interface{}); !ok || len(patterns) != 1 || patterns[0] != "/*" {
		t.Fatalf("patternsToMatch not stable on GET: %v", association["patternsToMatch"])
	}

	// Both resources list.
	resp = doRequest(t, "GET", secrets+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	list := decodeJSON(t, resp)
	resp.Body.Close()
	if items, ok := list["value"].([]interface{}); !ok || len(items) != 1 {
		t.Fatalf("expected 1 secret, got %v", list["value"])
	}

	resp = doRequest(t, "GET", securityPolicies+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	list = decodeJSON(t, resp)
	resp.Body.Close()
	if items, ok := list["value"].([]interface{}); !ok || len(items) != 1 {
		t.Fatalf("expected 1 security policy, got %v", list["value"])
	}

	// PATCH changes the patterns and keeps the WAF policy and isActive.
	patchBody := `{
		"properties": {
			"parameters": {
				"associations": [
					{
						"domains": [{"id": "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cdn/profiles/p1/customDomains/www"}],
						"patternsToMatch": ["/api/*", "/static/*"]
					}
				]
			}
		}
	}`
	resp = doRequest(t, "PATCH", securityPolicies+"/sp1"+frontDoorAPIVersion, patchBody)
	expectStatus(t, resp, 200)
	policy = decodeJSON(t, resp)
	resp.Body.Close()
	policyProps, _ = policy["properties"].(map[string]interface{})
	params, _ = policyProps["parameters"].(map[string]interface{})
	if params["type"] != "WebApplicationFirewall" {
		t.Fatalf("type lost on PATCH: %v", params["type"])
	}
	wafPolicy, _ = params["wafPolicy"].(map[string]interface{})
	if wafPolicy["id"] != wantWAFID {
		t.Fatalf("wafPolicy lost on PATCH: %v", params["wafPolicy"])
	}
	associations, _ = params["associations"].([]interface{})
	association, _ = associations[0].(map[string]interface{})
	if patterns, ok := association["patternsToMatch"].([]interface{}); !ok || len(patterns) != 2 ||
		patterns[0] != "/api/*" || patterns[1] != "/static/*" {
		t.Fatalf("patterns not updated on PATCH: %v", association["patternsToMatch"])
	}
	domains, _ = association["domains"].([]interface{})
	domain, _ = domains[0].(map[string]interface{})
	if domain["isActive"] != true {
		t.Fatalf("expected isActive=true after PATCH, got %v", domain["isActive"])
	}

	// DELETE both.
	resp = doRequest(t, "DELETE", secrets+"/s1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	resp = doRequest(t, "DELETE", securityPolicies+"/sp1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	resp = doRequest(t, "GET", secrets+"/s1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()

	resp = doRequest(t, "GET", securityPolicies+"/sp1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
}
