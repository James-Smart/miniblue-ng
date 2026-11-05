package tests

import (
	"regexp"
	"testing"
)

// TestVMOSDiskLifecycle verifies that creating a VM without an explicit managed
// disk generates an OS disk in the same resource group, attaches it, surfaces
// it in the VM response, and detaches (but does not delete) it on VM delete.
func TestVMOSDiskLifecycle(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	body := `{"location":"eastus","properties":{
		"storageProfile":{"osDisk":{"caching":"ReadWrite","storageAccountType":"StandardSSD_LRS","diskSizeGB":64}},
		"hardwareProfile":{"vmSize":"Standard_B2s"}
	}}`
	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm")+vmAV, body)
	expectStatus(t, resp, 201)
	m := decodeJSON(t, resp)
	resp.Body.Close()

	props, ok := m["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %T", m["properties"])
	}
	vmID, _ := props["vmId"].(string)
	if vmID == "" {
		t.Fatalf("expected non-empty vmId, got %v", props["vmId"])
	}

	storage, ok := props["storageProfile"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected storageProfile object, got %T", props["storageProfile"])
	}
	osDisk, ok := storage["osDisk"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected storageProfile.osDisk object, got %T", storage["osDisk"])
	}

	name, _ := osDisk["name"].(string)
	if !regexp.MustCompile(`^vm_OsDisk_1_[0-9a-f]{8}$`).MatchString(name) {
		t.Fatalf("expected generated OS disk name matching vm_OsDisk_1_<8 hex>, got %q", name)
	}

	md, ok := osDisk["managedDisk"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected osDisk.managedDisk object, got %T", osDisk["managedDisk"])
	}
	diskID, _ := md["id"].(string)
	if diskID == "" {
		t.Fatalf("expected osDisk.managedDisk.id, got %v", md["id"])
	}
	if md["storageAccountType"] != "StandardSSD_LRS" {
		t.Fatalf("expected managedDisk.storageAccountType StandardSSD_LRS, got %v", md["storageAccountType"])
	}
	if osDisk["diskSizeGB"] != float64(64) {
		t.Fatalf("expected diskSizeGB 64, got %v", osDisk["diskSizeGB"])
	}
	if osDisk["osType"] != "Linux" {
		t.Fatalf("expected osType Linux, got %v", osDisk["osType"])
	}
	if osDisk["createOption"] != "FromImage" {
		t.Fatalf("expected createOption FromImage, got %v", osDisk["createOption"])
	}
	if osDisk["caching"] != "ReadWrite" {
		t.Fatalf("expected caching ReadWrite, got %v", osDisk["caching"])
	}

	// The generated disk exists, is attached to the VM and carries the SKU.
	resp = doRequest(t, "GET", ts.URL+diskID+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	disk := decodeJSON(t, resp)
	resp.Body.Close()

	diskProps, ok := disk["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected disk properties object, got %T", disk["properties"])
	}
	if diskProps["managedBy"] != vmID {
		t.Fatalf("expected disk managedBy %q, got %v", vmID, diskProps["managedBy"])
	}
	if diskProps["diskState"] != "Attached" {
		t.Fatalf("expected diskState Attached, got %v", diskProps["diskState"])
	}
	if diskProps["osType"] != "Linux" {
		t.Fatalf("expected disk osType Linux, got %v", diskProps["osType"])
	}
	sku, ok := disk["sku"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected disk sku object, got %T", disk["sku"])
	}
	if sku["name"] != "StandardSSD_LRS" {
		t.Fatalf("expected disk sku.name StandardSSD_LRS, got %v", sku["name"])
	}

	// Deleting the VM detaches the disk but leaves it in place.
	resp = doRequest(t, "DELETE", vmURL(ts, "sub1", "rg1", "vm")+vmAV, "")
	expectStatus(t, resp, 204)
	resp.Body.Close()

	resp = doRequest(t, "GET", ts.URL+diskID+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	disk = decodeJSON(t, resp)
	resp.Body.Close()
	diskProps = disk["properties"].(map[string]interface{})
	if diskProps["managedBy"] != "" {
		t.Fatalf("expected detached disk managedBy empty, got %v", diskProps["managedBy"])
	}
	if diskProps["diskState"] != "Unattached" {
		t.Fatalf("expected diskState Unattached after VM delete, got %v", diskProps["diskState"])
	}

	// The provider's delete_os_disk_on_deletion feature can now delete it.
	resp = doRequest(t, "DELETE", ts.URL+diskID+diskAPIVersion, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()
}

// TestVMOSDiskDefaults verifies the documented defaults for a generated OS
// disk: 30 GiB, Standard_LRS and createOption FromImage.
func TestVMOSDiskDefaults(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "plain")+vmAV, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	m := decodeJSON(t, resp)
	resp.Body.Close()

	props := m["properties"].(map[string]interface{})
	osDisk, ok := props["storageProfile"].(map[string]interface{})["osDisk"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected generated storageProfile.osDisk, got %v", props["storageProfile"])
	}
	if name, _ := osDisk["name"].(string); !regexp.MustCompile(`^plain_OsDisk_1_[0-9a-f]{8}$`).MatchString(name) {
		t.Fatalf("expected generated OS disk name matching plain_OsDisk_1_<8 hex>, got %q", name)
	}
	if osDisk["diskSizeGB"] != float64(30) {
		t.Fatalf("expected default diskSizeGB 30, got %v", osDisk["diskSizeGB"])
	}
	if osDisk["createOption"] != "FromImage" {
		t.Fatalf("expected default createOption FromImage, got %v", osDisk["createOption"])
	}
	md, ok := osDisk["managedDisk"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected osDisk.managedDisk object, got %T", osDisk["managedDisk"])
	}
	if md["storageAccountType"] != "Standard_LRS" {
		t.Fatalf("expected default storageAccountType Standard_LRS, got %v", md["storageAccountType"])
	}
}

// TestVMOSDiskAttachExisting verifies that a VM can reference an existing
// managed disk: it is attached, not adopted as VM-owned, and only detached when
// the VM is deleted. A reference to a missing disk fails with 404 DiskNotFound.
func TestVMOSDiskAttachExisting(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	resp := doRequest(t, "PUT", diskBase(ts.URL)+"/existing"+diskAPIVersion,
		`{"location":"eastus","sku":{"name":"Premium_LRS"},"properties":{"diskSizeGB":128,"creationData":{"createOption":"Empty"}}}`)
	expectStatus(t, resp, 201)
	existing := decodeJSON(t, resp)
	resp.Body.Close()
	diskID, _ := existing["id"].(string)
	if diskID == "" {
		t.Fatal("expected pre-created disk id")
	}

	body := `{"location":"eastus","properties":{
		"storageProfile":{"osDisk":{"name":"existing","caching":"ReadOnly","managedDisk":{"id":"` + diskID + `"}}}
	}}`
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm2")+vmAV, body)
	expectStatus(t, resp, 201)
	m := decodeJSON(t, resp)
	resp.Body.Close()

	props := m["properties"].(map[string]interface{})
	vmID, _ := props["vmId"].(string)
	storage, ok := props["storageProfile"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected storageProfile object, got %T", props["storageProfile"])
	}
	osDisk, ok := storage["osDisk"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected storageProfile.osDisk object, got %T", storage["osDisk"])
	}
	if osDisk["name"] != "existing" {
		t.Fatalf("expected osDisk.name existing, got %v", osDisk["name"])
	}
	md, ok := osDisk["managedDisk"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected osDisk.managedDisk object, got %T", osDisk["managedDisk"])
	}
	if md["id"] != diskID {
		t.Fatalf("expected managedDisk.id %q, got %v", diskID, md["id"])
	}
	if md["storageAccountType"] != "Premium_LRS" {
		t.Fatalf("expected managedDisk.storageAccountType Premium_LRS, got %v", md["storageAccountType"])
	}
	if osDisk["diskSizeGB"] != float64(128) {
		t.Fatalf("expected diskSizeGB 128 from existing disk, got %v", osDisk["diskSizeGB"])
	}
	if osDisk["osType"] != "Linux" {
		t.Fatalf("expected osType Linux, got %v", osDisk["osType"])
	}
	if osDisk["caching"] != "ReadOnly" {
		t.Fatalf("expected caching ReadOnly, got %v", osDisk["caching"])
	}

	resp = doRequest(t, "GET", ts.URL+diskID+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	disk := decodeJSON(t, resp)
	resp.Body.Close()
	diskProps := disk["properties"].(map[string]interface{})
	if diskProps["managedBy"] != vmID {
		t.Fatalf("expected existing disk managedBy %q, got %v", vmID, diskProps["managedBy"])
	}
	if diskProps["diskState"] != "Attached" {
		t.Fatalf("expected diskState Attached, got %v", diskProps["diskState"])
	}

	// Deleting the VM only detaches the disk; it stays deletable afterwards,
	// which also proves it was not adopted (and removed) as a VM-owned disk.
	resp = doRequest(t, "DELETE", vmURL(ts, "sub1", "rg1", "vm2")+vmAV, "")
	expectStatus(t, resp, 204)
	resp.Body.Close()

	resp = doRequest(t, "GET", ts.URL+diskID+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	disk = decodeJSON(t, resp)
	resp.Body.Close()
	diskProps = disk["properties"].(map[string]interface{})
	if diskProps["managedBy"] != "" || diskProps["diskState"] != "Unattached" {
		t.Fatalf("expected existing disk detached after VM delete, got managedBy=%v diskState=%v",
			diskProps["managedBy"], diskProps["diskState"])
	}

	resp = doRequest(t, "DELETE", ts.URL+diskID+diskAPIVersion, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	// A missing explicit disk ID must fail the VM create with 404 DiskNotFound.
	missing := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/disks/ghost"
	body = `{"location":"eastus","properties":{"storageProfile":{"osDisk":{"managedDisk":{"id":"` + missing + `"}}}}}`
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm3")+vmAV, body)
	expectStatus(t, resp, 404)
	e := decodeError(t, resp)
	resp.Body.Close()
	if e.Error.Code != "DiskNotFound" {
		t.Fatalf("expected DiskNotFound, got %s", e.Error.Code)
	}
}

// TestVMOSDiskRejectsDiskAttachedToAnotherVM verifies that an explicit OS disk
// already attached to one VM cannot be attached to a second VM: the create is
// rejected with 409 DiskInUse, the second VM is not stored, and the disk stays
// attached to its original VM.
func TestVMOSDiskRejectsDiskAttachedToAnotherVM(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	resp := doRequest(t, "PUT", diskBase(ts.URL)+"/shared"+diskAPIVersion,
		`{"location":"eastus","sku":{"name":"Premium_LRS"},"properties":{"diskSizeGB":128,"creationData":{"createOption":"Empty"}}}`)
	expectStatus(t, resp, 201)
	disk := decodeJSON(t, resp)
	resp.Body.Close()
	diskID, _ := disk["id"].(string)
	if diskID == "" {
		t.Fatal("expected pre-created disk id")
	}

	body := `{"location":"eastus","properties":{
		"storageProfile":{"osDisk":{"managedDisk":{"id":"` + diskID + `"}}}
	}}`
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm-a")+vmAV, body)
	expectStatus(t, resp, 201)
	vmA := decodeJSON(t, resp)
	resp.Body.Close()
	vmAID := vmA["properties"].(map[string]interface{})["vmId"].(string)

	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm-b")+vmAV, body)
	expectStatus(t, resp, 409)
	e := decodeError(t, resp)
	resp.Body.Close()
	if e.Error.Code != "DiskInUse" {
		t.Fatalf("expected DiskInUse, got %s", e.Error.Code)
	}

	// The rejected VM must not exist.
	resp = doRequest(t, "GET", vmURL(ts, "sub1", "rg1", "vm-b")+vmAV, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()

	// The disk must still belong to the first VM.
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
}
