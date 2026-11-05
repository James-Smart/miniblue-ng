package tests

import (
	"encoding/json"
	"testing"
)

// TestVMPatchPreservesUnknownPropertyBlocks verifies that a PATCH merges only
// the blocks it supplies: tags and nested property maps update, while unknown
// pass-through blocks and the generated OS disk reference are preserved.
func TestVMPatchPreservesUnknownPropertyBlocks(t *testing.T) {
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
		"scheduledEventsProfile":{"terminateNotificationProfile":{"enable":false,"notBeforeTimeout":"PT5M"}},
		"storageProfile":{"osDisk":{"caching":"ReadWrite","storageAccountType":"StandardSSD_LRS","diskSizeGB":64}},
		"hardwareProfile":{"vmSize":"Standard_B2s"}
	}}`
	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "web")+vmAV, body)
	expectStatus(t, resp, 201)
	created := decodeJSON(t, resp)
	resp.Body.Close()

	createdProps := created["properties"].(map[string]interface{})
	createdOSDisk := createdProps["storageProfile"].(map[string]interface{})["osDisk"].(map[string]interface{})
	diskName, _ := createdOSDisk["name"].(string)
	diskID, _ := createdOSDisk["managedDisk"].(map[string]interface{})["id"].(string)
	if diskName == "" || diskID == "" {
		t.Fatalf("expected a generated OS disk, got name=%q id=%q", diskName, diskID)
	}

	patch := `{"tags":{"env":"test"},"properties":{
		"hardwareProfile":{"vmSize":"Standard_D2s_v3"},
		"securityProfile":{"encryptionAtHost":false}
	}}`
	resp = doRequest(t, "PATCH", vmURL(ts, "sub1", "rg1", "web")+vmAV, patch)
	expectStatus(t, resp, 200)
	resp.Body.Close()

	resp = doRequest(t, "GET", vmURL(ts, "sub1", "rg1", "web")+vmAV, "")
	expectStatus(t, resp, 200)
	got := decodeJSON(t, resp)
	resp.Body.Close()

	tags, ok := got["tags"].(map[string]interface{})
	if !ok || tags["env"] != "test" {
		t.Fatalf("expected tags.env test, got %v", got["tags"])
	}
	props := got["properties"].(map[string]interface{})

	hardware, _ := props["hardwareProfile"].(map[string]interface{})
	if hardware["vmSize"] != "Standard_D2s_v3" {
		t.Fatalf("expected vmSize Standard_D2s_v3, got %v", hardware["vmSize"])
	}
	security, _ := props["securityProfile"].(map[string]interface{})
	if security["encryptionAtHost"] != false {
		t.Fatalf("expected encryptionAtHost false after merge, got %v", security["encryptionAtHost"])
	}

	// Blocks not mentioned in the PATCH must survive untouched.
	for _, key := range []string{
		"diagnosticsProfile",
		"additionalCapabilities",
		"scheduledEventsProfile",
	} {
		if _, ok := props[key]; !ok {
			t.Fatalf("expected property block %q to be preserved, got %v", key, props[key])
		}
	}
	if props["priority"] != "Spot" {
		t.Fatalf("expected priority Spot to be preserved, got %v", props["priority"])
	}
	if props["evictionPolicy"] != "Deallocate" {
		t.Fatalf("expected evictionPolicy Deallocate to be preserved, got %v", props["evictionPolicy"])
	}

	storage, _ := props["storageProfile"].(map[string]interface{})
	osDisk, _ := storage["osDisk"].(map[string]interface{})
	if osDisk["name"] != diskName {
		t.Fatalf("expected OS disk name %q to be preserved, got %v", diskName, osDisk["name"])
	}
	if md, _ := osDisk["managedDisk"].(map[string]interface{}); md["id"] != diskID {
		t.Fatalf("expected OS disk id %q to be preserved, got %v", diskID, md["id"])
	}
}

// TestVMPutFullModelPreservesGeneratedFields simulates a provider that GETs a
// VM, strips read-only blocks and PUTs the whole model back. Generated values
// must survive, and an explicit null identity must clear the block.
func TestVMPutFullModelPreservesGeneratedFields(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	armID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.ManagedIdentity/userAssignedIdentities/app-id"
	resp := doRequest(t, "PUT", identityURL(ts, "sub1", "rg1", "app-id")+vmAV, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	body := `{"location":"eastus",
		"identity":{"type":"UserAssigned","userAssignedIdentities":{"` + armID + `":{}}},
		"properties":{"hardwareProfile":{"vmSize":"Standard_B2s"}}}`
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "web")+vmAV, body)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "GET", vmURL(ts, "sub1", "rg1", "web")+vmAV, "")
	expectStatus(t, resp, 200)
	full := decodeJSON(t, resp)
	resp.Body.Close()

	props := full["properties"].(map[string]interface{})
	vmID, _ := props["vmId"].(string)
	osDisk := props["storageProfile"].(map[string]interface{})["osDisk"].(map[string]interface{})
	diskName, _ := osDisk["name"].(string)
	diskID, _ := osDisk["managedDisk"].(map[string]interface{})["id"].(string)
	diskSize := osDisk["diskSizeGB"]
	if vmID == "" || diskName == "" || diskID == "" || diskSize == nil {
		t.Fatalf("expected generated vmId and OS disk fields, got vmId=%q diskName=%q diskID=%q size=%v",
			vmID, diskName, diskID, diskSize)
	}
	ivBefore := props["instanceView"].(map[string]interface{})
	if !hasStatus(ivBefore["statuses"].([]interface{}), "PowerState/running") {
		t.Fatalf("expected the VM to report PowerState/running before the update, got %v", ivBefore["statuses"])
	}
	if _, ok := full["identity"]; !ok {
		t.Fatal("expected the created VM to have an identity block")
	}

	// Only read-only/generated blocks are dropped, exactly as a provider would
	// do before PUTting a model it just read.
	delete(props, "instanceView")
	delete(props, "miniblue")

	payload, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal full model: %v", err)
	}
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "web")+vmAV, string(payload))
	expectStatus(t, resp, 200)
	resp.Body.Close()

	resp = doRequest(t, "GET", vmURL(ts, "sub1", "rg1", "web")+vmAV, "")
	expectStatus(t, resp, 200)
	got := decodeJSON(t, resp)
	resp.Body.Close()

	gotProps := got["properties"].(map[string]interface{})
	if gotProps["vmId"] != vmID {
		t.Fatalf("expected vmId %q to be preserved, got %v", vmID, gotProps["vmId"])
	}
	gotOSDisk := gotProps["storageProfile"].(map[string]interface{})["osDisk"].(map[string]interface{})
	if gotOSDisk["name"] != diskName {
		t.Fatalf("expected OS disk name %q to be preserved, got %v", diskName, gotOSDisk["name"])
	}
	if md, _ := gotOSDisk["managedDisk"].(map[string]interface{}); md["id"] != diskID {
		t.Fatalf("expected OS disk id %q to be preserved, got %v", diskID, md["id"])
	}
	if gotOSDisk["diskSizeGB"] != diskSize {
		t.Fatalf("expected diskSizeGB %v to be preserved, got %v", diskSize, gotOSDisk["diskSizeGB"])
	}
	gotIV := gotProps["instanceView"].(map[string]interface{})
	if !hasStatus(gotIV["statuses"].([]interface{}), "PowerState/running") {
		t.Fatalf("expected PowerState/running after the full PUT, got %v", gotIV["statuses"])
	}
	if _, ok := got["identity"]; !ok {
		t.Fatal("expected the identity block to survive a full PUT")
	}

	// An explicit null removes the identity block from PATCH.
	resp = doRequest(t, "PATCH", vmURL(ts, "sub1", "rg1", "web")+vmAV, `{"identity":null}`)
	expectStatus(t, resp, 200)
	resp.Body.Close()

	resp = doRequest(t, "GET", vmURL(ts, "sub1", "rg1", "web")+vmAV, "")
	expectStatus(t, resp, 200)
	got = decodeJSON(t, resp)
	resp.Body.Close()
	if _, ok := got["identity"]; ok {
		t.Fatalf("expected identity block to be removed by null, got %v", got["identity"])
	}
}

// TestVMPatchReplacesOSDisk verifies that pointing an existing VM's OS disk at
// another managed disk detaches the old disk, attaches the new one, and
// refreshes the VM's storage profile from the new disk.
func TestVMPatchReplacesOSDisk(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	body := `{"location":"eastus","properties":{
		"storageProfile":{"osDisk":{"caching":"ReadOnly","createOption":"FromImage"}}
	}}`
	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm-a")+vmAV, body)
	expectStatus(t, resp, 201)
	vmA := decodeJSON(t, resp)
	resp.Body.Close()

	vmAProps := vmA["properties"].(map[string]interface{})
	vmAID, _ := vmAProps["vmId"].(string)
	oldOSDisk := vmAProps["storageProfile"].(map[string]interface{})["osDisk"].(map[string]interface{})
	oldName, _ := oldOSDisk["name"].(string)
	oldID, _ := oldOSDisk["managedDisk"].(map[string]interface{})["id"].(string)
	if oldName == "" || oldID == "" {
		t.Fatalf("expected a generated OS disk, got name=%q id=%q", oldName, oldID)
	}

	resp = doRequest(t, "PUT", diskBase(ts.URL)+"/replacement"+diskAPIVersion,
		`{"location":"eastus","sku":{"name":"Premium_LRS"},"properties":{"diskSizeGB":128,"creationData":{"createOption":"Empty"}}}`)
	expectStatus(t, resp, 201)
	diskB := decodeJSON(t, resp)
	resp.Body.Close()
	diskBID, _ := diskB["id"].(string)
	diskBName, _ := diskB["name"].(string)
	if diskBID == "" || diskBName == "" {
		t.Fatalf("expected a replacement disk, got id=%q name=%q", diskBID, diskBName)
	}

	// The caller does not supply name/disk fields: they must be refreshed from
	// the newly attached disk while caching/createOption stay as requested.
	patch := `{"properties":{"storageProfile":{"osDisk":{"managedDisk":{"id":"` + diskBID + `"}}}}}`
	resp = doRequest(t, "PATCH", vmURL(ts, "sub1", "rg1", "vm-a")+vmAV, patch)
	expectStatus(t, resp, 200)
	patched := decodeJSON(t, resp)
	resp.Body.Close()

	patchedOSDisk := patched["properties"].(map[string]interface{})["storageProfile"].(map[string]interface{})["osDisk"].(map[string]interface{})
	if md, _ := patchedOSDisk["managedDisk"].(map[string]interface{}); md["id"] != diskBID {
		t.Fatalf("expected managedDisk.id %q, got %v", diskBID, md["id"])
	}
	if patchedOSDisk["name"] != diskBName {
		t.Fatalf("expected osDisk.name %q from the attached disk, got %v", diskBName, patchedOSDisk["name"])
	}
	if patchedOSDisk["caching"] != "ReadOnly" {
		t.Fatalf("expected caching ReadOnly to be preserved, got %v", patchedOSDisk["caching"])
	}
	if patchedOSDisk["createOption"] != "FromImage" {
		t.Fatalf("expected createOption FromImage to be preserved, got %v", patchedOSDisk["createOption"])
	}
	if patchedOSDisk["diskSizeGB"] != float64(128) {
		t.Fatalf("expected diskSizeGB 128 from the attached disk, got %v", patchedOSDisk["diskSizeGB"])
	}
	if md, _ := patchedOSDisk["managedDisk"].(map[string]interface{}); md["storageAccountType"] != "Premium_LRS" {
		t.Fatalf("expected storageAccountType Premium_LRS from the attached disk, got %v", md["storageAccountType"])
	}

	// The old disk is detached but still exists and remains deletable.
	resp = doRequest(t, "GET", ts.URL+oldID+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	oldDisk := decodeJSON(t, resp)
	resp.Body.Close()
	oldProps := oldDisk["properties"].(map[string]interface{})
	if oldProps["managedBy"] != "" {
		t.Fatalf("expected old disk managedBy empty, got %v", oldProps["managedBy"])
	}
	if oldProps["diskState"] != "Unattached" {
		t.Fatalf("expected old disk diskState Unattached, got %v", oldProps["diskState"])
	}

	// The new disk belongs to the VM.
	resp = doRequest(t, "GET", ts.URL+diskBID+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	newDisk := decodeJSON(t, resp)
	resp.Body.Close()
	newProps := newDisk["properties"].(map[string]interface{})
	if newProps["managedBy"] != vmAID {
		t.Fatalf("expected new disk managedBy %q, got %v", vmAID, newProps["managedBy"])
	}
	if newProps["diskState"] != "Attached" {
		t.Fatalf("expected new disk diskState Attached, got %v", newProps["diskState"])
	}
}

// TestVMPatchRejectsOSDiskAttachedToAnotherVM verifies that an OS disk swap is
// validated before anything is mutated: a disk owned by another VM yields 409
// DiskInUse and the VM keeps its original disk.
func TestVMPatchRejectsOSDiskAttachedToAnotherVM(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	resp := doRequest(t, "PUT", diskBase(ts.URL)+"/shared"+diskAPIVersion,
		`{"location":"eastus","sku":{"name":"Premium_LRS"},"properties":{"diskSizeGB":128,"creationData":{"createOption":"Empty"}}}`)
	expectStatus(t, resp, 201)
	shared := decodeJSON(t, resp)
	resp.Body.Close()
	sharedID, _ := shared["id"].(string)

	body := `{"location":"eastus","properties":{"storageProfile":{"osDisk":{"managedDisk":{"id":"` + sharedID + `"}}}}}`
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm-a")+vmAV, body)
	expectStatus(t, resp, 201)
	vmA := decodeJSON(t, resp)
	resp.Body.Close()
	vmAID := vmA["properties"].(map[string]interface{})["vmId"].(string)

	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm-b")+vmAV, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	vmB := decodeJSON(t, resp)
	resp.Body.Close()
	vmBProps := vmB["properties"].(map[string]interface{})
	vmBOSDisk := vmBProps["storageProfile"].(map[string]interface{})["osDisk"].(map[string]interface{})
	vmBOldName, _ := vmBOSDisk["name"].(string)
	vmBOldID, _ := vmBOSDisk["managedDisk"].(map[string]interface{})["id"].(string)

	patch := `{"properties":{"storageProfile":{"osDisk":{"managedDisk":{"id":"` + sharedID + `"}}}}}`
	resp = doRequest(t, "PATCH", vmURL(ts, "sub1", "rg1", "vm-b")+vmAV, patch)
	expectStatus(t, resp, 409)
	e := decodeError(t, resp)
	resp.Body.Close()
	if e.Error.Code != "DiskInUse" {
		t.Fatalf("expected DiskInUse, got %s", e.Error.Code)
	}

	// The VM must be unchanged after the rejected swap.
	resp = doRequest(t, "GET", vmURL(ts, "sub1", "rg1", "vm-b")+vmAV, "")
	expectStatus(t, resp, 200)
	got := decodeJSON(t, resp)
	resp.Body.Close()
	gotOSDisk := got["properties"].(map[string]interface{})["storageProfile"].(map[string]interface{})["osDisk"].(map[string]interface{})
	if gotOSDisk["name"] != vmBOldName {
		t.Fatalf("expected VM B to keep OS disk name %q, got %v", vmBOldName, gotOSDisk["name"])
	}
	if md, _ := gotOSDisk["managedDisk"].(map[string]interface{}); md["id"] != vmBOldID {
		t.Fatalf("expected VM B to keep OS disk id %q, got %v", vmBOldID, md["id"])
	}

	// The shared disk still belongs to VM A.
	resp = doRequest(t, "GET", ts.URL+sharedID+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	sharedDisk := decodeJSON(t, resp)
	resp.Body.Close()
	sharedProps := sharedDisk["properties"].(map[string]interface{})
	if sharedProps["managedBy"] != vmAID || sharedProps["diskState"] != "Attached" {
		t.Fatalf("expected shared disk to stay attached to %q, got managedBy=%v diskState=%v",
			vmAID, sharedProps["managedBy"], sharedProps["diskState"])
	}
}
