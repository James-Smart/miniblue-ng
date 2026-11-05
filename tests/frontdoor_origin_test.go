package tests

import (
	"encoding/json"
	"testing"
)

func TestFrontDoorOriginGroupAndOrigin(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	profileBase := frontDoorProfileBase(ts.URL)
	resp := doRequest(t, "PUT", profileBase+"/p1"+frontDoorAPIVersion, `{"location":"global"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	groups := profileBase + "/p1/originGroups"
	groupBody := `{
		"location": "global",
		"properties": {
			"healthProbeSettings": {
				"probeIntervalInSeconds": 240,
				"probePath": "/health",
				"probeProtocol": "Https",
				"probeRequestType": "GET"
			},
			"loadBalancingSettings": {
				"additionalLatencyInMilliseconds": 0,
				"sampleSize": 16,
				"successfulSamplesRequired": 3
			},
			"sessionAffinityState": "Enabled",
			"trafficRestorationTimeToHealedOrNewEndpointsInMinutes": 10
		}
	}`

	// Create the origin group and check every supplied field is echoed.
	resp = doRequest(t, "PUT", groups+"/g1"+frontDoorAPIVersion, groupBody)
	expectStatus(t, resp, 201)
	group := decodeJSON(t, resp)
	resp.Body.Close()

	wantGroupID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cdn/profiles/p1/originGroups/g1"
	if group["id"] != wantGroupID {
		t.Fatalf("expected origin group id=%s, got %v", wantGroupID, group["id"])
	}
	if group["type"] != "Microsoft.Cdn/profiles/originGroups" {
		t.Fatalf("expected origin group type, got %v", group["type"])
	}
	groupProps, ok := group["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected origin group properties object, got %T", group["properties"])
	}
	if groupProps["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState=Succeeded, got %v", groupProps["provisioningState"])
	}
	health, ok := groupProps["healthProbeSettings"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected healthProbeSettings object, got %T", groupProps["healthProbeSettings"])
	}
	if health["probeIntervalInSeconds"] != float64(240) ||
		health["probePath"] != "/health" ||
		health["probeProtocol"] != "Https" ||
		health["probeRequestType"] != "GET" {
		t.Fatalf("healthProbeSettings not echoed: %v", health)
	}
	loadBalancing, ok := groupProps["loadBalancingSettings"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected loadBalancingSettings object, got %T", groupProps["loadBalancingSettings"])
	}
	if loadBalancing["additionalLatencyInMilliseconds"] != float64(0) ||
		loadBalancing["sampleSize"] != float64(16) ||
		loadBalancing["successfulSamplesRequired"] != float64(3) {
		t.Fatalf("loadBalancingSettings not echoed: %v", loadBalancing)
	}
	if groupProps["sessionAffinityState"] != "Enabled" {
		t.Fatalf("expected sessionAffinityState=Enabled, got %v", groupProps["sessionAffinityState"])
	}
	if groupProps["trafficRestorationTimeToHealedOrNewEndpointsInMinutes"] != float64(10) {
		t.Fatalf("expected traffic restoration time=10, got %v", groupProps["trafficRestorationTimeToHealedOrNewEndpointsInMinutes"])
	}

	origins := groups + "/g1/origins"
	originBody := `{
		"location": "global",
		"properties": {
			"hostName": "backend.example.com",
			"originHostHeader": "backend.example.com",
			"httpPort": 80,
			"httpsPort": 443,
			"priority": 1,
			"weight": 500,
			"enabledState": "Enabled",
			"enforceCertificateNameCheck": true
		}
	}`

	// Create the origin and check every supplied field is echoed.
	resp = doRequest(t, "PUT", origins+"/o1"+frontDoorAPIVersion, originBody)
	expectStatus(t, resp, 201)
	origin := decodeJSON(t, resp)
	resp.Body.Close()

	wantOriginID := wantGroupID + "/origins/o1"
	if origin["id"] != wantOriginID {
		t.Fatalf("expected origin id=%s, got %v", wantOriginID, origin["id"])
	}
	if origin["type"] != "Microsoft.Cdn/profiles/originGroups/origins" {
		t.Fatalf("expected origin type, got %v", origin["type"])
	}
	originProps, ok := origin["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected origin properties object, got %T", origin["properties"])
	}
	if originProps["hostName"] != "backend.example.com" {
		t.Fatalf("expected hostName echoed, got %v", originProps["hostName"])
	}
	if originProps["originHostHeader"] != "backend.example.com" {
		t.Fatalf("expected originHostHeader echoed, got %v", originProps["originHostHeader"])
	}
	if originProps["httpPort"] != float64(80) || originProps["httpsPort"] != float64(443) {
		t.Fatalf("expected ports 80/443, got %v/%v", originProps["httpPort"], originProps["httpsPort"])
	}
	if originProps["priority"] != float64(1) || originProps["weight"] != float64(500) {
		t.Fatalf("expected priority=1 weight=500, got %v/%v", originProps["priority"], originProps["weight"])
	}
	if originProps["enabledState"] != "Enabled" {
		t.Fatalf("expected enabledState=Enabled, got %v", originProps["enabledState"])
	}
	if originProps["enforceCertificateNameCheck"] != true {
		t.Fatalf("expected enforceCertificateNameCheck=true, got %v", originProps["enforceCertificateNameCheck"])
	}
	if originProps["originGroupName"] != "g1" {
		t.Fatalf("expected originGroupName=g1, got %v", originProps["originGroupName"])
	}
	if originProps["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState=Succeeded, got %v", originProps["provisioningState"])
	}

	// GET and list both origin groups and origins.
	resp = doRequest(t, "GET", groups+"/g1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	resp.Body.Close()

	resp = doRequest(t, "GET", origins+"/o1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	origin = decodeJSON(t, resp)
	resp.Body.Close()

	resp = doRequest(t, "GET", groups+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	groupList := decodeJSON(t, resp)
	resp.Body.Close()
	groupItems, ok := groupList["value"].([]interface{})
	if !ok || len(groupItems) != 1 {
		t.Fatalf("expected 1 origin group, got %v", groupList["value"])
	}

	resp = doRequest(t, "GET", origins+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	originList := decodeJSON(t, resp)
	resp.Body.Close()
	originItems, ok := originList["value"].([]interface{})
	if !ok || len(originItems) != 1 {
		t.Fatalf("expected 1 origin, got %v", originList["value"])
	}

	// A full PUT of the origin GET body with only weight changed keeps the ID
	// and the parent link stable.
	originProps, _ = origin["properties"].(map[string]interface{})
	originProps["weight"] = float64(1000)
	putBody, err := json.Marshal(origin)
	if err != nil {
		t.Fatal(err)
	}
	resp = doRequest(t, "PUT", origins+"/o1"+frontDoorAPIVersion, string(putBody))
	expectStatus(t, resp, 200)
	origin = decodeJSON(t, resp)
	resp.Body.Close()
	if origin["id"] != wantOriginID {
		t.Fatalf("expected origin id stable after PUT, got %v", origin["id"])
	}
	originProps, _ = origin["properties"].(map[string]interface{})
	if originProps["weight"] != float64(1000) {
		t.Fatalf("expected weight=1000 after PUT, got %v", originProps["weight"])
	}
	if originProps["originGroupName"] != "g1" {
		t.Fatalf("expected origin group link stable after PUT, got %v", originProps["originGroupName"])
	}

	// Omitted values get their defaults.
	resp = doRequest(t, "PUT", groups+"/g2"+frontDoorAPIVersion, `{"properties":{}}`)
	expectStatus(t, resp, 201)
	defaultGroup := decodeJSON(t, resp)
	resp.Body.Close()
	defaultGroupProps, _ := defaultGroup["properties"].(map[string]interface{})
	defaultHealth, _ := defaultGroupProps["healthProbeSettings"].(map[string]interface{})
	if defaultHealth["probePath"] != "/" ||
		defaultHealth["probeProtocol"] != "Http" ||
		defaultHealth["probeRequestType"] != "HEAD" ||
		defaultHealth["probeIntervalInSeconds"] != float64(30) {
		t.Fatalf("expected default health probe, got %v", defaultHealth)
	}
	defaultLoadBalancing, _ := defaultGroupProps["loadBalancingSettings"].(map[string]interface{})
	if defaultLoadBalancing["sampleSize"] != float64(4) ||
		defaultLoadBalancing["successfulSamplesRequired"] != float64(3) ||
		defaultLoadBalancing["additionalLatencyInMilliseconds"] != float64(0) {
		t.Fatalf("expected default load balancing, got %v", defaultLoadBalancing)
	}
	if defaultGroupProps["sessionAffinityState"] != "Disabled" {
		t.Fatalf("expected default sessionAffinityState=Disabled, got %v", defaultGroupProps["sessionAffinityState"])
	}
	if defaultGroupProps["trafficRestorationTimeToHealedOrNewEndpointsInMinutes"] != float64(10) {
		t.Fatalf("expected default traffic restoration time=10, got %v", defaultGroupProps["trafficRestorationTimeToHealedOrNewEndpointsInMinutes"])
	}

	resp = doRequest(t, "PUT", groups+"/g2/origins/o2"+frontDoorAPIVersion, `{"properties":{"hostName":"default.example.com"}}`)
	expectStatus(t, resp, 201)
	defaultOrigin := decodeJSON(t, resp)
	resp.Body.Close()
	defaultOriginProps, _ := defaultOrigin["properties"].(map[string]interface{})
	if defaultOriginProps["originHostHeader"] != "default.example.com" {
		t.Fatalf("expected default originHostHeader from hostName, got %v", defaultOriginProps["originHostHeader"])
	}
	if defaultOriginProps["httpPort"] != float64(80) || defaultOriginProps["httpsPort"] != float64(443) {
		t.Fatalf("expected default ports 80/443, got %v/%v", defaultOriginProps["httpPort"], defaultOriginProps["httpsPort"])
	}
	if defaultOriginProps["priority"] != float64(1) || defaultOriginProps["weight"] != float64(1000) {
		t.Fatalf("expected default priority=1 weight=1000, got %v/%v", defaultOriginProps["priority"], defaultOriginProps["weight"])
	}
	if defaultOriginProps["enabledState"] != "Enabled" {
		t.Fatalf("expected default enabledState=Enabled, got %v", defaultOriginProps["enabledState"])
	}
	if defaultOriginProps["enforceCertificateNameCheck"] != true {
		t.Fatalf("expected default enforceCertificateNameCheck=true, got %v", defaultOriginProps["enforceCertificateNameCheck"])
	}

	// DELETE the origin group and its origins cascade away.
	resp = doRequest(t, "DELETE", groups+"/g1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	resp = doRequest(t, "GET", origins+"/o1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()

	// The cascade is scoped to the deleted group; another group's origin stays.
	resp = doRequest(t, "GET", groups+"/g2/origins/o2"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	resp.Body.Close()

	// PUT an origin under a missing group returns 404.
	resp = doRequest(t, "PUT", groups+"/missing/origins/o1"+frontDoorAPIVersion, originBody)
	expectStatus(t, resp, 404)
	resp.Body.Close()
}
