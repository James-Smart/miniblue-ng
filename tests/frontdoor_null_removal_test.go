package tests

import (
	"testing"
)

// TestFrontDoorRouteNullRemoval pins that a PATCH which explicitly nulls a
// route field removes it from the stored record instead of resurrecting the
// previous value during the computed pass.
func TestFrontDoorRouteNullRemoval(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	profileBase := frontDoorProfileBase(ts.URL)
	resp := doRequest(t, "PUT", profileBase+"/p1"+frontDoorAPIVersion, `{"location":"global"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "PUT", profileBase+"/p1/afdEndpoints/e1"+frontDoorAPIVersion, `{"properties":{}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	routes := profileBase + "/p1/afdEndpoints/e1/routes"
	body := `{
		"properties": {
			"originPath": "/v1",
			"customDomains": [{"id": "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cdn/profiles/p1/customDomains/www"}],
			"cacheConfiguration": {"queryStringCachingBehavior": "IgnoreQueryString"}
		}
	}`
	resp = doRequest(t, "PUT", routes+"/r1"+frontDoorAPIVersion, body)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "PATCH", routes+"/r1"+frontDoorAPIVersion,
		`{"properties":{"customDomains":null,"originPath":null,"cacheConfiguration":null}}`)
	expectStatus(t, resp, 200)
	patched := decodeJSON(t, resp)
	resp.Body.Close()
	assertRouteNullsAbsent(t, patched, "PATCH")

	resp = doRequest(t, "GET", routes+"/r1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	fetched := decodeJSON(t, resp)
	resp.Body.Close()
	assertRouteNullsAbsent(t, fetched, "GET")

	props, _ := fetched["properties"].(map[string]interface{})
	if props["endpointName"] != "e1" {
		t.Fatalf("expected endpointName=e1 after null PATCH, got %v", props["endpointName"])
	}
	if props["provisioningState"] != "Succeeded" || props["resourceState"] != "Active" {
		t.Fatalf("expected computed state after null PATCH, got %v", props)
	}
}

func assertRouteNullsAbsent(t *testing.T, route map[string]interface{}, stage string) {
	t.Helper()
	props, _ := route["properties"].(map[string]interface{})
	if props == nil {
		t.Fatalf("%s: expected route properties, got %T", stage, route["properties"])
	}
	for _, key := range []string{"customDomains", "originPath", "cacheConfiguration"} {
		if _, present := props[key]; present {
			t.Fatalf("%s: expected %s to be absent after null, got %v", stage, key, props[key])
		}
	}
}

// TestFrontDoorEndpointTagsNullRemoval pins that tags:null clears the tag map
// while the generated host name is preserved.
func TestFrontDoorEndpointTagsNullRemoval(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	profileBase := frontDoorProfileBase(ts.URL)
	resp := doRequest(t, "PUT", profileBase+"/p1"+frontDoorAPIVersion, `{"location":"global"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	endpoints := profileBase + "/p1/afdEndpoints"
	resp = doRequest(t, "PUT", endpoints+"/e1"+frontDoorAPIVersion, `{"tags":{"env":"test"},"properties":{}}`)
	expectStatus(t, resp, 201)
	endpoint := decodeJSON(t, resp)
	resp.Body.Close()
	props, _ := endpoint["properties"].(map[string]interface{})
	hostName, _ := props["hostName"].(string)
	if hostName == "" {
		t.Fatal("expected generated hostName")
	}

	resp = doRequest(t, "PATCH", endpoints+"/e1"+frontDoorAPIVersion, `{"tags":null}`)
	expectStatus(t, resp, 200)
	endpoint = decodeJSON(t, resp)
	resp.Body.Close()
	assertTagsCleared(t, endpoint, "PATCH")

	resp = doRequest(t, "GET", endpoints+"/e1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	endpoint = decodeJSON(t, resp)
	resp.Body.Close()
	assertTagsCleared(t, endpoint, "GET")
	props, _ = endpoint["properties"].(map[string]interface{})
	if props["hostName"] != hostName {
		t.Fatalf("expected hostName preserved after tags:null, got %v", props["hostName"])
	}
}

func assertTagsCleared(t *testing.T, endpoint map[string]interface{}, stage string) {
	t.Helper()
	tags, ok := endpoint["tags"].(map[string]interface{})
	if !ok {
		t.Fatalf("%s: expected tags map, got %T", stage, endpoint["tags"])
	}
	if len(tags) != 0 {
		t.Fatalf("%s: expected empty tags after null, got %v", stage, tags)
	}
}

// TestFrontDoorSecretParametersNullRemoval pins that parameters:null removes
// the tagged union rather than resurrecting the previous parameters.
func TestFrontDoorSecretParametersNullRemoval(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	profileBase := frontDoorProfileBase(ts.URL)
	resp := doRequest(t, "PUT", profileBase+"/p1"+frontDoorAPIVersion, `{"location":"global"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	secrets := profileBase + "/p1/secrets"
	body := `{
		"properties": {
			"parameters": {
				"type": "CustomerCertificate",
				"secretSource": {"id": "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.KeyVault/vaults/kv/secrets/cert"},
				"useLatestVersion": true
			}
		}
	}`
	resp = doRequest(t, "PUT", secrets+"/s1"+frontDoorAPIVersion, body)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "PATCH", secrets+"/s1"+frontDoorAPIVersion, `{"properties":{"parameters":null}}`)
	expectStatus(t, resp, 200)
	secret := decodeJSON(t, resp)
	resp.Body.Close()
	assertSecretParametersAbsent(t, secret, "PATCH")

	resp = doRequest(t, "GET", secrets+"/s1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	secret = decodeJSON(t, resp)
	resp.Body.Close()
	assertSecretParametersAbsent(t, secret, "GET")

	props, _ := secret["properties"].(map[string]interface{})
	if props["deploymentStatus"] != "NotStarted" {
		t.Fatalf("expected deploymentStatus after parameters:null, got %v", props["deploymentStatus"])
	}
}

func assertSecretParametersAbsent(t *testing.T, secret map[string]interface{}, stage string) {
	t.Helper()
	props, _ := secret["properties"].(map[string]interface{})
	if props == nil {
		t.Fatalf("%s: expected secret properties, got %T", stage, secret["properties"])
	}
	if params, present := props["parameters"]; present {
		t.Fatalf("%s: expected parameters to be absent after null, got %v", stage, params)
	}
}
