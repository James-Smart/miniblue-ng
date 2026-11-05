package tests

import "testing"

// nicIPs returns the private addresses of a NIC response in configuration order.
func nicIPs(t *testing.T, nic map[string]interface{}) []string {
	t.Helper()
	props, ok := nic["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected NIC properties, got %T", nic["properties"])
	}
	configs, ok := props["ipConfigurations"].([]interface{})
	if !ok {
		t.Fatalf("expected NIC ipConfigurations, got %T", props["ipConfigurations"])
	}
	ips := make([]string, 0, len(configs))
	for _, raw := range configs {
		cfg, _ := raw.(map[string]interface{})
		cfgProps, _ := cfg["properties"].(map[string]interface{})
		ip, _ := cfgProps["privateIPAddress"].(string)
		ips = append(ips, ip)
	}
	return ips
}

// TestNetworkInterfaceMultipleDynamicIPs verifies that two dynamic
// configurations on one NIC receive distinct addresses.
func TestNetworkInterfaceMultipleDynamicIPs(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	netBase, subnetID := createNICFixture(t, ts.URL)
	body := `{"location":"eastus","properties":{"ipConfigurations":[
		{"name":"ipconfig1","properties":{"subnet":{"id":"` + subnetID + `"}}},
		{"name":"ipconfig2","properties":{"subnet":{"id":"` + subnetID + `"}}}
	]}}`
	resp := doRequest(t, "PUT", netBase+"/networkInterfaces/nic-multi"+nicAPIVersion, body)
	expectStatus(t, resp, 201)
	nic := decodeJSON(t, resp)
	resp.Body.Close()

	ips := nicIPs(t, nic)
	if len(ips) != 2 {
		t.Fatalf("expected 2 IP configurations, got %v", ips)
	}
	if ips[0] != "10.0.1.4" || ips[1] != "10.0.1.5" {
		t.Fatalf("expected distinct addresses 10.0.1.4 and 10.0.1.5, got %v", ips)
	}
}

// TestNetworkInterfaceStaticDoesNotCollideWithDynamic verifies that a static
// address is reserved before dynamic allocation, even when the dynamic config
// comes first in the request.
func TestNetworkInterfaceStaticDoesNotCollideWithDynamic(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	netBase, subnetID := createNICFixture(t, ts.URL)
	body := `{"location":"eastus","properties":{"ipConfigurations":[
		{"name":"ipconfig1","properties":{"subnet":{"id":"` + subnetID + `"}}},
		{"name":"ipconfig2","properties":{
			"privateIPAllocationMethod":"Static",
			"privateIPAddress":"10.0.1.4",
			"subnet":{"id":"` + subnetID + `"}}}
	]}}`
	resp := doRequest(t, "PUT", netBase+"/networkInterfaces/nic-mixed"+nicAPIVersion, body)
	expectStatus(t, resp, 201)
	nic := decodeJSON(t, resp)
	resp.Body.Close()

	ips := nicIPs(t, nic)
	if len(ips) != 2 {
		t.Fatalf("expected 2 IP configurations, got %v", ips)
	}
	if ips[1] != "10.0.1.4" {
		t.Fatalf("expected static address 10.0.1.4 to be preserved, got %v", ips)
	}
	if ips[0] == "" || ips[0] == ips[1] {
		t.Fatalf("expected dynamic address distinct from the static address, got %v", ips)
	}
	if ips[0] != "10.0.1.5" {
		t.Fatalf("expected dynamic address to skip the reserved static address, got %v", ips)
	}
}
