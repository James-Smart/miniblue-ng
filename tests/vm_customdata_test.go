package tests

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestVMCustomDataStubMode pins the Docker-free contract for custom data: the
// VM is still created, and the skipped execution is recorded with a reason the
// caller can inspect. Execution itself is covered by the Docker integration
// test.
func TestVMCustomDataStubMode(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	customData := base64.StdEncoding.EncodeToString([]byte("echo hello"))
	body := `{"location":"eastus","properties":{"osProfile":{"customData":"` + customData + `"}}}`
	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "web")+vmAV, body)
	expectStatus(t, resp, 201)
	m := decodeJSON(t, resp)
	resp.Body.Close()

	mb := m["properties"].(map[string]interface{})["miniblue"].(map[string]interface{})
	status, ok := mb["customData"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties.miniblue.customData status, got %v", mb["customData"])
	}
	if status["executed"] != false {
		t.Fatalf("expected customData.executed false in stub mode, got %v", status["executed"])
	}
	reason, _ := status["reason"].(string)
	if !strings.Contains(strings.ToLower(reason), "docker") {
		t.Fatalf("expected reason to mention docker, got %q", reason)
	}
}
