package tests

import (
	"encoding/json"
	"testing"
)

const diskAPIVersion = "?api-version=2023-04-02"

func diskBase(tsURL string) string {
	return tsURL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/disks"
}

func TestManagedDiskLifecycle(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	base := diskBase(ts.URL)
	body := `{
		"location": "eastus",
		"sku": {"name": "Premium_LRS"},
		"properties": {
			"diskSizeGB": 64,
			"creationData": {"createOption": "Empty"}
		}
	}`

	resp := doRequest(t, "PUT", base+"/disk1"+diskAPIVersion, body)
	expectStatus(t, resp, 201)
	disk := decodeJSON(t, resp)
	resp.Body.Close()

	wantID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/disks/disk1"
	if disk["id"] != wantID {
		t.Fatalf("expected id=%s, got %v", wantID, disk["id"])
	}
	if disk["name"] != "disk1" {
		t.Fatalf("expected name=disk1, got %v", disk["name"])
	}
	if disk["type"] != "Microsoft.Compute/disks" {
		t.Fatalf("expected type=Microsoft.Compute/disks, got %v", disk["type"])
	}
	if disk["location"] != "eastus" {
		t.Fatalf("expected location=eastus, got %v", disk["location"])
	}

	sku, ok := disk["sku"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected sku object, got %T", disk["sku"])
	}
	if sku["name"] != "Premium_LRS" {
		t.Fatalf("expected sku.name=Premium_LRS, got %v", sku["name"])
	}

	props, ok := disk["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %T", disk["properties"])
	}
	if props["diskSizeGB"] != float64(64) {
		t.Fatalf("expected diskSizeGB=64, got %v", props["diskSizeGB"])
	}
	if props["diskSizeBytes"] != float64(68719476736) {
		t.Fatalf("expected diskSizeBytes=68719476736, got %v", props["diskSizeBytes"])
	}
	if props["publicNetworkAccess"] != "Enabled" {
		t.Fatalf("expected publicNetworkAccess=Enabled, got %v", props["publicNetworkAccess"])
	}
	if props["networkAccessPolicy"] != "AllowAll" {
		t.Fatalf("expected networkAccessPolicy=AllowAll, got %v", props["networkAccessPolicy"])
	}
	if props["diskState"] != "Unattached" {
		t.Fatalf("expected diskState=Unattached, got %v", props["diskState"])
	}
	if props["managedBy"] != "" {
		t.Fatalf("expected managedBy empty, got %v", props["managedBy"])
	}
	creationData, ok := props["creationData"].(map[string]interface{})
	if !ok || creationData["createOption"] != "Empty" {
		t.Fatalf("expected creationData.createOption=Empty, got %v", props["creationData"])
	}
	timeCreated, _ := props["timeCreated"].(string)
	if timeCreated == "" {
		t.Fatalf("expected a timeCreated, got %v", props["timeCreated"])
	}
	if _, found := disk["_vmOwned"]; found {
		t.Fatal("expected internal _vmOwned field to be stripped from the response")
	}

	// A GET must return the same generated timestamp.
	resp = doRequest(t, "GET", base+"/disk1"+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	getDisk := decodeJSON(t, resp)
	resp.Body.Close()
	getProps, ok := getDisk["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %T", getDisk["properties"])
	}
	if getProps["timeCreated"] != timeCreated {
		t.Fatalf("expected timeCreated to stay %v, got %v", timeCreated, getProps["timeCreated"])
	}

	// PATCH updates tags and size while preserving the record.
	resp = doRequest(t, "PATCH", base+"/disk1"+diskAPIVersion,
		`{"tags":{"env":"test"},"properties":{"diskSizeGB":128}}`)
	expectStatus(t, resp, 200)
	patched := decodeJSON(t, resp)
	resp.Body.Close()

	tags, ok := patched["tags"].(map[string]interface{})
	if !ok || tags["env"] != "test" {
		t.Fatalf("expected tags.env=test, got %v", patched["tags"])
	}
	patchedProps, ok := patched["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %T", patched["properties"])
	}
	if patchedProps["diskSizeGB"] != float64(128) {
		t.Fatalf("expected diskSizeGB=128 after PATCH, got %v", patchedProps["diskSizeGB"])
	}
	if patchedProps["diskSizeBytes"] != float64(137438953472) {
		t.Fatalf("expected diskSizeBytes to follow diskSizeGB after PATCH, got %v", patchedProps["diskSizeBytes"])
	}
	if patchedProps["timeCreated"] != timeCreated {
		t.Fatalf("expected timeCreated to stay %v after PATCH, got %v", timeCreated, patchedProps["timeCreated"])
	}

	// The resource-group list returns the single disk.
	resp = doRequest(t, "GET", base+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	list := decodeJSON(t, resp)
	resp.Body.Close()
	value, ok := list["value"].([]interface{})
	if !ok || len(value) != 1 {
		t.Fatalf("expected one disk in list, got %v", list["value"])
	}
	listed, ok := value[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected listed disk object, got %T", value[0])
	}
	if _, found := listed["_vmOwned"]; found {
		t.Fatal("expected internal _vmOwned field to be stripped from list responses")
	}

	// DELETE returns 202 and the disk is gone.
	resp = doRequest(t, "DELETE", base+"/disk1"+diskAPIVersion, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	resp = doRequest(t, "GET", base+"/disk1"+diskAPIVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
}

func TestManagedDiskPUTPreservesAttachmentFields(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	base := diskBase(ts.URL)

	resp := doRequest(t, "PUT", base+"/disk1"+diskAPIVersion,
		`{"location":"eastus","properties":{}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "GET", base+"/disk1"+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	original := decodeJSON(t, resp)
	resp.Body.Close()

	origProps, ok := original["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %T", original["properties"])
	}
	origSku, ok := original["sku"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected sku object, got %T", original["sku"])
	}

	// Change only the tags and PUT the whole model back, as the provider does.
	original["tags"] = map[string]interface{}{"env": "test"}
	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal disk body: %v", err)
	}

	resp = doRequest(t, "PUT", base+"/disk1"+diskAPIVersion, string(payload))
	expectStatus(t, resp, 200)
	resp.Body.Close()

	resp = doRequest(t, "GET", base+"/disk1"+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	updated := decodeJSON(t, resp)
	resp.Body.Close()

	updProps, ok := updated["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %T", updated["properties"])
	}
	updSku, ok := updated["sku"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected sku object, got %T", updated["sku"])
	}

	tags, ok := updated["tags"].(map[string]interface{})
	if !ok || tags["env"] != "test" {
		t.Fatalf("expected tags.env=test after PUT, got %v", updated["tags"])
	}
	if updProps["timeCreated"] != origProps["timeCreated"] {
		t.Fatalf("expected timeCreated %v, got %v", origProps["timeCreated"], updProps["timeCreated"])
	}
	if updProps["diskSizeBytes"] != origProps["diskSizeBytes"] {
		t.Fatalf("expected diskSizeBytes %v, got %v", origProps["diskSizeBytes"], updProps["diskSizeBytes"])
	}
	if updProps["diskState"] != origProps["diskState"] {
		t.Fatalf("expected diskState %v, got %v", origProps["diskState"], updProps["diskState"])
	}
	if updProps["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState=Succeeded, got %v", updProps["provisioningState"])
	}
	if updProps["publicNetworkAccess"] != "Enabled" {
		t.Fatalf("expected publicNetworkAccess=Enabled, got %v", updProps["publicNetworkAccess"])
	}
	if updProps["networkAccessPolicy"] != "AllowAll" {
		t.Fatalf("expected networkAccessPolicy=AllowAll, got %v", updProps["networkAccessPolicy"])
	}
	creationData, ok := updProps["creationData"].(map[string]interface{})
	if !ok || creationData["createOption"] != "Empty" {
		t.Fatalf("expected creationData.createOption=Empty, got %v", updProps["creationData"])
	}
	if updSku["name"] != origSku["name"] || updSku["tier"] != origSku["tier"] {
		t.Fatalf("expected sku to stay %v, got %v", origSku, updSku)
	}
	if _, found := updated["_vmOwned"]; found {
		t.Fatal("expected internal _vmOwned field to be stripped from the response")
	}
}
