package tests

import (
	"encoding/json"
	"testing"
)

const nicAPIVersion = "?api-version=2023-09-01"

// createNICFixture creates the VNet and subnet used by the NIC tests and
// returns the network base URL plus the subnet ID.
func createNICFixture(t *testing.T, tsURL string) (netBase, subnetID string) {
	t.Helper()

	netBase = tsURL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network"
	vnetBase := netBase + "/virtualNetworks"

	resp := doRequest(t, "PUT", vnetBase+"/vnet1"+nicAPIVersion,
		`{"location":"eastus","properties":{"addressSpace":{"addressPrefixes":["10.0.0.0/16"]}}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "PUT", vnetBase+"/vnet1/subnets/web"+nicAPIVersion,
		`{"properties":{"addressPrefix":"10.0.1.0/24"}}`)
	expectStatus(t, resp, 201)
	subnet := decodeJSON(t, resp)
	resp.Body.Close()

	subnetID, _ = subnet["id"].(string)
	if subnetID == "" {
		t.Fatal("expected subnet response to include an id")
	}
	return netBase, subnetID
}

func TestNetworkInterfaceLifecycle(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	netBase, subnetID := createNICFixture(t, ts.URL)
	nicBase := netBase + "/networkInterfaces"
	pipID := netBase + "/publicIPAddresses/pip1"
	nsgID := netBase + "/networkSecurityGroups/nsg1"
	lbPoolID := netBase + "/loadBalancers/lb1/backendAddressPools/pool1"

	nicBody := `{
		"location": "eastus",
		"properties": {
			"ipConfigurations": [{
				"name": "ipconfig1",
				"properties": {
					"subnet": {"id": "` + subnetID + `"},
					"publicIPAddress": {"id": "` + pipID + `"},
					"loadBalancerBackendAddressPools": [{"id": "` + lbPoolID + `"}]
				}
			}],
			"networkSecurityGroup": {"id": "` + nsgID + `"}
		}
	}`

	resp := doRequest(t, "PUT", nicBase+"/nic1"+nicAPIVersion, nicBody)
	expectStatus(t, resp, 201)
	nic := decodeJSON(t, resp)
	resp.Body.Close()

	props, ok := nic["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %T", nic["properties"])
	}
	ips, ok := props["ipConfigurations"].([]interface{})
	if !ok || len(ips) != 1 {
		t.Fatalf("expected 1 ipConfiguration, got %v", props["ipConfigurations"])
	}
	ip := ips[0].(map[string]interface{})
	ipProps, ok := ip["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected ipConfiguration properties, got %T", ip["properties"])
	}

	if ipProps["privateIPAddress"] != "10.0.1.4" {
		t.Fatalf("expected privateIPAddress=10.0.1.4, got %v", ipProps["privateIPAddress"])
	}
	if ipProps["privateIPAllocationMethod"] != "Dynamic" {
		t.Fatalf("expected Dynamic allocation, got %v", ipProps["privateIPAllocationMethod"])
	}
	if ipProps["privateIPAddressVersion"] != "IPv4" {
		t.Fatalf("expected IPv4, got %v", ipProps["privateIPAddressVersion"])
	}
	subnet, ok := ipProps["subnet"].(map[string]interface{})
	if !ok || subnet["id"] != subnetID {
		t.Fatalf("expected subnet id=%s, got %v", subnetID, ipProps["subnet"])
	}
	pip, ok := ipProps["publicIPAddress"].(map[string]interface{})
	if !ok || pip["id"] != pipID {
		t.Fatalf("expected publicIPAddress id=%s, got %v", pipID, ipProps["publicIPAddress"])
	}
	nsg, ok := props["networkSecurityGroup"].(map[string]interface{})
	if !ok || nsg["id"] != nsgID {
		t.Fatalf("expected networkSecurityGroup id=%s, got %v", nsgID, props["networkSecurityGroup"])
	}
	pools, ok := ipProps["loadBalancerBackendAddressPools"].([]interface{})
	if !ok || len(pools) != 1 {
		t.Fatalf("expected 1 loadBalancerBackendAddressPool, got %v", ipProps["loadBalancerBackendAddressPools"])
	}
	if props["macAddress"] == "" || props["macAddress"] == nil {
		t.Fatalf("expected a macAddress, got %v", props["macAddress"])
	}
	dns, ok := props["dnsSettings"].(map[string]interface{})
	if !ok || dns["internalDomainNameSuffix"] == "" || dns["internalDomainNameSuffix"] == nil {
		t.Fatalf("expected dnsSettings.internalDomainNameSuffix, got %v", props["dnsSettings"])
	}

	firstIP := ipProps["privateIPAddress"].(string)
	firstMAC := props["macAddress"].(string)

	// A second NIC on the same subnet must get a different address.
	resp = doRequest(t, "PUT", nicBase+"/nic2"+nicAPIVersion,
		`{"location":"eastus","properties":{"ipConfigurations":[{"name":"ipconfig1","properties":{"subnet":{"id":"`+subnetID+`"}}}]}}`)
	expectStatus(t, resp, 201)
	second := decodeJSON(t, resp)
	resp.Body.Close()

	secondProps := second["properties"].(map[string]interface{})
	secondIP := secondProps["ipConfigurations"].([]interface{})[0].(map[string]interface{})["properties"].(map[string]interface{})["privateIPAddress"]
	if secondIP == firstIP {
		t.Fatalf("second NIC reused address %s", firstIP)
	}

	// List in resource group and subscription.
	resp = doRequest(t, "GET", nicBase+nicAPIVersion, "")
	expectStatus(t, resp, 200)
	list := decodeJSON(t, resp)
	resp.Body.Close()
	if items, ok := list["value"].([]interface{}); !ok || len(items) != 2 {
		t.Fatalf("expected 2 NICs in resource group list, got %v", list["value"])
	}

	resp = doRequest(t, "GET", ts.URL+"/subscriptions/sub1/providers/Microsoft.Network/networkInterfaces"+nicAPIVersion, "")
	expectStatus(t, resp, 200)
	list = decodeJSON(t, resp)
	resp.Body.Close()
	if items, ok := list["value"].([]interface{}); !ok || len(items) != 2 {
		t.Fatalf("expected 2 NICs in subscription list, got %v", list["value"])
	}

	// GET must be stable.
	resp = doRequest(t, "GET", nicBase+"/nic1"+nicAPIVersion, "")
	expectStatus(t, resp, 200)
	got := decodeJSON(t, resp)
	resp.Body.Close()
	gotProps := got["properties"].(map[string]interface{})
	gotIP := gotProps["ipConfigurations"].([]interface{})[0].(map[string]interface{})["properties"].(map[string]interface{})["privateIPAddress"]
	if gotIP != firstIP {
		t.Fatalf("GET changed private IP: got %v, want %s", gotIP, firstIP)
	}
	if gotProps["macAddress"] != firstMAC {
		t.Fatalf("GET changed MAC: got %v, want %s", gotProps["macAddress"], firstMAC)
	}

	// PATCH tags must succeed and keep the allocated IP.
	resp = doRequest(t, "PATCH", nicBase+"/nic1"+nicAPIVersion, `{"tags":{"env":"test"}}`)
	expectStatus(t, resp, 200)
	patched := decodeJSON(t, resp)
	resp.Body.Close()
	if tags, ok := patched["tags"].(map[string]interface{}); !ok || tags["env"] != "test" {
		t.Fatalf("expected tags.env=test, got %v", patched["tags"])
	}
	patchedProps := patched["properties"].(map[string]interface{})
	patchedIP := patchedProps["ipConfigurations"].([]interface{})[0].(map[string]interface{})["properties"].(map[string]interface{})["privateIPAddress"]
	if patchedIP != firstIP {
		t.Fatalf("PATCH changed private IP: got %v, want %s", patchedIP, firstIP)
	}

	// DELETE then GET -> 404.
	resp = doRequest(t, "DELETE", nicBase+"/nic1"+nicAPIVersion, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	resp = doRequest(t, "GET", nicBase+"/nic1"+nicAPIVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
}

func TestNetworkInterfacePUTPreservesGeneratedFields(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	netBase, subnetID := createNICFixture(t, ts.URL)
	nicBase := netBase + "/networkInterfaces"

	resp := doRequest(t, "PUT", nicBase+"/nic1"+nicAPIVersion,
		`{"location":"eastus","properties":{"ipConfigurations":[{"name":"ipconfig1","properties":{"subnet":{"id":"`+subnetID+`"}}}]}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "GET", nicBase+"/nic1"+nicAPIVersion, "")
	expectStatus(t, resp, 200)
	before := decodeJSON(t, resp)
	resp.Body.Close()

	// Change only tags and PUT the whole model back, as the provider does.
	before["tags"] = map[string]interface{}{"env": "updated"}
	body, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}

	resp = doRequest(t, "PUT", nicBase+"/nic1"+nicAPIVersion, string(body))
	expectStatus(t, resp, 200)
	after := decodeJSON(t, resp)
	resp.Body.Close()

	beforeProps := before["properties"].(map[string]interface{})
	afterProps := after["properties"].(map[string]interface{})

	if afterProps["resourceGuid"] != beforeProps["resourceGuid"] {
		t.Fatalf("resourceGuid changed: got %v, want %v", afterProps["resourceGuid"], beforeProps["resourceGuid"])
	}
	if afterProps["macAddress"] != beforeProps["macAddress"] {
		t.Fatalf("macAddress changed: got %v, want %v", afterProps["macAddress"], beforeProps["macAddress"])
	}

	beforeDNS := beforeProps["dnsSettings"].(map[string]interface{})
	afterDNS := afterProps["dnsSettings"].(map[string]interface{})
	if afterDNS["internalDomainNameSuffix"] != beforeDNS["internalDomainNameSuffix"] {
		t.Fatalf("internalDomainNameSuffix changed: got %v, want %v", afterDNS["internalDomainNameSuffix"], beforeDNS["internalDomainNameSuffix"])
	}

	beforeIP := beforeProps["ipConfigurations"].([]interface{})[0].(map[string]interface{})
	afterIP := afterProps["ipConfigurations"].([]interface{})[0].(map[string]interface{})
	if afterIP["id"] != beforeIP["id"] {
		t.Fatalf("ipConfiguration id changed: got %v, want %v", afterIP["id"], beforeIP["id"])
	}

	beforeIPProps := beforeIP["properties"].(map[string]interface{})
	afterIPProps := afterIP["properties"].(map[string]interface{})
	if afterIPProps["privateIPAddress"] != beforeIPProps["privateIPAddress"] {
		t.Fatalf("privateIPAddress changed: got %v, want %v", afterIPProps["privateIPAddress"], beforeIPProps["privateIPAddress"])
	}

	if tags, ok := after["tags"].(map[string]interface{}); !ok || tags["env"] != "updated" {
		t.Fatalf("expected tags.env=updated, got %v", after["tags"])
	}
}
