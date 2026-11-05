package tests

import (
	"testing"
)

func frontDoorWAFBase(tsURL string) string {
	return tsURL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/frontDoorWebApplicationFirewallPolicies"
}

func TestFrontDoorFirewallPolicyLifecycle(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	base := frontDoorWAFBase(ts.URL)
	body := `{
		"location": "Global",
		"sku": {"name": "Standard_AzureFrontDoor"},
		"tags": {"env": "test"},
		"properties": {
			"policySettings": {
				"enabledState": "Enabled",
				"mode": "Prevention",
				"requestBodyCheck": "Enabled"
			},
			"customRules": {
				"rules": [{
					"name": "block-scanner",
					"enabledState": "Enabled",
					"priority": 1,
					"ruleType": "MatchRule",
					"matchConditions": [],
					"action": "Block"
				}]
			}
		}
	}`

	resp := doRequest(t, "PUT", base+"/waf1"+frontDoorAPIVersion, body)
	expectStatus(t, resp, 201)
	policy := decodeJSON(t, resp)
	resp.Body.Close()

	wantID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/FrontDoorWebApplicationFirewallPolicies/waf1"
	if policy["id"] != wantID {
		t.Fatalf("expected id=%s, got %v", wantID, policy["id"])
	}
	if policy["name"] != "waf1" {
		t.Fatalf("expected name=waf1, got %v", policy["name"])
	}
	wantType := "Microsoft.Network/FrontDoorWebApplicationFirewallPolicies"
	if policy["type"] != wantType {
		t.Fatalf("expected type=%s, got %v", wantType, policy["type"])
	}
	if policy["location"] != "Global" {
		t.Fatalf("expected location=Global, got %v", policy["location"])
	}

	sku, ok := policy["sku"].(map[string]interface{})
	if !ok || sku["name"] != "Standard_AzureFrontDoor" {
		t.Fatalf("expected sku.name=Standard_AzureFrontDoor, got %v", policy["sku"])
	}
	tags, ok := policy["tags"].(map[string]interface{})
	if !ok || tags["env"] != "test" {
		t.Fatalf("expected tags.env=test, got %v", policy["tags"])
	}

	props, ok := policy["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %T", policy["properties"])
	}
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState=Succeeded, got %v", props["provisioningState"])
	}
	if _, ok := props["frontendEndpointLinks"].([]interface{}); !ok {
		t.Fatalf("expected frontendEndpointLinks array, got %v", props["frontendEndpointLinks"])
	}

	settings, ok := props["policySettings"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected policySettings object, got %v", props["policySettings"])
	}
	if settings["enabledState"] != "Enabled" || settings["mode"] != "Prevention" ||
		settings["requestBodyCheck"] != "Enabled" {
		t.Fatalf("policySettings not echoed: %v", settings)
	}

	customRules, ok := props["customRules"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected customRules object, got %v", props["customRules"])
	}
	rules, ok := customRules["rules"].([]interface{})
	if !ok || len(rules) != 1 {
		t.Fatalf("expected 1 custom rule, got %v", customRules["rules"])
	}
	rule, _ := rules[0].(map[string]interface{})
	if rule["name"] != "block-scanner" || rule["action"] != "Block" || rule["priority"] != float64(1) {
		t.Fatalf("custom rule not echoed: %v", rule)
	}
	if _, ok := rule["matchConditions"].([]interface{}); !ok {
		t.Fatalf("expected custom rule matchConditions array, got %v", rule["matchConditions"])
	}

	// managedRules is only present when supplied.
	if _, ok := props["managedRules"]; ok {
		t.Fatalf("expected managedRules to be absent for a Standard policy, got %v", props["managedRules"])
	}

	// GET returns the same policy.
	resp = doRequest(t, "GET", base+"/waf1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	policy = decodeJSON(t, resp)
	resp.Body.Close()
	props, _ = policy["properties"].(map[string]interface{})
	settings, _ = props["policySettings"].(map[string]interface{})
	if settings["mode"] != "Prevention" {
		t.Fatalf("policySettings.mode not stable on GET: %v", settings)
	}
	if _, ok := props["customRules"].(map[string]interface{}); !ok {
		t.Fatalf("customRules not stable on GET: %v", props["customRules"])
	}

	// PATCH changes tags and the policy mode, and keeps the custom rules and
	// the defaulted settings.
	resp = doRequest(t, "PATCH", base+"/waf1"+frontDoorAPIVersion,
		`{"tags":{"env":"prod"},"properties":{"policySettings":{"mode":"Detection"}}}`)
	expectStatus(t, resp, 200)
	policy = decodeJSON(t, resp)
	resp.Body.Close()

	tags, _ = policy["tags"].(map[string]interface{})
	if tags["env"] != "prod" {
		t.Fatalf("expected tags.env=prod after PATCH, got %v", policy["tags"])
	}
	props, _ = policy["properties"].(map[string]interface{})
	settings, _ = props["policySettings"].(map[string]interface{})
	if settings["mode"] != "Detection" {
		t.Fatalf("expected mode=Detection after PATCH, got %v", settings["mode"])
	}
	if settings["enabledState"] != "Enabled" || settings["requestBodyCheck"] != "Enabled" {
		t.Fatalf("policySettings defaults lost on PATCH: %v", settings)
	}
	customRules, _ = props["customRules"].(map[string]interface{})
	if rules, ok := customRules["rules"].([]interface{}); !ok || len(rules) != 1 {
		t.Fatalf("customRules lost on PATCH: %v", props["customRules"])
	}
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("provisioningState lost on PATCH: %v", props["provisioningState"])
	}

	// The resource group list returns the single policy.
	resp = doRequest(t, "GET", base+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	list := decodeJSON(t, resp)
	resp.Body.Close()
	if items, ok := list["value"].([]interface{}); !ok || len(items) != 1 {
		t.Fatalf("expected 1 WAF policy, got %v", list["value"])
	}

	// The capitalised path spelling resolves to the same record.
	capitalised := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/FrontDoorWebApplicationFirewallPolicies"
	resp = doRequest(t, "GET", capitalised+"/waf1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	policy = decodeJSON(t, resp)
	resp.Body.Close()
	if policy["id"] != wantID {
		t.Fatalf("expected id=%s via capitalised path, got %v", wantID, policy["id"])
	}

	// A Premium policy round-trips managed rules.
	premiumBody := `{
		"location": "Global",
		"sku": {"name": "Premium_AzureFrontDoor"},
		"properties": {
			"managedRules": {
				"managedRuleSets": [{
					"ruleSetType": "Microsoft_DefaultRuleSet",
					"ruleSetVersion": "2.1",
					"ruleGroupOverrides": []
				}]
			}
		}
	}`
	resp = doRequest(t, "PUT", base+"/waf2"+frontDoorAPIVersion, premiumBody)
	expectStatus(t, resp, 201)
	policy = decodeJSON(t, resp)
	resp.Body.Close()

	props, _ = policy["properties"].(map[string]interface{})
	managed, ok := props["managedRules"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected managedRules object, got %v", props["managedRules"])
	}
	sets, ok := managed["managedRuleSets"].([]interface{})
	if !ok || len(sets) != 1 {
		t.Fatalf("expected 1 managed rule set, got %v", managed["managedRuleSets"])
	}
	set, _ := sets[0].(map[string]interface{})
	if set["ruleSetType"] != "Microsoft_DefaultRuleSet" || set["ruleSetVersion"] != "2.1" {
		t.Fatalf("managed rule set not echoed: %v", set)
	}

	resp = doRequest(t, "GET", base+"/waf2"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	policy = decodeJSON(t, resp)
	resp.Body.Close()
	props, _ = policy["properties"].(map[string]interface{})
	if _, ok := props["managedRules"].(map[string]interface{}); !ok {
		t.Fatalf("managedRules not stable on GET: %v", props["managedRules"])
	}

	// The subscription-level list includes every WAF policy in the subscription.
	subListURL := ts.URL + "/subscriptions/sub1/providers/Microsoft.Network/frontDoorWebApplicationFirewallPolicies"
	resp = doRequest(t, "GET", subListURL+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	list = decodeJSON(t, resp)
	resp.Body.Close()
	if items, ok := list["value"].([]interface{}); !ok || len(items) != 2 {
		t.Fatalf("expected 2 WAF policies in subscription list, got %v", list["value"])
	}

	// DELETE removes the policy and only the policy.
	resp = doRequest(t, "DELETE", base+"/waf1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 204)
	resp.Body.Close()

	resp = doRequest(t, "GET", base+"/waf1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()

	resp = doRequest(t, "GET", base+"/waf2"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	resp.Body.Close()

	resp = doRequest(t, "DELETE", base+"/waf2"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 204)
	resp.Body.Close()

	resp = doRequest(t, "GET", capitalised+"/waf2"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
}
