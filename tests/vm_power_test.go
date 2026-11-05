package tests

import (
	"net/http/httptest"
	"testing"
)

// vmPowerStateFromResponse returns properties.miniblue.powerState from a VM
// response body.
func vmPowerStateFromResponse(t *testing.T, m map[string]interface{}) string {
	t.Helper()
	props, ok := m["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %T", m["properties"])
	}
	mb, ok := props["miniblue"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties.miniblue object, got %T", props["miniblue"])
	}
	state, _ := mb["powerState"].(string)
	return state
}

// assertVMPowerState checks that both GET and the dedicated instanceView
// endpoint report the expected VM power state.
func assertVMPowerState(t *testing.T, ts *httptest.Server, sub, rg, name, want string) {
	t.Helper()
	powerCode := "PowerState/" + want

	resp := doRequest(t, "GET", vmURL(ts, sub, rg, name)+vmAV, "")
	expectStatus(t, resp, 200)
	m := decodeJSON(t, resp)
	resp.Body.Close()

	if got := vmPowerStateFromResponse(t, m); got != want {
		t.Fatalf("GET powerState: expected %q, got %q", want, got)
	}
	props := m["properties"].(map[string]interface{})
	iv, ok := props["instanceView"].(map[string]interface{})
	if !ok {
		t.Fatalf("GET: expected properties.instanceView object, got %T", props["instanceView"])
	}
	statuses, _ := iv["statuses"].([]interface{})
	if !hasStatus(statuses, powerCode) {
		t.Fatalf("GET instanceView: expected %s status, got %v", powerCode, statuses)
	}

	resp = doRequest(t, "GET", vmURL(ts, sub, rg, name)+"/instanceView"+vmAV, "")
	expectStatus(t, resp, 200)
	ivResp := decodeJSON(t, resp)
	resp.Body.Close()
	statuses, _ = ivResp["statuses"].([]interface{})
	if !hasStatus(statuses, powerCode) {
		t.Fatalf("instanceView endpoint: expected %s status, got %v", powerCode, statuses)
	}
}

// postVMPowerAction posts a power action and asserts it succeeds with 200.
func postVMPowerAction(t *testing.T, ts *httptest.Server, sub, rg, name, action string) map[string]interface{} {
	t.Helper()
	resp := doRequest(t, "POST", vmURL(ts, sub, rg, name)+"/"+action+vmAV, "")
	expectStatus(t, resp, 200)
	m := decodeJSON(t, resp)
	resp.Body.Close()
	return m
}

// TestVMPowerActionsWithoutDocker verifies that start, powerOff, restart and
// deallocate are metadata-only without Docker: they update the recorded power
// state, are reflected in the instance view, and do not return 409. Runtime
// operations that genuinely need Docker (runCommand) keep the 409.
func TestVMPowerActionsWithoutDocker(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "web")+vmAV, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	assertVMPowerState(t, ts, "sub1", "rg1", "web", "running")

	m := postVMPowerAction(t, ts, "sub1", "rg1", "web", "deallocate")
	got := vmPowerStateFromResponse(t, m)
	if got != "stopped" && got != "deallocated" {
		t.Fatalf("deallocate: expected powerState stopped or deallocated, got %q", got)
	}
	assertVMPowerState(t, ts, "sub1", "rg1", "web", got)

	m = postVMPowerAction(t, ts, "sub1", "rg1", "web", "start")
	if got := vmPowerStateFromResponse(t, m); got != "running" {
		t.Fatalf("start: expected powerState running, got %q", got)
	}
	assertVMPowerState(t, ts, "sub1", "rg1", "web", "running")

	m = postVMPowerAction(t, ts, "sub1", "rg1", "web", "powerOff")
	if got := vmPowerStateFromResponse(t, m); got != "stopped" {
		t.Fatalf("powerOff: expected powerState stopped, got %q", got)
	}
	assertVMPowerState(t, ts, "sub1", "rg1", "web", "stopped")

	m = postVMPowerAction(t, ts, "sub1", "rg1", "web", "restart")
	if got := vmPowerStateFromResponse(t, m); got != "running" {
		t.Fatalf("restart: expected powerState running, got %q", got)
	}
	assertVMPowerState(t, ts, "sub1", "rg1", "web", "running")

	// runCommand still requires Docker.
	resp = doRequest(t, "POST", vmURL(ts, "sub1", "rg1", "web")+"/runCommand"+vmAV,
		`{"commandId":"RunShellScript","script":["echo hi"]}`)
	expectStatus(t, resp, 409)
	e := decodeError(t, resp)
	resp.Body.Close()
	if e.Error.Code != "DockerUnavailable" {
		t.Fatalf("runCommand: expected DockerUnavailable, got %s", e.Error.Code)
	}
}
