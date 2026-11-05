package tests

import (
	"testing"
	"time"
)

func hasStatus(states []interface{}, code string) bool {
	for _, s := range states {
		st, ok := s.(map[string]interface{})
		if ok && st["code"] == code {
			return true
		}
	}
	return false
}

func statusTime(states []interface{}, code string) string {
	for _, s := range states {
		st, ok := s.(map[string]interface{})
		if ok && st["code"] == code {
			ts, _ := st["time"].(string)
			return ts
		}
	}
	return ""
}

func TestVMReadIncludesInstanceViewAndRoundTripsProperties(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	body := `{"location":"eastus","properties":{
		"userData":"aGVsbG8=",
		"priority":"Spot",
		"evictionPolicy":"Deallocate",
		"diagnosticsProfile":{"bootDiagnostics":{"enabled":false}},
		"securityProfile":{"encryptionAtHost":true},
		"additionalCapabilities":{"ultraSSDEnabled":true},
		"scheduledEventsProfile":{"terminateNotificationProfile":{"enable":false,"notBeforeTimeout":"PT5M"}}
	}}`
	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "web")+vmAV, body)
	expectStatus(t, resp, 201)
	m := decodeJSON(t, resp)
	resp.Body.Close()

	props := m["properties"].(map[string]interface{})
	vmID, _ := props["vmId"].(string)
	if vmID == "" {
		t.Fatalf("expected non-empty properties.vmId, got %v", props["vmId"])
	}
	iv, ok := props["instanceView"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties.instanceView object, got %T", props["instanceView"])
	}
	statuses, ok := iv["statuses"].([]interface{})
	if !ok {
		t.Fatalf("expected instanceView.statuses array, got %T", iv["statuses"])
	}
	if !hasStatus(statuses, "PowerState/running") {
		t.Fatalf("expected a PowerState/running status, got %v", statuses)
	}
	provisionedAt := statusTime(statuses, "ProvisioningState/succeeded")
	if provisionedAt == "" {
		t.Fatalf("expected a provisioning timestamp, got %v", statuses)
	}
	if props["userData"] != "aGVsbG8=" {
		t.Fatalf("expected userData to round-trip, got %v", props["userData"])
	}
	if props["priority"] != "Spot" {
		t.Fatalf("expected priority Spot, got %v", props["priority"])
	}
	if props["evictionPolicy"] != "Deallocate" {
		t.Fatalf("expected evictionPolicy Deallocate, got %v", props["evictionPolicy"])
	}
	if props["diagnosticsProfile"] == nil {
		t.Fatal("expected diagnosticsProfile to round-trip")
	}
	if props["securityProfile"] == nil {
		t.Fatal("expected securityProfile to round-trip")
	}
	if props["additionalCapabilities"] == nil {
		t.Fatal("expected additionalCapabilities to round-trip")
	}
	if props["scheduledEventsProfile"] == nil {
		t.Fatal("expected scheduledEventsProfile to round-trip")
	}

	// Cross a timestamp boundary so a freshly generated time would be visible.
	time.Sleep(1100 * time.Millisecond)

	// GET returns the same stable vmId and instanceView.
	resp = doRequest(t, "GET", vmURL(ts, "sub1", "rg1", "web")+vmAV, "")
	expectStatus(t, resp, 200)
	got := decodeJSON(t, resp)
	resp.Body.Close()
	gotProps := got["properties"].(map[string]interface{})
	if gotProps["vmId"] != vmID {
		t.Fatalf("expected stable vmId %q, got %v", vmID, gotProps["vmId"])
	}
	gotIV := gotProps["instanceView"].(map[string]interface{})
	gotStatuses := gotIV["statuses"].([]interface{})
	if !hasStatus(gotStatuses, "PowerState/running") {
		t.Fatalf("expected GET instanceView to report PowerState/running, got %v", gotStatuses)
	}
	if got := statusTime(gotStatuses, "ProvisioningState/succeeded"); got != provisionedAt {
		t.Fatalf("expected provisioning timestamp to stay stable at %q, got %q", provisionedAt, got)
	}

	// A second GET must not move the generated timestamp either.
	resp = doRequest(t, "GET", vmURL(ts, "sub1", "rg1", "web")+vmAV, "")
	expectStatus(t, resp, 200)
	again := decodeJSON(t, resp)
	resp.Body.Close()
	againIV := again["properties"].(map[string]interface{})["instanceView"].(map[string]interface{})
	if got := statusTime(againIV["statuses"].([]interface{}), "ProvisioningState/succeeded"); got != provisionedAt {
		t.Fatalf("expected provisioning timestamp to stay stable across GETs at %q, got %q", provisionedAt, got)
	}

	// Dedicated instanceView endpoint returns the object unwrapped.
	resp = doRequest(t, "GET", vmURL(ts, "sub1", "rg1", "web")+"/instanceView"+vmAV, "")
	expectStatus(t, resp, 200)
	ivResp := decodeJSON(t, resp)
	resp.Body.Close()
	ivStatuses, ok := ivResp["statuses"].([]interface{})
	if !ok {
		t.Fatalf("expected top-level statuses array from instanceView, got %T", ivResp["statuses"])
	}
	if !hasStatus(ivStatuses, "PowerState/running") {
		t.Fatalf("expected instanceView endpoint to report PowerState/running, got %v", ivStatuses)
	}
	if got := statusTime(ivStatuses, "ProvisioningState/succeeded"); got != provisionedAt {
		t.Fatalf("expected instanceView endpoint timestamp to match %q, got %q", provisionedAt, got)
	}

	// vmSizes returns a non-empty list of common sizes.
	resp = doRequest(t, "GET", vmURL(ts, "sub1", "rg1", "web")+"/vmSizes"+vmAV, "")
	expectStatus(t, resp, 200)
	sizes := decodeJSON(t, resp)
	resp.Body.Close()
	value, ok := sizes["value"].([]interface{})
	if !ok || len(value) == 0 {
		t.Fatalf("expected non-empty vmSizes value, got %v", sizes["value"])
	}
}

func TestVMUpdateRoundTripsTopLevelFields(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "web")+vmAV, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	update := `{"location":"eastus",
		"tags":{"env":"test"},
		"zones":["1"],
		"plan":{"name":"plan-a","publisher":"acme","product":"widget"},
		"extendedLocation":{"name":"edge-loc","type":"EdgeZone"}}`
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "web")+vmAV, update)
	expectStatus(t, resp, 200)
	m := decodeJSON(t, resp)
	resp.Body.Close()

	checkTopLevel := func(label string, m map[string]interface{}) {
		t.Helper()
		tags, ok := m["tags"].(map[string]interface{})
		if !ok || tags["env"] != "test" {
			t.Fatalf("%s: expected tags.env test, got %v", label, m["tags"])
		}
		zones, ok := m["zones"].([]interface{})
		if !ok || len(zones) != 1 || zones[0] != "1" {
			t.Fatalf("%s: expected zones [1], got %v", label, m["zones"])
		}
		plan, ok := m["plan"].(map[string]interface{})
		if !ok || plan["name"] != "plan-a" {
			t.Fatalf("%s: expected plan.name plan-a, got %v", label, m["plan"])
		}
		ext, ok := m["extendedLocation"].(map[string]interface{})
		if !ok || ext["type"] != "EdgeZone" {
			t.Fatalf("%s: expected extendedLocation.type EdgeZone, got %v", label, m["extendedLocation"])
		}
	}
	checkTopLevel("PUT response", m)

	resp = doRequest(t, "GET", vmURL(ts, "sub1", "rg1", "web")+vmAV, "")
	expectStatus(t, resp, 200)
	got := decodeJSON(t, resp)
	resp.Body.Close()
	checkTopLevel("GET response", got)
}

func TestVMReadOmitsDefaultAdditionalCapabilities(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	body := `{"location":"eastus","properties":{"additionalCapabilities":{"ultraSSDEnabled":false,"hibernationEnabled":false}}}`
	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "defaults")+vmAV, body)
	expectStatus(t, resp, 201)
	m := decodeJSON(t, resp)
	resp.Body.Close()

	props := m["properties"].(map[string]interface{})
	if _, ok := props["additionalCapabilities"]; ok {
		t.Fatalf("expected default additionalCapabilities to be omitted, got %v", props["additionalCapabilities"])
	}
}
