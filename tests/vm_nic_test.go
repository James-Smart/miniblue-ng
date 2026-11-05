package tests

import "testing"

// nicVirtualMachineID GETs a NIC and returns properties.virtualMachine.id, or ""
// when the NIC is not linked to a virtual machine.
func nicVirtualMachineID(t *testing.T, tsURL, name string) string {
	t.Helper()
	url := tsURL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/" + name + nicAPIVersion
	resp := doRequest(t, "GET", url, "")
	expectStatus(t, resp, 200)
	nic := decodeJSON(t, resp)
	resp.Body.Close()

	props, _ := nic["properties"].(map[string]interface{})
	vm, _ := props["virtualMachine"].(map[string]interface{})
	id, _ := vm["id"].(string)
	return id
}

// TestVMNICLinkLifecycle verifies that a VM links the NICs referenced by its
// networkProfile, moves the link when the profile changes, and clears the links
// when the VM is deleted.
func TestVMNICLinkLifecycle(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	netBase, subnetID := createNICFixture(t, ts.URL)
	nicBase := netBase + "/networkInterfaces"

	for _, name := range []string{"nic1", "nic2"} {
		body := `{"location":"eastus","properties":{"ipConfigurations":[{"name":"ipconfig1","properties":{"subnet":{"id":"` + subnetID + `"}}}]}}`
		resp := doRequest(t, "PUT", nicBase+"/"+name+nicAPIVersion, body)
		expectStatus(t, resp, 201)
		resp.Body.Close()
	}

	nicID := func(name string) string {
		return "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/" + name
	}

	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm")+vmAV,
		`{"location":"eastus","properties":{"networkProfile":{"networkInterfaces":[{"id":"`+nicID("nic1")+`"}]}}}`)
	expectStatus(t, resp, 201)
	vm := decodeJSON(t, resp)
	resp.Body.Close()

	vmID, _ := vm["id"].(string)
	if vmID == "" {
		t.Fatal("expected the VM response to include an id")
	}

	if got := nicVirtualMachineID(t, ts.URL, "nic1"); got != vmID {
		t.Fatalf("expected nic1 virtualMachine.id %q, got %q", vmID, got)
	}

	// Replacing the profile detaches the old NIC and attaches the new one.
	resp = doRequest(t, "PATCH", vmURL(ts, "sub1", "rg1", "vm")+vmAV,
		`{"properties":{"networkProfile":{"networkInterfaces":[{"id":"`+nicID("nic2")+`"}]}}}`)
	expectStatus(t, resp, 200)
	resp.Body.Close()

	if got := nicVirtualMachineID(t, ts.URL, "nic1"); got != "" {
		t.Fatalf("expected nic1 to be unlinked, got virtualMachine.id %q", got)
	}
	if got := nicVirtualMachineID(t, ts.URL, "nic2"); got != vmID {
		t.Fatalf("expected nic2 virtualMachine.id %q, got %q", vmID, got)
	}

	// Deleting the VM clears every remaining link.
	resp = doRequest(t, "DELETE", vmURL(ts, "sub1", "rg1", "vm")+vmAV, "")
	expectStatus(t, resp, 204)
	resp.Body.Close()

	if got := nicVirtualMachineID(t, ts.URL, "nic2"); got != "" {
		t.Fatalf("expected nic2 to be unlinked after VM delete, got virtualMachine.id %q", got)
	}
}
