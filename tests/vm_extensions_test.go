package tests

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func extensionURL(ts *httptest.Server, sub, rg, vm, name string) string {
	u := vmURL(ts, sub, rg, vm) + "/extensions"
	if name != "" {
		u += "/" + name
	}
	return u + vmAV
}

func TestVMExtensionLifecycle(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "web")+vmAV, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	putBody := `{"location":"eastus","properties":{
		"publisher":"Microsoft.Azure.Extensions","type":"CustomScript",
		"typeHandlerVersion":"2.0","autoUpgradeMinorVersion":true,
		"settings":{"commandToExecute":"echo hi"},
		"protectedSettings":{"storageAccountKey":"secret"}}}`
	resp = doRequest(t, "PUT", extensionURL(ts, "sub1", "rg1", "web", "ext"), putBody)
	expectStatus(t, resp, 201)
	raw, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("reading extension response: %v", err)
	}
	if strings.Contains(string(raw), "secret") {
		t.Fatalf("protected settings leaked into the PUT response: %s", raw)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decoding extension response: %v", err)
	}

	if m["name"] != "ext" || m["type"] != "Microsoft.Compute/virtualMachines/extensions" {
		t.Fatalf("unexpected extension identity: %v / %v", m["name"], m["type"])
	}
	wantID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/virtualMachines/web/extensions/ext"
	if m["id"] != wantID {
		t.Fatalf("expected id %s, got %v", wantID, m["id"])
	}
	if m["location"] != "eastus" {
		t.Fatalf("expected location eastus, got %v", m["location"])
	}
	props := m["properties"].(map[string]interface{})
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState Succeeded, got %v", props["provisioningState"])
	}
	if props["publisher"] != "Microsoft.Azure.Extensions" || props["type"] != "CustomScript" {
		t.Fatalf("unexpected publisher/type: %v / %v", props["publisher"], props["type"])
	}
	if props["typeHandlerVersion"] != "2.0" || props["autoUpgradeMinorVersion"] != true {
		t.Fatalf("unexpected version/upgrade fields: %v / %v", props["typeHandlerVersion"], props["autoUpgradeMinorVersion"])
	}
	settings := props["settings"].(map[string]interface{})
	if settings["commandToExecute"] != "echo hi" {
		t.Fatalf("expected settings echoed, got %v", settings)
	}
	iv := props["instanceView"].(map[string]interface{})
	status := iv["status"].(map[string]interface{})
	if status["code"] != "ProvisioningState/succeeded" {
		t.Fatalf("expected instanceView status succeeded, got %v", status["code"])
	}

	// GET returns the stored extension.
	resp = doRequest(t, "GET", extensionURL(ts, "sub1", "rg1", "web", "ext"), "")
	expectStatus(t, resp, 200)
	m = decodeJSON(t, resp)
	resp.Body.Close()
	props = m["properties"].(map[string]interface{})
	if props["settings"].(map[string]interface{})["commandToExecute"] != "echo hi" {
		t.Fatalf("GET did not preserve settings: %v", props["settings"])
	}

	// List returns the extension in the ARM envelope.
	resp = doRequest(t, "GET", extensionURL(ts, "sub1", "rg1", "web", ""), "")
	expectStatus(t, resp, 200)
	m = decodeJSON(t, resp)
	resp.Body.Close()
	if got := len(m["value"].([]interface{})); got != 1 {
		t.Fatalf("expected 1 extension, got %d", got)
	}

	// PATCH merges tags and leaves settings intact.
	resp = doRequest(t, "PATCH", extensionURL(ts, "sub1", "rg1", "web", "ext"), `{"tags":{"env":"test"}}`)
	expectStatus(t, resp, 200)
	m = decodeJSON(t, resp)
	resp.Body.Close()
	tags := m["tags"].(map[string]interface{})
	if tags["env"] != "test" {
		t.Fatalf("expected tag env=test, got %v", tags)
	}
	props = m["properties"].(map[string]interface{})
	if props["settings"].(map[string]interface{})["commandToExecute"] != "echo hi" {
		t.Fatalf("PATCH dropped settings: %v", props["settings"])
	}

	// DELETE accepts asynchronously and the extension is gone.
	resp = doRequest(t, "DELETE", extensionURL(ts, "sub1", "rg1", "web", "ext"), "")
	expectStatus(t, resp, 202)
	resp.Body.Close()
	resp = doRequest(t, "GET", extensionURL(ts, "sub1", "rg1", "web", "ext"), "")
	expectStatus(t, resp, 404)
	resp.Body.Close()

	// PUT on a missing VM returns 404.
	resp = doRequest(t, "PUT", extensionURL(ts, "sub1", "rg1", "ghost", "ext"), putBody)
	expectStatus(t, resp, 404)
	resp.Body.Close()
}

func TestVMExtensionProtectedSettingsHidden(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "web")+vmAV, `{"location":"westus"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	// No location: the extension inherits the VM's location.
	body := `{"properties":{
		"publisher":"Microsoft.Azure.Extensions","type":"CustomScript",
		"typeHandlerVersion":"2.0",
		"settings":{"commandToExecute":"echo hi"},
		"protectedSettings":{"storageAccountKey":"secret"}}}`
	resp = doRequest(t, "PUT", extensionURL(ts, "sub1", "rg1", "web", "ext"), body)
	expectStatus(t, resp, 201)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(raw), "secret") {
		t.Fatalf("protected settings leaked into the PUT response: %s", raw)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decoding extension response: %v", err)
	}
	if m["location"] != "westus" {
		t.Fatalf("expected inherited location westus, got %v", m["location"])
	}
	props := m["properties"].(map[string]interface{})
	if _, ok := props["protectedSettings"]; ok {
		t.Fatalf("protectedSettings present in response properties: %v", props)
	}

	resp = doRequest(t, "GET", extensionURL(ts, "sub1", "rg1", "web", "ext"), "")
	expectStatus(t, resp, 200)
	raw, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(raw), "secret") {
		t.Fatalf("protected settings leaked into the GET response: %s", raw)
	}

	resp = doRequest(t, "GET", extensionURL(ts, "sub1", "rg1", "web", ""), "")
	expectStatus(t, resp, 200)
	raw, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(raw), "secret") {
		t.Fatalf("protected settings leaked into the list response: %s", raw)
	}
}
