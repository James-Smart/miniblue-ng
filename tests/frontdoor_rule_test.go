package tests

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFrontDoorRuleSetAndRule(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	profileBase := frontDoorProfileBase(ts.URL)
	resp := doRequest(t, "PUT", profileBase+"/p1"+frontDoorAPIVersion, `{"location":"global"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	ruleSets := profileBase + "/p1/ruleSets"

	// Create the rule set.
	resp = doRequest(t, "PUT", ruleSets+"/rs1"+frontDoorAPIVersion, `{"properties":{}}`)
	expectStatus(t, resp, 201)
	ruleSet := decodeJSON(t, resp)
	resp.Body.Close()

	wantRuleSetID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cdn/profiles/p1/ruleSets/rs1"
	if ruleSet["id"] != wantRuleSetID {
		t.Fatalf("expected rule set id=%s, got %v", wantRuleSetID, ruleSet["id"])
	}
	if ruleSet["type"] != "Microsoft.Cdn/profiles/ruleSets" {
		t.Fatalf("expected rule set type, got %v", ruleSet["type"])
	}
	ruleSetProps, ok := ruleSet["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected rule set properties object, got %T", ruleSet["properties"])
	}
	if ruleSetProps["provisioningState"] != "Succeeded" {
		t.Fatalf("expected rule set provisioningState=Succeeded, got %v", ruleSetProps["provisioningState"])
	}
	if ruleSetProps["resourceState"] != "Active" {
		t.Fatalf("expected rule set resourceState=Active, got %v", ruleSetProps["resourceState"])
	}

	// Create the rule with explicit order, match behavior, conditions and
	// actions. Conditions and actions are opaque and must echo exactly.
	ruleBody := `{
		"properties": {
			"order": 1,
			"matchProcessingBehavior": "Continue",
			"conditions": [{
				"name": "RequestUri",
				"parameters": {
					"operator": "Contains",
					"matchValues": ["/api"],
					"transforms": []
				}
			}],
			"actions": [{
				"name": "RouteConfigurationOverride",
				"parameters": {}
			}]
		}
	}`
	rules := ruleSets + "/rs1/rules"
	resp = doRequest(t, "PUT", rules+"/r1"+frontDoorAPIVersion, ruleBody)
	expectStatus(t, resp, 201)
	rule := decodeJSON(t, resp)
	resp.Body.Close()

	var wantBody struct {
		Properties struct {
			Conditions []interface{} `json:"conditions"`
			Actions    []interface{} `json:"actions"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(ruleBody), &wantBody); err != nil {
		t.Fatal(err)
	}

	wantRuleID := wantRuleSetID + "/rules/r1"
	if rule["id"] != wantRuleID {
		t.Fatalf("expected rule id=%s, got %v", wantRuleID, rule["id"])
	}
	if rule["type"] != "Microsoft.Cdn/profiles/ruleSets/rules" {
		t.Fatalf("expected rule type, got %v", rule["type"])
	}
	ruleProps, ok := rule["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected rule properties object, got %T", rule["properties"])
	}
	if ruleProps["order"] != float64(1) {
		t.Fatalf("expected order=1, got %v", ruleProps["order"])
	}
	if ruleProps["matchProcessingBehavior"] != "Continue" {
		t.Fatalf("expected matchProcessingBehavior=Continue, got %v", ruleProps["matchProcessingBehavior"])
	}
	if !reflect.DeepEqual(ruleProps["conditions"], wantBody.Properties.Conditions) {
		t.Fatalf("conditions not echoed: %v", ruleProps["conditions"])
	}
	if !reflect.DeepEqual(ruleProps["actions"], wantBody.Properties.Actions) {
		t.Fatalf("actions not echoed: %v", ruleProps["actions"])
	}
	if ruleProps["provisioningState"] != "Succeeded" {
		t.Fatalf("expected rule provisioningState=Succeeded, got %v", ruleProps["provisioningState"])
	}
	if ruleProps["resourceState"] != "Active" {
		t.Fatalf("expected rule resourceState=Active, got %v", ruleProps["resourceState"])
	}

	// GET returns the same rule.
	resp = doRequest(t, "GET", rules+"/r1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	rule = decodeJSON(t, resp)
	resp.Body.Close()
	ruleProps, _ = rule["properties"].(map[string]interface{})
	if ruleProps["order"] != float64(1) ||
		ruleProps["matchProcessingBehavior"] != "Continue" {
		t.Fatalf("rule fields not stable on GET: %v", ruleProps)
	}
	if !reflect.DeepEqual(ruleProps["conditions"], wantBody.Properties.Conditions) {
		t.Fatalf("conditions not stable on GET: %v", ruleProps["conditions"])
	}
	if !reflect.DeepEqual(ruleProps["actions"], wantBody.Properties.Actions) {
		t.Fatalf("actions not stable on GET: %v", ruleProps["actions"])
	}

	// List returns the single rule.
	resp = doRequest(t, "GET", rules+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	list := decodeJSON(t, resp)
	resp.Body.Close()
	items, ok := list["value"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 rule, got %v", list["value"])
	}

	// PATCH changes only the order and keeps the conditions.
	resp = doRequest(t, "PATCH", rules+"/r1"+frontDoorAPIVersion, `{"properties":{"order":2}}`)
	expectStatus(t, resp, 200)
	rule = decodeJSON(t, resp)
	resp.Body.Close()
	ruleProps, _ = rule["properties"].(map[string]interface{})
	if ruleProps["order"] != float64(2) {
		t.Fatalf("expected order=2 after PATCH, got %v", ruleProps["order"])
	}
	if !reflect.DeepEqual(ruleProps["conditions"], wantBody.Properties.Conditions) {
		t.Fatalf("conditions lost on PATCH: %v", ruleProps["conditions"])
	}

	// Omitted fields get their defaults.
	resp = doRequest(t, "PUT", rules+"/r2"+frontDoorAPIVersion, `{"properties":{}}`)
	expectStatus(t, resp, 201)
	defaultRule := decodeJSON(t, resp)
	resp.Body.Close()
	defaultProps, _ := defaultRule["properties"].(map[string]interface{})
	if defaultProps["matchProcessingBehavior"] != "Continue" {
		t.Fatalf("expected default matchProcessingBehavior=Continue, got %v", defaultProps["matchProcessingBehavior"])
	}
	if conditions, ok := defaultProps["conditions"].([]interface{}); !ok || len(conditions) != 0 {
		t.Fatalf("expected default conditions=[], got %v", defaultProps["conditions"])
	}
	if actions, ok := defaultProps["actions"].([]interface{}); !ok || len(actions) != 0 {
		t.Fatalf("expected default actions=[], got %v", defaultProps["actions"])
	}
	if defaultProps["deploymentStatus"] != "NotStarted" {
		t.Fatalf("expected default deploymentStatus=NotStarted, got %v", defaultProps["deploymentStatus"])
	}

	// DELETE the rule set cascades to its rules.
	resp = doRequest(t, "DELETE", ruleSets+"/rs1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 204)
	resp.Body.Close()

	resp = doRequest(t, "GET", rules+"/r1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()

	// PUT a rule under a missing rule set returns 404.
	resp = doRequest(t, "PUT", ruleSets+"/missing/rules/r1"+frontDoorAPIVersion, ruleBody)
	expectStatus(t, resp, 404)
	resp.Body.Close()
}
