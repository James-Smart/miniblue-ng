package tests

import "testing"

// TestResourceGroupCamelCaseRoutes exercises every resource group operation
// through the camel-case /resourceGroups segment used by azurerm 5.x.
func TestResourceGroupCamelCaseRoutes(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	base := ts.URL + "/subscriptions/sub1/resourceGroups"
	av := "?api-version=2023-07-01"
	rgURL := base + "/myRG" + av

	// Create
	resp := doRequest(t, "PUT", rgURL, `{"location":"eastus","tags":{"env":"dev"}}`)
	expectStatus(t, resp, 201)
	created := decodeJSON(t, resp)
	resp.Body.Close()
	if created["name"] != "myRG" {
		t.Fatalf("expected name=myRG, got %v", created["name"])
	}
	if created["id"] != "/subscriptions/sub1/resourceGroups/myRG" {
		t.Fatalf("unexpected id %v", created["id"])
	}

	// Get
	resp = doRequest(t, "GET", rgURL, "")
	expectStatus(t, resp, 200)
	got := decodeJSON(t, resp)
	resp.Body.Close()
	if got["name"] != "myRG" {
		t.Fatalf("expected name=myRG, got %v", got["name"])
	}
	tags, _ := got["tags"].(map[string]interface{})
	if tags["env"] != "dev" {
		t.Fatalf("expected tag env=dev, got %v", got["tags"])
	}

	// PATCH tags
	resp = doRequest(t, "PATCH", rgURL, `{"tags":{"env":"prod"}}`)
	expectStatus(t, resp, 200)
	patched := decodeJSON(t, resp)
	resp.Body.Close()
	tags, _ = patched["tags"].(map[string]interface{})
	if tags["env"] != "prod" {
		t.Fatalf("expected tag env=prod, got %v", patched["tags"])
	}

	// HEAD
	resp = doRequest(t, "HEAD", rgURL, "")
	resp.Body.Close()
	expectStatus(t, resp, 204)

	// List resource groups
	resp = doRequest(t, "GET", base+av, "")
	expectStatus(t, resp, 200)
	list := decodeJSON(t, resp)
	resp.Body.Close()
	items, ok := list["value"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 resource group in list, got %v", list["value"])
	}
	first, _ := items[0].(map[string]interface{})
	if first["name"] != "myRG" {
		t.Fatalf("expected listed name=myRG, got %v", first["name"])
	}

	// List resources within the resource group
	resp = doRequest(t, "GET", base+"/myRG/resources"+av, "")
	expectStatus(t, resp, 200)
	resources := decodeJSON(t, resp)
	resp.Body.Close()
	items, ok = resources["value"].([]interface{})
	if !ok || len(items) != 0 {
		t.Fatalf("expected empty resource list, got %v", resources["value"])
	}

	// Delete
	resp = doRequest(t, "DELETE", rgURL, "")
	resp.Body.Close()
	expectStatus(t, resp, 202)

	// Confirm the group is gone through the same camel-case route
	resp = doRequest(t, "GET", rgURL, "")
	resp.Body.Close()
	expectStatus(t, resp, 404)
}
