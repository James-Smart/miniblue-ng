package tests

import (
	"testing"
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
		"additionalCapabilities":{"ultraSSDEnabled":false},
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
	if !hasStatus(gotIV["statuses"].([]interface{}), "PowerState/running") {
		t.Fatalf("expected GET instanceView to report PowerState/running, got %v", gotIV["statuses"])
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
