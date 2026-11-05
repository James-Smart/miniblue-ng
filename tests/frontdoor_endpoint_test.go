package tests

import (
	"encoding/json"
	"regexp"
	"testing"
)

func TestFrontDoorEndpointLifecycle(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	profileBase := frontDoorProfileBase(ts.URL)
	resp := doRequest(t, "PUT", profileBase+"/p1"+frontDoorAPIVersion, `{"location":"global"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	endpoints := profileBase + "/p1/afdEndpoints"
	body := `{
		"location": "global",
		"tags": {"env": "test"},
		"properties": {"enabledState": "Enabled"}
	}`

	resp = doRequest(t, "PUT", endpoints+"/e1"+frontDoorAPIVersion, body)
	expectStatus(t, resp, 201)
	endpoint := decodeJSON(t, resp)
	resp.Body.Close()

	props, ok := endpoint["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %T", endpoint["properties"])
	}
	hostName, _ := props["hostName"].(string)
	hostPattern := regexp.MustCompile(`^e1-[0-9a-f]{6}\.z01\.azurefd\.net$`)
	if !hostPattern.MatchString(hostName) {
		t.Fatalf("expected generated hostName matching %s, got %q", hostPattern, hostName)
	}
	if props["enabledState"] != "Enabled" {
		t.Fatalf("expected enabledState=Enabled, got %v", props["enabledState"])
	}
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState=Succeeded, got %v", props["provisioningState"])
	}
	tags, _ := endpoint["tags"].(map[string]interface{})
	if tags["env"] != "test" {
		t.Fatalf("expected tags.env=test, got %v", endpoint["tags"])
	}
	wantID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cdn/profiles/p1/afdEndpoints/e1"
	if endpoint["id"] != wantID {
		t.Fatalf("expected id=%s, got %v", wantID, endpoint["id"])
	}

	// GET returns the same generated hostname.
	resp = doRequest(t, "GET", endpoints+"/e1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	endpoint = decodeJSON(t, resp)
	resp.Body.Close()
	props, _ = endpoint["properties"].(map[string]interface{})
	if props["hostName"] != hostName {
		t.Fatalf("expected hostName to be stable, got %v", props["hostName"])
	}

	// A full PUT of the GET body with only tags changed keeps the hostname.
	endpoint["tags"] = map[string]interface{}{"env": "prod"}
	putBody, err := json.Marshal(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	resp = doRequest(t, "PUT", endpoints+"/e1"+frontDoorAPIVersion, string(putBody))
	expectStatus(t, resp, 200)
	endpoint = decodeJSON(t, resp)
	resp.Body.Close()
	props, _ = endpoint["properties"].(map[string]interface{})
	if props["hostName"] != hostName {
		t.Fatalf("expected hostName unchanged after PUT, got %v", props["hostName"])
	}
	tags, _ = endpoint["tags"].(map[string]interface{})
	if tags["env"] != "prod" {
		t.Fatalf("expected tags.env=prod after PUT, got %v", endpoint["tags"])
	}

	// PATCH changes enabledState and keeps the hostname.
	resp = doRequest(t, "PATCH", endpoints+"/e1"+frontDoorAPIVersion, `{"properties":{"enabledState":"Disabled"}}`)
	expectStatus(t, resp, 200)
	endpoint = decodeJSON(t, resp)
	resp.Body.Close()
	props, _ = endpoint["properties"].(map[string]interface{})
	if props["enabledState"] != "Disabled" {
		t.Fatalf("expected enabledState=Disabled after PATCH, got %v", props["enabledState"])
	}
	if props["hostName"] != hostName {
		t.Fatalf("expected hostName unchanged after PATCH, got %v", props["hostName"])
	}

	// List returns the single endpoint.
	resp = doRequest(t, "GET", endpoints+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	list := decodeJSON(t, resp)
	resp.Body.Close()
	items, ok := list["value"].([]interface{})
	if !ok {
		t.Fatalf("expected value array, got %T", list["value"])
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(items))
	}

	// DELETE then GET returns 404.
	resp = doRequest(t, "DELETE", endpoints+"/e1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	resp = doRequest(t, "GET", endpoints+"/e1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()

	// PUT under a missing profile returns 404.
	resp = doRequest(t, "PUT", profileBase+"/missing/afdEndpoints/e1"+frontDoorAPIVersion, body)
	expectStatus(t, resp, 404)
	resp.Body.Close()
}
