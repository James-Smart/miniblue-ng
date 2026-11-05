package tests

import "testing"

// TestVMGapsResourceGroupCascade verifies that deleting a resource group
// removes every resource introduced by the VM gaps work: NICs, managed disks,
// SSH keys, VMs and VM extensions. It also checks the health endpoint advertises
// the new disks and sshkeys services.
func TestVMGapsResourceGroupCascade(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	netBase, subnetID := createNICFixture(t, ts.URL)
	nicBase := netBase + "/networkInterfaces"
	resp := doRequest(t, "PUT", nicBase+"/nic1"+nicAPIVersion,
		`{"location":"eastus","properties":{"ipConfigurations":[{"name":"ipconfig1","properties":{"subnet":{"id":"`+subnetID+`"}}}]}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "PUT", diskBase(ts.URL)+"/disk1"+diskAPIVersion,
		`{"location":"eastus","sku":{"name":"Premium_LRS"},"properties":{"diskSizeGB":64,"creationData":{"createOption":"Empty"}}}`)
	expectStatus(t, resp, 201)
	disk := decodeJSON(t, resp)
	resp.Body.Close()
	diskID, _ := disk["id"].(string)
	if diskID == "" {
		t.Fatal("expected the disk response to include an id")
	}

	sshBase := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/sshPublicKeys"
	resp = doRequest(t, "PUT", sshBase+"/key1"+sshKeyAPIVersion, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	nicID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1"
	vmBody := `{"location":"eastus","properties":{
		"networkProfile":{"networkInterfaces":[{"id":"` + nicID + `"}]},
		"storageProfile":{"osDisk":{"managedDisk":{"id":"` + diskID + `"}}}
	}}`
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm1")+vmAV, vmBody)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "PUT", extensionURL(ts, "sub1", "rg1", "vm1", "ext1"),
		`{"location":"eastus","properties":{"publisher":"Microsoft.Azure.Extensions","type":"CustomScript","typeHandlerVersion":"2.0"}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "DELETE", ts.URL+"/subscriptions/sub1/resourcegroups/rg1"+vmAV, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	checks := []struct {
		name string
		path string
	}{
		{"NIC", nicBase + "/nic1" + nicAPIVersion},
		{"disk", ts.URL + diskID + diskAPIVersion},
		{"SSH key", sshBase + "/key1" + sshKeyAPIVersion},
		{"VM", vmURL(ts, "sub1", "rg1", "vm1") + vmAV},
		{"extension", extensionURL(ts, "sub1", "rg1", "vm1", "ext1")},
	}
	for _, c := range checks {
		resp := doRequest(t, "GET", c.path, "")
		if resp.StatusCode != 404 {
			t.Errorf("%s should be 404 after RG delete, got %d", c.name, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// Recreating the VM name must not resurrect a leaked extension record:
	// extension GETs are only observable while the parent VM exists.
	createRG(t, ts, "sub1", "rg1")
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm1")+vmAV, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()
	resp = doRequest(t, "GET", extensionURL(ts, "sub1", "rg1", "vm1", "ext1"), "")
	if resp.StatusCode != 404 {
		t.Errorf("extension record leaked past RG delete: got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Health advertises the new services and keeps its count consistent.
	resp = doRequest(t, "GET", ts.URL+"/health", "")
	expectStatus(t, resp, 200)
	health := decodeJSON(t, resp)
	resp.Body.Close()

	rawServices, ok := health["services"].([]interface{})
	if !ok {
		t.Fatalf("expected health.services array, got %T", health["services"])
	}
	seen := make(map[string]bool, len(rawServices))
	for _, raw := range rawServices {
		if name, ok := raw.(string); ok {
			seen[name] = true
		}
	}
	for _, want := range []string{"disks", "sshkeys"} {
		if !seen[want] {
			t.Errorf("health services missing %q: %v", want, rawServices)
		}
	}
	count, _ := health["service_count"].(float64)
	if int(count) != len(rawServices) {
		t.Errorf("service_count %v does not match %d advertised services", health["service_count"], len(rawServices))
	}
	if int(count) < 2 {
		t.Errorf("expected service_count to include the new services, got %v", health["service_count"])
	}
}

// TestVMGapsVMDeleteCascade verifies that deleting a VM removes its extension
// children and detaches (but does not delete) its NIC and managed disks.
func TestVMGapsVMDeleteCascade(t *testing.T) {
	ts := setupVMServer(t)
	defer ts.Close()
	createRG(t, ts, "sub1", "rg1")

	netBase, subnetID := createNICFixture(t, ts.URL)
	nicBase := netBase + "/networkInterfaces"
	resp := doRequest(t, "PUT", nicBase+"/nic1"+nicAPIVersion,
		`{"location":"eastus","properties":{"ipConfigurations":[{"name":"ipconfig1","properties":{"subnet":{"id":"`+subnetID+`"}}}]}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "PUT", diskBase(ts.URL)+"/disk1"+diskAPIVersion,
		`{"location":"eastus","sku":{"name":"StandardSSD_LRS"},"properties":{"diskSizeGB":64,"creationData":{"createOption":"Empty"}}}`)
	expectStatus(t, resp, 201)
	disk := decodeJSON(t, resp)
	resp.Body.Close()
	diskID, _ := disk["id"].(string)
	if diskID == "" {
		t.Fatal("expected the disk response to include an id")
	}

	nicID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/networkInterfaces/nic1"
	vmBody := `{"location":"eastus","properties":{
		"networkProfile":{"networkInterfaces":[{"id":"` + nicID + `"}]},
		"storageProfile":{"osDisk":{"managedDisk":{"id":"` + diskID + `"}}}
	}}`
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm1")+vmAV, vmBody)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "PUT", extensionURL(ts, "sub1", "rg1", "vm1", "ext1"),
		`{"location":"eastus","properties":{"publisher":"Microsoft.Azure.Extensions","type":"CustomScript","typeHandlerVersion":"2.0"}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "DELETE", vmURL(ts, "sub1", "rg1", "vm1")+vmAV, "")
	expectStatus(t, resp, 204)
	resp.Body.Close()

	// The NIC record survives with its VM link cleared.
	resp = doRequest(t, "GET", nicBase+"/nic1"+nicAPIVersion, "")
	expectStatus(t, resp, 200)
	resp.Body.Close()
	if got := nicVirtualMachineID(t, ts.URL, "nic1"); got != "" {
		t.Errorf("expected nic1 to be detached after VM delete, got virtualMachine.id %q", got)
	}

	// The disk record survives, detached from the VM.
	resp = doRequest(t, "GET", ts.URL+diskID+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	disk = decodeJSON(t, resp)
	resp.Body.Close()
	diskProps, _ := disk["properties"].(map[string]interface{})
	if diskProps["managedBy"] != "" || diskProps["diskState"] != "Unattached" {
		t.Errorf("expected disk detached after VM delete, got managedBy=%v diskState=%v",
			diskProps["managedBy"], diskProps["diskState"])
	}

	// The extension is gone: recreating the VM name must not resurrect it.
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm1")+vmAV, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()
	resp = doRequest(t, "GET", extensionURL(ts, "sub1", "rg1", "vm1", "ext1"), "")
	if resp.StatusCode != 404 {
		t.Errorf("extension record survived VM delete: got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
