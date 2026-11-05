package tests

import (
	"testing"
)

func TestPublicIPFQDNFromDomainNameLabel(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/publicIPAddresses"
	av := "?api-version=2023-09-01"

	// Create a public IP with a DNS label. The FQDN should be derived from the
	// label and the normalized location ("East US" -> "eastus").
	resp := doRequest(t, "PUT", base+"/pip-dns"+av,
		`{"location":"East US","sku":{"name":"Standard","tier":"Regional"},"properties":{"publicIPAllocationMethod":"Static","dnsSettings":{"domainNameLabel":"myapp","reverseFqdn":"reverse.example.com","domainNameLabelScope":"TenantReuse"}}}`)
	expectStatus(t, resp, 201)
	m := decodeJSON(t, resp)
	resp.Body.Close()

	props := m["properties"].(map[string]interface{})
	dns, ok := props["dnsSettings"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected dnsSettings object, got %v", props["dnsSettings"])
	}
	if dns["fqdn"] != "myapp.eastus.cloudapp.azure.com" {
		t.Fatalf("expected derived fqdn, got %v", dns["fqdn"])
	}
	if dns["domainNameLabel"] != "myapp" {
		t.Fatalf("expected domainNameLabel=myapp, got %v", dns["domainNameLabel"])
	}
	if dns["reverseFqdn"] != "reverse.example.com" {
		t.Fatalf("expected reverseFqdn preserved, got %v", dns["reverseFqdn"])
	}
	if dns["domainNameLabelScope"] != "TenantReuse" {
		t.Fatalf("expected domainNameLabelScope preserved, got %v", dns["domainNameLabelScope"])
	}

	// Replacing the resource without dnsSettings should remove the FQDN.
	resp = doRequest(t, "PUT", base+"/pip-dns"+av,
		`{"location":"East US","sku":{"name":"Standard","tier":"Regional"},"properties":{"publicIPAllocationMethod":"Static"}}`)
	expectStatus(t, resp, 200)
	m = decodeJSON(t, resp)
	resp.Body.Close()

	props = m["properties"].(map[string]interface{})
	if dns, ok := props["dnsSettings"]; ok && dns != nil {
		t.Fatalf("expected dnsSettings to be removed after PUT without it, got %v", dns)
	}
}
