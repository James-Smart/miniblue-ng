package tests

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestFrontDoorRouteLifecycle(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	profileBase := frontDoorProfileBase(ts.URL)
	resp := doRequest(t, "PUT", profileBase+"/p1"+frontDoorAPIVersion, `{"location":"global"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	// Create the parent endpoint, origin group and rule set.
	resp = doRequest(t, "PUT", profileBase+"/p1/afdEndpoints/e1"+frontDoorAPIVersion, `{"properties":{}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "PUT", profileBase+"/p1/originGroups/g1"+frontDoorAPIVersion, `{"properties":{}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "PUT", profileBase+"/p1/ruleSets/rs1"+frontDoorAPIVersion, `{"properties":{}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	originGroupID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cdn/profiles/p1/originGroups/g1"
	ruleSetID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cdn/profiles/p1/ruleSets/rs1"
	domainA := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cdn/profiles/p1/customDomains/www"
	domainB := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cdn/profiles/p1/customDomains/api"

	routeBody := fmt.Sprintf(`{
		"properties": {
			"originGroup": {"id": %q},
			"supportedProtocols": ["Http", "Https"],
			"patternsToMatch": ["/*"],
			"forwardingProtocol": "HttpsOnly",
			"linkToDefaultDomain": "Enabled",
			"httpsRedirect": "Enabled",
			"enabledState": "Enabled",
			"customDomains": [{"id": %q}, {"id": %q}],
			"ruleSets": [{"id": %q}],
			"cacheConfiguration": {
				"queryStringCachingBehavior": "IgnoreQueryString",
				"compressionSettings": {
					"isCompressionEnabled": true,
					"contentTypesToCompress": ["text/html"]
				}
			}
		}
	}`, originGroupID, domainA, domainB, ruleSetID)

	wantCustomDomains := []interface{}{
		map[string]interface{}{"id": domainA},
		map[string]interface{}{"id": domainB},
	}
	wantRuleSets := []interface{}{
		map[string]interface{}{"id": ruleSetID},
	}
	wantOriginGroup := map[string]interface{}{"id": originGroupID}
	wantCacheConfiguration := map[string]interface{}{
		"queryStringCachingBehavior": "IgnoreQueryString",
		"compressionSettings": map[string]interface{}{
			"isCompressionEnabled":   true,
			"contentTypesToCompress": []interface{}{"text/html"},
		},
	}

	routes := profileBase + "/p1/afdEndpoints/e1/routes"
	resp = doRequest(t, "PUT", routes+"/r1"+frontDoorAPIVersion, routeBody)
	expectStatus(t, resp, 201)
	route := decodeJSON(t, resp)
	resp.Body.Close()

	wantRouteID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cdn/profiles/p1/afdEndpoints/e1/routes/r1"
	if route["id"] != wantRouteID {
		t.Fatalf("expected route id=%s, got %v", wantRouteID, route["id"])
	}
	if route["type"] != "Microsoft.Cdn/profiles/afdEndpoints/routes" {
		t.Fatalf("expected route type, got %v", route["type"])
	}
	props, ok := route["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected route properties object, got %T", route["properties"])
	}

	if props["endpointName"] != "e1" {
		t.Fatalf("expected endpointName=e1, got %v", props["endpointName"])
	}
	if props["deploymentStatus"] == nil {
		t.Fatalf("expected deploymentStatus to be present")
	}
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState=Succeeded, got %v", props["provisioningState"])
	}
	if props["resourceState"] != "Active" {
		t.Fatalf("expected resourceState=Active, got %v", props["resourceState"])
	}
	if !reflect.DeepEqual(props["originGroup"], wantOriginGroup) {
		t.Fatalf("originGroup not echoed: %v", props["originGroup"])
	}
	if !reflect.DeepEqual(props["supportedProtocols"], []interface{}{"Http", "Https"}) {
		t.Fatalf("supportedProtocols not echoed: %v", props["supportedProtocols"])
	}
	if !reflect.DeepEqual(props["patternsToMatch"], []interface{}{"/*"}) {
		t.Fatalf("patternsToMatch not echoed: %v", props["patternsToMatch"])
	}
	if props["forwardingProtocol"] != "HttpsOnly" {
		t.Fatalf("expected forwardingProtocol=HttpsOnly, got %v", props["forwardingProtocol"])
	}
	if props["linkToDefaultDomain"] != "Enabled" {
		t.Fatalf("expected linkToDefaultDomain=Enabled, got %v", props["linkToDefaultDomain"])
	}
	if props["httpsRedirect"] != "Enabled" {
		t.Fatalf("expected httpsRedirect=Enabled, got %v", props["httpsRedirect"])
	}
	if props["enabledState"] != "Enabled" {
		t.Fatalf("expected enabledState=Enabled, got %v", props["enabledState"])
	}
	if !reflect.DeepEqual(props["customDomains"], wantCustomDomains) {
		t.Fatalf("customDomains not echoed in order: %v", props["customDomains"])
	}
	if !reflect.DeepEqual(props["ruleSets"], wantRuleSets) {
		t.Fatalf("ruleSets not echoed: %v", props["ruleSets"])
	}
	if !reflect.DeepEqual(props["cacheConfiguration"], wantCacheConfiguration) {
		t.Fatalf("cacheConfiguration not echoed: %v", props["cacheConfiguration"])
	}

	// GET returns the same route with customDomains order preserved.
	resp = doRequest(t, "GET", routes+"/r1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	route = decodeJSON(t, resp)
	resp.Body.Close()
	props, _ = route["properties"].(map[string]interface{})
	if !reflect.DeepEqual(props["customDomains"], wantCustomDomains) {
		t.Fatalf("customDomains order not preserved on GET: %v", props["customDomains"])
	}
	if !reflect.DeepEqual(props["ruleSets"], wantRuleSets) ||
		!reflect.DeepEqual(props["originGroup"], wantOriginGroup) {
		t.Fatalf("associations not stable on GET: %v", props)
	}

	// PUT the full GET body back with only patternsToMatch changed. Association
	// arrays and generated fields must survive the round trip.
	newPatterns := []interface{}{"/api/*"}
	props["patternsToMatch"] = newPatterns
	fullBody, err := json.Marshal(route)
	if err != nil {
		t.Fatal(err)
	}
	resp = doRequest(t, "PUT", routes+"/r1"+frontDoorAPIVersion, string(fullBody))
	expectStatus(t, resp, 200)
	route = decodeJSON(t, resp)
	resp.Body.Close()
	props, _ = route["properties"].(map[string]interface{})
	if !reflect.DeepEqual(props["patternsToMatch"], newPatterns) {
		t.Fatalf("patternsToMatch not updated: %v", props["patternsToMatch"])
	}
	if !reflect.DeepEqual(props["customDomains"], wantCustomDomains) {
		t.Fatalf("customDomains lost on full PUT: %v", props["customDomains"])
	}
	if !reflect.DeepEqual(props["ruleSets"], wantRuleSets) {
		t.Fatalf("ruleSets lost on full PUT: %v", props["ruleSets"])
	}
	if !reflect.DeepEqual(props["originGroup"], wantOriginGroup) {
		t.Fatalf("originGroup lost on full PUT: %v", props["originGroup"])
	}
	if props["endpointName"] != "e1" {
		t.Fatalf("endpointName lost on full PUT: %v", props["endpointName"])
	}
	if props["deploymentStatus"] == nil {
		t.Fatalf("deploymentStatus lost on full PUT")
	}
	if props["provisioningState"] != "Succeeded" || props["resourceState"] != "Active" {
		t.Fatalf("generated state lost on full PUT: %v", props)
	}

	// PATCH changes only the enabled state.
	resp = doRequest(t, "PATCH", routes+"/r1"+frontDoorAPIVersion, `{"properties":{"enabledState":"Disabled"}}`)
	expectStatus(t, resp, 200)
	route = decodeJSON(t, resp)
	resp.Body.Close()
	props, _ = route["properties"].(map[string]interface{})
	if props["enabledState"] != "Disabled" {
		t.Fatalf("expected enabledState=Disabled after PATCH, got %v", props["enabledState"])
	}
	if !reflect.DeepEqual(props["customDomains"], wantCustomDomains) {
		t.Fatalf("customDomains lost on PATCH: %v", props["customDomains"])
	}
	if !reflect.DeepEqual(props["patternsToMatch"], newPatterns) {
		t.Fatalf("patternsToMatch lost on PATCH: %v", props["patternsToMatch"])
	}

	// List returns the single route.
	resp = doRequest(t, "GET", routes+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	list := decodeJSON(t, resp)
	resp.Body.Close()
	items, ok := list["value"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 route, got %v", list["value"])
	}

	// Omitted fields get their defaults.
	resp = doRequest(t, "PUT", routes+"/r2"+frontDoorAPIVersion, `{"properties":{}}`)
	expectStatus(t, resp, 201)
	defaultRoute := decodeJSON(t, resp)
	resp.Body.Close()
	defaultProps, _ := defaultRoute["properties"].(map[string]interface{})
	if !reflect.DeepEqual(defaultProps["supportedProtocols"], []interface{}{"Https"}) {
		t.Fatalf("expected default supportedProtocols=[Https], got %v", defaultProps["supportedProtocols"])
	}
	if !reflect.DeepEqual(defaultProps["patternsToMatch"], []interface{}{"/*"}) {
		t.Fatalf("expected default patternsToMatch=[/*], got %v", defaultProps["patternsToMatch"])
	}
	if defaultProps["forwardingProtocol"] != "MatchRequest" {
		t.Fatalf("expected default forwardingProtocol=MatchRequest, got %v", defaultProps["forwardingProtocol"])
	}
	if defaultProps["linkToDefaultDomain"] != "Enabled" {
		t.Fatalf("expected default linkToDefaultDomain=Enabled, got %v", defaultProps["linkToDefaultDomain"])
	}
	if defaultProps["httpsRedirect"] != "Enabled" {
		t.Fatalf("expected default httpsRedirect=Enabled, got %v", defaultProps["httpsRedirect"])
	}
	if defaultProps["enabledState"] != "Enabled" {
		t.Fatalf("expected default enabledState=Enabled, got %v", defaultProps["enabledState"])
	}
	if domains, ok := defaultProps["customDomains"].([]interface{}); !ok || len(domains) != 0 {
		t.Fatalf("expected default customDomains=[], got %v", defaultProps["customDomains"])
	}
	if ruleSets, ok := defaultProps["ruleSets"].([]interface{}); !ok || len(ruleSets) != 0 {
		t.Fatalf("expected default ruleSets=[], got %v", defaultProps["ruleSets"])
	}
	if defaultProps["deploymentStatus"] != "NotStarted" {
		t.Fatalf("expected default deploymentStatus=NotStarted, got %v", defaultProps["deploymentStatus"])
	}

	resp = doRequest(t, "DELETE", routes+"/r2"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	// DELETE removes the route.
	resp = doRequest(t, "DELETE", routes+"/r1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	resp = doRequest(t, "GET", routes+"/r1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()

	// A route under a missing endpoint returns 404.
	resp = doRequest(t, "PUT", profileBase+"/p1/afdEndpoints/missing/routes/r1"+frontDoorAPIVersion, routeBody)
	expectStatus(t, resp, 404)
	resp.Body.Close()
}
