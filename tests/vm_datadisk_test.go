package tests

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// getVMModel GETs the VM and returns the full response body, which is what the
// provider sends back on a data-disk attachment update.
func getVMModel(t *testing.T, ts *httptest.Server, sub, rg, name string) map[string]interface{} {
	t.Helper()
	resp := doRequest(t, "GET", vmURL(ts, sub, rg, name)+vmAV, "")
	expectStatus(t, resp, 200)
	m := decodeJSON(t, resp)
	resp.Body.Close()
	return m
}

func vmStorage(m map[string]interface{}) map[string]interface{} {
	props := m["properties"].(map[string]interface{})
	return props["storageProfile"].(map[string]interface{})
}

func dataDisks(m map[string]interface{}) []interface{} {
	disks, _ := vmStorage(m)["dataDisks"].([]interface{})
	return disks
}

// TestVMDiskAttachmentRoundTrip mirrors the provider's
// azurerm_virtual_machine_data_disk_attachment flow: create a managed disk and
// a VM, GET the whole VM model, add a dataDisks entry, PUT it back, then PUT the
// model with an empty array to detach. The OS disk must survive both updates.
func TestVMDiskAttachmentRoundTrip(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	resp := doRequest(t, "PUT", diskBase(ts.URL)+"/data1"+diskAPIVersion,
		`{"location":"eastus","sku":{"name":"Standard_LRS"},"properties":{"diskSizeGB":32,"creationData":{"createOption":"Empty"}}}`)
	expectStatus(t, resp, 201)
	disk := decodeJSON(t, resp)
	resp.Body.Close()
	diskID, _ := disk["id"].(string)
	if diskID == "" {
		t.Fatal("expected managed disk id")
	}

	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm")+vmAV, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	created := decodeJSON(t, resp)
	resp.Body.Close()
	vmID, _ := created["properties"].(map[string]interface{})["vmId"].(string)
	if vmID == "" {
		t.Fatal("expected non-empty vmId")
	}

	// The provider GETs the whole VM and PUTs the whole model back.
	vm := getVMModel(t, ts, "sub1", "rg1", "vm")
	vmStorage(vm)["dataDisks"] = []interface{}{
		map[string]interface{}{
			"name":         "data1",
			"lun":          float64(0),
			"caching":      "ReadWrite",
			"createOption": "Attach",
			"managedDisk": map[string]interface{}{
				"id":                 diskID,
				"storageAccountType": "Standard_LRS",
			},
		},
	}
	body, err := json.Marshal(vm)
	if err != nil {
		t.Fatal(err)
	}
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm")+vmAV, string(body))
	expectStatus(t, resp, 200)
	resp.Body.Close()

	// The disk is now attached to the VM.
	resp = doRequest(t, "GET", ts.URL+diskID+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	disk = decodeJSON(t, resp)
	resp.Body.Close()
	diskProps := disk["properties"].(map[string]interface{})
	if diskProps["managedBy"] != vmID {
		t.Fatalf("expected data disk managedBy %q, got %v", vmID, diskProps["managedBy"])
	}
	if diskProps["diskState"] != "Attached" {
		t.Fatalf("expected data disk diskState Attached, got %v", diskProps["diskState"])
	}

	// The VM round-trips the data disk entry.
	vm = getVMModel(t, ts, "sub1", "rg1", "vm")
	gotDisks := dataDisks(vm)
	if len(gotDisks) != 1 {
		t.Fatalf("expected 1 data disk, got %d: %v", len(gotDisks), gotDisks)
	}
	gotDisk := gotDisks[0].(map[string]interface{})
	if gotDisk["name"] != "data1" {
		t.Fatalf("expected data disk name data1, got %v", gotDisk["name"])
	}
	if gotDisk["lun"] != float64(0) {
		t.Fatalf("expected data disk lun 0, got %v", gotDisk["lun"])
	}

	// Remove the data disk from the model; it must be detached, not deleted.
	vm = getVMModel(t, ts, "sub1", "rg1", "vm")
	vmStorage(vm)["dataDisks"] = []interface{}{}
	body, err = json.Marshal(vm)
	if err != nil {
		t.Fatal(err)
	}
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm")+vmAV, string(body))
	expectStatus(t, resp, 200)
	resp.Body.Close()

	resp = doRequest(t, "GET", ts.URL+diskID+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	disk = decodeJSON(t, resp)
	resp.Body.Close()
	diskProps = disk["properties"].(map[string]interface{})
	if diskProps["managedBy"] != "" {
		t.Fatalf("expected detached data disk managedBy empty, got %v", diskProps["managedBy"])
	}
	if diskProps["diskState"] != "Unattached" {
		t.Fatalf("expected detached data disk diskState Unattached, got %v", diskProps["diskState"])
	}

	// The OS disk is untouched by data-disk syncing.
	vm = getVMModel(t, ts, "sub1", "rg1", "vm")
	osDisk, ok := vmStorage(vm)["osDisk"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected storageProfile.osDisk to survive, got %v", vmStorage(vm)["osDisk"])
	}
	md, _ := osDisk["managedDisk"].(map[string]interface{})
	if id, _ := md["id"].(string); id == "" {
		t.Fatalf("expected OS disk managedDisk.id to survive, got %v", osDisk["managedDisk"])
	}
}

// TestVMCreateWithMissingDataDisk verifies that a VM PUT referencing a data
// disk that does not exist yet creates the disk record with the documented
// defaults (createOption Attach) and attaches it.
func TestVMCreateWithMissingDataDisk(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	missingID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/disks/data2"
	body := `{"location":"eastus","properties":{"storageProfile":{"dataDisks":[
		{"name":"data2","lun":1,"caching":"None","createOption":"Attach","diskSizeGB":64,
		 "managedDisk":{"id":"` + missingID + `","storageAccountType":"Premium_LRS"}}]}}}`

	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm")+vmAV, body)
	expectStatus(t, resp, 201)
	created := decodeJSON(t, resp)
	resp.Body.Close()
	vmID, _ := created["properties"].(map[string]interface{})["vmId"].(string)
	if vmID == "" {
		t.Fatal("expected non-empty vmId")
	}

	resp = doRequest(t, "GET", ts.URL+missingID+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	disk := decodeJSON(t, resp)
	resp.Body.Close()
	diskProps := disk["properties"].(map[string]interface{})
	if diskProps["managedBy"] != vmID {
		t.Fatalf("expected created data disk managedBy %q, got %v", vmID, diskProps["managedBy"])
	}
	if diskProps["diskState"] != "Attached" {
		t.Fatalf("expected created data disk diskState Attached, got %v", diskProps["diskState"])
	}
	creationData, _ := diskProps["creationData"].(map[string]interface{})
	if creationData["createOption"] != "Attach" {
		t.Fatalf("expected creationData.createOption Attach, got %v", creationData["createOption"])
	}
	if diskProps["diskSizeGB"] != float64(64) {
		t.Fatalf("expected diskSizeGB 64, got %v", diskProps["diskSizeGB"])
	}
	sku, _ := disk["sku"].(map[string]interface{})
	if sku["name"] != "Premium_LRS" {
		t.Fatalf("expected sku.name Premium_LRS, got %v", sku["name"])
	}
}

// createManagedDisk creates a managed disk and returns its ARM ID.
func createManagedDisk(t *testing.T, ts *httptest.Server, name string) string {
	t.Helper()
	resp := doRequest(t, "PUT", diskBase(ts.URL)+"/"+name+diskAPIVersion,
		`{"location":"eastus","sku":{"name":"Standard_LRS"},"properties":{"diskSizeGB":32,"creationData":{"createOption":"Empty"}}}`)
	expectStatus(t, resp, 201)
	disk := decodeJSON(t, resp)
	resp.Body.Close()
	diskID, _ := disk["id"].(string)
	if diskID == "" {
		t.Fatal("expected managed disk id")
	}
	return diskID
}

// dataDiskAttachment builds a provider-shaped dataDisks entry for diskID.
func dataDiskAttachment(name, diskID string) map[string]interface{} {
	return map[string]interface{}{
		"name":         name,
		"lun":          float64(0),
		"caching":      "ReadWrite",
		"createOption": "Attach",
		"managedDisk": map[string]interface{}{
			"id":                 diskID,
			"storageAccountType": "Standard_LRS",
		},
	}
}

// TestVMDataDiskRejectsDiskAttachedToAnotherVM verifies that a data disk
// already attached to one VM cannot be claimed by another VM. Both the create
// and update paths must return 409 DiskInUse before mutating anything: the
// second VM must not be stored/mutated and the disk must stay with its owner.
func TestVMDataDiskRejectsDiskAttachedToAnotherVM(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	diskID := createManagedDisk(t, ts, "shareddata")

	body := `{"location":"eastus","properties":{"storageProfile":{"dataDisks":[
		{"name":"shareddata","lun":0,"caching":"ReadWrite","createOption":"Attach",
		 "managedDisk":{"id":"` + diskID + `","storageAccountType":"Standard_LRS"}}]}}}`
	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm-a")+vmAV, body)
	expectStatus(t, resp, 201)
	vmA := decodeJSON(t, resp)
	resp.Body.Close()
	vmAID := vmA["properties"].(map[string]interface{})["vmId"].(string)

	resp = doRequest(t, "GET", ts.URL+diskID+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	disk := decodeJSON(t, resp)
	resp.Body.Close()
	if got := disk["properties"].(map[string]interface{})["managedBy"]; got != vmAID {
		t.Fatalf("expected disk owned by %q, got %v", vmAID, got)
	}

	// Create a second VM that tries to claim the same data disk.
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm-b")+vmAV, body)
	expectStatus(t, resp, 409)
	e := decodeError(t, resp)
	resp.Body.Close()
	if e.Error.Code != "DiskInUse" {
		t.Fatalf("expected DiskInUse, got %s", e.Error.Code)
	}
	resp = doRequest(t, "GET", vmURL(ts, "sub1", "rg1", "vm-b")+vmAV, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()

	// An existing VM must be left untouched when an update adds the foreign disk.
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm-c")+vmAV, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	vmC := decodeJSON(t, resp)
	resp.Body.Close()
	vmCID := vmC["properties"].(map[string]interface{})["vmId"].(string)

	model := getVMModel(t, ts, "sub1", "rg1", "vm-c")
	vmStorage(model)["dataDisks"] = []interface{}{dataDiskAttachment("shareddata", diskID)}
	raw, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm-c")+vmAV, string(raw))
	expectStatus(t, resp, 409)
	e = decodeError(t, resp)
	resp.Body.Close()
	if e.Error.Code != "DiskInUse" {
		t.Fatalf("expected DiskInUse, got %s", e.Error.Code)
	}

	// vm-a is unchanged and still owns the disk.
	vmA = getVMModel(t, ts, "sub1", "rg1", "vm-a")
	if got := vmA["properties"].(map[string]interface{})["vmId"]; got != vmAID {
		t.Fatalf("expected vm-a vmId %q, got %v", vmAID, got)
	}
	resp = doRequest(t, "GET", ts.URL+diskID+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	disk = decodeJSON(t, resp)
	resp.Body.Close()
	diskProps := disk["properties"].(map[string]interface{})
	if diskProps["managedBy"] != vmAID {
		t.Fatalf("expected disk to stay attached to %q, got %v", vmAID, diskProps["managedBy"])
	}
	if diskProps["diskState"] != "Attached" {
		t.Fatalf("expected diskState Attached, got %v", diskProps["diskState"])
	}

	// vm-c never claimed the disk and its identity is unchanged.
	vmC = getVMModel(t, ts, "sub1", "rg1", "vm-c")
	if got := vmC["properties"].(map[string]interface{})["vmId"]; got != vmCID {
		t.Fatalf("expected vm-c vmId %q, got %v", vmCID, got)
	}
	if got := dataDisks(vmC); len(got) != 0 {
		t.Fatalf("expected vm-c to have no data disks, got %v", got)
	}
}

// TestVMDataDiskRejectsMalformedID verifies that a data-disk entry whose
// managed-disk ID cannot be parsed is rejected with 404 DiskNotFound instead of
// being silently kept. The create path must not store the VM and the update
// path must not mutate an existing VM.
func TestVMDataDiskRejectsMalformedID(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	malformedID := "/not/a/managed/disk/id"
	body := `{"location":"eastus","properties":{"storageProfile":{"dataDisks":[
		{"name":"bad","lun":0,"caching":"None","createOption":"Attach",
		 "managedDisk":{"id":"` + malformedID + `"}}]}}}`

	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm-bad")+vmAV, body)
	expectStatus(t, resp, 404)
	e := decodeError(t, resp)
	resp.Body.Close()
	if e.Error.Code != "DiskNotFound" {
		t.Fatalf("expected DiskNotFound, got %s", e.Error.Code)
	}
	resp = doRequest(t, "GET", vmURL(ts, "sub1", "rg1", "vm-bad")+vmAV, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()

	// An update that adds the malformed reference must be rejected and leave the
	// existing VM (including its OS disk) untouched.
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm-ok")+vmAV, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	created := decodeJSON(t, resp)
	resp.Body.Close()
	vmID := created["properties"].(map[string]interface{})["vmId"].(string)
	osDiskID := osDiskManagedID(t, created)

	model := getVMModel(t, ts, "sub1", "rg1", "vm-ok")
	vmStorage(model)["dataDisks"] = []interface{}{
		map[string]interface{}{
			"name":         "bad",
			"lun":          float64(0),
			"createOption": "Attach",
			"managedDisk":  map[string]interface{}{"id": malformedID},
		},
	}
	raw, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm-ok")+vmAV, string(raw))
	expectStatus(t, resp, 404)
	e = decodeError(t, resp)
	resp.Body.Close()
	if e.Error.Code != "DiskNotFound" {
		t.Fatalf("expected DiskNotFound, got %s", e.Error.Code)
	}

	model = getVMModel(t, ts, "sub1", "rg1", "vm-ok")
	props := model["properties"].(map[string]interface{})
	if got := props["vmId"]; got != vmID {
		t.Fatalf("expected vmId to stay %q, got %v", vmID, got)
	}
	if got := dataDisks(model); len(got) != 0 {
		t.Fatalf("expected no data disks after rejected update, got %v", got)
	}
	if got := osDiskManagedID(t, model); got != osDiskID {
		t.Fatalf("expected OS disk to stay %q, got %v", osDiskID, got)
	}
}

// osDiskManagedID returns the managed-disk ID of a VM response's OS disk.
func osDiskManagedID(t *testing.T, vm map[string]interface{}) string {
	t.Helper()
	osDisk, _ := vmStorage(vm)["osDisk"].(map[string]interface{})
	md, _ := osDisk["managedDisk"].(map[string]interface{})
	id, _ := md["id"].(string)
	if id == "" {
		t.Fatalf("expected OS disk managedDisk.id, got %v", osDisk["managedDisk"])
	}
	return id
}
