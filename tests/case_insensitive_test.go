package tests

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestVMOSDiskCaseInsensitiveReference verifies that an explicit OS disk ID is
// resolved case-insensitively, both when referenced from a VM and when the disk
// is read back with a differently cased name.
func TestVMOSDiskCaseInsensitiveReference(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	diskID := createManagedDisk(t, ts, "existing")
	mixedID := strings.ToUpper(diskID)

	body := `{"location":"eastus","properties":{
		"storageProfile":{"osDisk":{"managedDisk":{"id":"` + mixedID + `"}}}
	}}`
	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm")+vmAV, body)
	expectStatus(t, resp, 201)
	vm := decodeJSON(t, resp)
	resp.Body.Close()
	vmID := vm["properties"].(map[string]interface{})["vmId"].(string)

	// Reading the canonical disk with a case-only name still resolves it.
	resp = doRequest(t, "GET", diskBase(ts.URL)+"/EXISTING"+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	disk := decodeJSON(t, resp)
	resp.Body.Close()
	diskProps := disk["properties"].(map[string]interface{})
	if diskProps["managedBy"] != vmID || diskProps["diskState"] != "Attached" {
		t.Fatalf("expected case-insensitive OS disk to be attached, got managedBy=%v diskState=%v",
			diskProps["managedBy"], diskProps["diskState"])
	}
}

// TestVMOSDiskCaseOnlyFullPUT verifies that a full-model PUT whose OS disk ID
// only changes case is not treated as an OS-disk replacement.
func TestVMOSDiskCaseOnlyFullPUT(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm")+vmAV, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	created := decodeJSON(t, resp)
	resp.Body.Close()
	vmID := created["properties"].(map[string]interface{})["vmId"].(string)
	osDiskID := osDiskManagedID(t, created)

	model := getVMModel(t, ts, "sub1", "rg1", "vm")
	osDisk := vmStorage(model)["osDisk"].(map[string]interface{})
	osDisk["managedDisk"].(map[string]interface{})["id"] = strings.ToUpper(osDiskID)
	raw, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm")+vmAV, string(raw))
	expectStatus(t, resp, 200)
	resp.Body.Close()

	resp = doRequest(t, "GET", ts.URL+osDiskID+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	disk := decodeJSON(t, resp)
	resp.Body.Close()
	diskProps := disk["properties"].(map[string]interface{})
	if diskProps["managedBy"] != vmID || diskProps["diskState"] != "Attached" {
		t.Fatalf("expected OS disk to stay attached after case-only PUT, got managedBy=%v diskState=%v",
			diskProps["managedBy"], diskProps["diskState"])
	}

	if disks := listDisks(t, ts); len(disks) != 1 {
		t.Fatalf("expected exactly 1 disk after case-only OS disk PUT, got %d: %v", len(disks), disks)
	}
}

// TestVMDataDiskCaseOnlyFullPUT verifies that a case-only data-disk reference
// neither creates a duplicate disk record nor desynchronises the attachment
// during the provider's GET-then-PUT full-model update.
func TestVMDataDiskCaseOnlyFullPUT(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	dataDiskID := createManagedDisk(t, ts, "data1")

	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm")+vmAV, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	created := decodeJSON(t, resp)
	resp.Body.Close()
	vmID := created["properties"].(map[string]interface{})["vmId"].(string)

	model := getVMModel(t, ts, "sub1", "rg1", "vm")
	vmStorage(model)["dataDisks"] = []interface{}{
		dataDiskAttachment("data1", strings.ToUpper(dataDiskID)),
	}
	raw, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm")+vmAV, string(raw))
	expectStatus(t, resp, 200)
	resp.Body.Close()

	// The original record stays attached instead of being detached after a new
	// case-variant record was attached.
	resp = doRequest(t, "GET", ts.URL+dataDiskID+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	disk := decodeJSON(t, resp)
	resp.Body.Close()
	diskProps := disk["properties"].(map[string]interface{})
	if diskProps["managedBy"] != vmID || diskProps["diskState"] != "Attached" {
		t.Fatalf("expected data disk to stay attached after case-only PUT, got managedBy=%v diskState=%v",
			diskProps["managedBy"], diskProps["diskState"])
	}

	// One generated OS disk plus one data disk, not a duplicate case variant.
	if disks := listDisks(t, ts); len(disks) != 2 {
		t.Fatalf("expected exactly 2 disk records after case-only data disk PUT, got %d: %v", len(disks), disks)
	}
}

// TestVMNICCaseInsensitiveLink verifies that a VM referencing a NIC with
// different casing still links and unlinks the existing NIC record.
func TestVMNICCaseInsensitiveLink(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	netBase, subnetID := createNICFixture(t, ts.URL)
	nicBase := netBase + "/networkInterfaces"
	resp := doRequest(t, "PUT", nicBase+"/nic1"+nicAPIVersion,
		`{"location":"eastus","properties":{"ipConfigurations":[{"name":"ipconfig1","properties":{"subnet":{"id":"`+subnetID+`"}}}]}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	mixedID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/NIC1"
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm")+vmAV,
		`{"location":"eastus","properties":{"networkProfile":{"networkInterfaces":[{"id":"`+mixedID+`"}]}}}`)
	expectStatus(t, resp, 201)
	vm := decodeJSON(t, resp)
	resp.Body.Close()
	vmID := vm["id"].(string)

	if got := nicVirtualMachineID(t, ts.URL, "nic1"); got != vmID {
		t.Fatalf("expected case-insensitive NIC reference to link nic1, got virtualMachine.id %q", got)
	}

	resp = doRequest(t, "PATCH", vmURL(ts, "sub1", "rg1", "vm")+vmAV,
		`{"properties":{"networkProfile":{"networkInterfaces":[]}}}`)
	expectStatus(t, resp, 200)
	resp.Body.Close()

	if got := nicVirtualMachineID(t, ts.URL, "nic1"); got != "" {
		t.Fatalf("expected case-insensitive NIC reference to unlink nic1, got virtualMachine.id %q", got)
	}
}
