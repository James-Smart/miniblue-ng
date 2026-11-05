package tests

import (
	"testing"

	"github.com/google/uuid"
)

const frontDoorAPIVersion = "?api-version=2024-02-01"

func frontDoorProfileBase(tsURL string) string {
	return tsURL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cdn/profiles"
}

func TestFrontDoorProfileLifecycle(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	base := frontDoorProfileBase(ts.URL)
	body := `{
		"location": "global",
		"sku": {"name": "Standard_AzureFrontDoor"},
		"tags": {"env": "test"},
		"properties": {"originResponseTimeoutSeconds": 120}
	}`

	resp := doRequest(t, "PUT", base+"/p1"+frontDoorAPIVersion, body)
	expectStatus(t, resp, 201)
	profile := decodeJSON(t, resp)
	resp.Body.Close()

	wantID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cdn/profiles/p1"
	if profile["id"] != wantID {
		t.Fatalf("expected id=%s, got %v", wantID, profile["id"])
	}
	if profile["name"] != "p1" {
		t.Fatalf("expected name=p1, got %v", profile["name"])
	}
	if profile["type"] != "Microsoft.Cdn/profiles" {
		t.Fatalf("expected type=Microsoft.Cdn/profiles, got %v", profile["type"])
	}
	if profile["location"] != "global" {
		t.Fatalf("expected location=global, got %v", profile["location"])
	}

	sku, ok := profile["sku"].(map[string]interface{})
	if !ok || sku["name"] != "Standard_AzureFrontDoor" {
		t.Fatalf("expected sku.name=Standard_AzureFrontDoor, got %v", profile["sku"])
	}
	tags, ok := profile["tags"].(map[string]interface{})
	if !ok || tags["env"] != "test" {
		t.Fatalf("expected tags.env=test, got %v", profile["tags"])
	}

	props, ok := profile["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %T", profile["properties"])
	}
	if props["originResponseTimeoutSeconds"] != float64(120) {
		t.Fatalf("expected originResponseTimeoutSeconds=120, got %v", props["originResponseTimeoutSeconds"])
	}
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState=Succeeded, got %v", props["provisioningState"])
	}
	if props["resourceState"] != "Active" {
		t.Fatalf("expected resourceState=Active, got %v", props["resourceState"])
	}
	frontDoorID, _ := props["frontDoorId"].(string)
	if _, err := uuid.Parse(frontDoorID); err != nil || frontDoorID == "" {
		t.Fatalf("expected non-empty UUID frontDoorId, got %v", props["frontDoorId"])
	}

	// GET returns the same generated ID and timeout.
	resp = doRequest(t, "GET", base+"/p1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	profile = decodeJSON(t, resp)
	resp.Body.Close()
	props, _ = profile["properties"].(map[string]interface{})
	if props["frontDoorId"] != frontDoorID {
		t.Fatalf("expected frontDoorId to be stable, got %v", props["frontDoorId"])
	}
	if props["originResponseTimeoutSeconds"] != float64(120) {
		t.Fatalf("expected originResponseTimeoutSeconds=120 after GET, got %v", props["originResponseTimeoutSeconds"])
	}

	// PATCH updates tags and leaves the generated ID unchanged.
	resp = doRequest(t, "PATCH", base+"/p1"+frontDoorAPIVersion, `{"tags":{"env":"prod"}}`)
	expectStatus(t, resp, 200)
	profile = decodeJSON(t, resp)
	resp.Body.Close()
	tags, _ = profile["tags"].(map[string]interface{})
	if tags["env"] != "prod" {
		t.Fatalf("expected tags.env=prod after PATCH, got %v", profile["tags"])
	}

	resp = doRequest(t, "GET", base+"/p1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	profile = decodeJSON(t, resp)
	resp.Body.Close()
	props, _ = profile["properties"].(map[string]interface{})
	if props["frontDoorId"] != frontDoorID {
		t.Fatalf("expected frontDoorId to survive PATCH, got %v", props["frontDoorId"])
	}
	tags, _ = profile["tags"].(map[string]interface{})
	if tags["env"] != "prod" {
		t.Fatalf("expected tags.env=prod after PATCH, got %v", profile["tags"])
	}

	// List returns the single profile.
	resp = doRequest(t, "GET", base+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	list := decodeJSON(t, resp)
	resp.Body.Close()
	items, ok := list["value"].([]interface{})
	if !ok {
		t.Fatalf("expected value array, got %T", list["value"])
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(items))
	}

	// DELETE removes the profile.
	resp = doRequest(t, "DELETE", base+"/p1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	resp = doRequest(t, "GET", base+"/p1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
}
