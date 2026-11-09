package tests

import (
	"slices"
	"testing"
)

var azureNameServers = []string{
	"ns1-01.azure-dns.com.",
	"ns2-01.azure-dns.net.",
	"ns3-01.azure-dns.org.",
	"ns4-01.azure-dns.info.",
}

func assertDNSNameServers(t *testing.T, zone map[string]interface{}) {
	t.Helper()
	props, ok := zone["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("zone response has no properties: %v", zone)
	}
	raw, ok := props["nameServers"].([]interface{})
	if !ok {
		t.Fatalf("zone properties have no nameServers: %v", props)
	}
	got := make([]string, len(raw))
	for i, v := range raw {
		got[i], _ = v.(string)
	}
	if !slices.Equal(got, azureNameServers) {
		t.Fatalf("expected nameServers %v, got %v", azureNameServers, got)
	}
}

func TestDNSZoneAndRecord(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/dnsZones"
	av := "?api-version=2023-07-01-preview"

	// Create zone
	resp := doRequest(t, "PUT", base+"/example.com"+av, `{"location":"global"}`)
	resp.Body.Close()
	expectStatus(t, resp, 201)

	// Create A record
	resp = doRequest(t, "PUT", base+"/example.com/A/www"+av,
		`{"properties":{"TTL":300,"ARecords":[{"ipv4Address":"1.2.3.4"}]}}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 201)

	// Delete zone cascades records
	doRequest(t, "DELETE", base+"/example.com"+av, "").Body.Close()
	resp = doRequest(t, "GET", base+"/example.com/A/www"+av, "")
	defer resp.Body.Close()
	expectStatus(t, resp, 404)
}

func TestDNSRecordOnNonexistentZone(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/dnsZones"

	resp := doRequest(t, "PUT", base+"/nope.com/A/www?api-version=2023-07-01",
		`{"properties":{"TTL":60}}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 404)
}

func TestDNSZoneCreateHasFourNameServers(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/dnsZones"

	resp := doRequest(t, "PUT", base+"/example.com?api-version=2023-07-01", `{"location":"global"}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 201)

	assertDNSNameServers(t, decodeJSON(t, resp))
}

func TestDNSZoneGetHasFourNameServers(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/dnsZones"

	createResp := doRequest(t, "PUT", base+"/example.com?api-version=2023-07-01", `{"location":"global"}`)
	createResp.Body.Close()
	expectStatus(t, createResp, 201)

	getResp := doRequest(t, "GET", base+"/example.com?api-version=2023-07-01", "")
	defer getResp.Body.Close()
	expectStatus(t, getResp, 200)

	assertDNSNameServers(t, decodeJSON(t, getResp))
}

func TestDNSZoneListHasFourNameServers(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/dnsZones"

	createResp := doRequest(t, "PUT", base+"/example.com?api-version=2023-07-01", `{"location":"global"}`)
	createResp.Body.Close()
	expectStatus(t, createResp, 201)

	listResp := doRequest(t, "GET", base+"?api-version=2023-07-01", "")
	defer listResp.Body.Close()
	expectStatus(t, listResp, 200)

	list := decodeJSON(t, listResp)
	items, ok := list["value"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 zone, got %v", list["value"])
	}
	zone, ok := items[0].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected zone shape: %v", items[0])
	}
	assertDNSNameServers(t, zone)
}

func TestDNSZoneApexNSRecordMatchesNameServers(t *testing.T) {
	ts := setupServer()
	defer ts.Close()
	base := ts.URL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/dnsZones"

	createResp := doRequest(t, "PUT", base+"/example.com?api-version=2023-07-01", `{"location":"global"}`)
	createResp.Body.Close()
	expectStatus(t, createResp, 201)

	getResp := doRequest(t, "GET", base+"/example.com/NS/@?api-version=2023-07-01", "")
	defer getResp.Body.Close()
	expectStatus(t, getResp, 200)

	recordSet := decodeJSON(t, getResp)
	props, ok := recordSet["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("NS record set has no properties: %v", recordSet)
	}
	raw, ok := props["NSRecords"].([]interface{})
	if !ok {
		t.Fatalf("NS record set has no NSRecords: %v", props)
	}
	got := make([]string, len(raw))
	for i, v := range raw {
		record, _ := v.(map[string]interface{})
		got[i], _ = record["nsdname"].(string)
	}
	if !slices.Equal(got, azureNameServers) {
		t.Fatalf("expected NSRecords %v, got %v", azureNameServers, got)
	}
}
