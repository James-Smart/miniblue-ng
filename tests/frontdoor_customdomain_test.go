package tests

import (
	"encoding/json"
	"fmt"
	"regexp"
	"testing"
)

func TestFrontDoorCustomDomainAndAssociation(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	profileBase := frontDoorProfileBase(ts.URL)
	resp := doRequest(t, "PUT", profileBase+"/p1"+frontDoorAPIVersion, `{"location":"global"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "PUT", profileBase+"/p1/afdEndpoints/e1"+frontDoorAPIVersion, `{"properties":{}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "PUT", profileBase+"/p1/originGroups/g1"+frontDoorAPIVersion, `{"properties":{}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	// A route with no custom domains.
	routes := profileBase + "/p1/afdEndpoints/e1/routes"
	resp = doRequest(t, "PUT", routes+"/r1"+frontDoorAPIVersion, `{"properties":{}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	domains := profileBase + "/p1/customDomains"
	domainBody := `{
		"properties": {
			"hostName": "www.example.com",
			"azureDnsZone": {"id": "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/dnsZones/example.com"},
			"tlsSettings": {
				"certificateType": "ManagedCertificate",
				"minimumTlsVersion": "TLS12"
			}
		}
	}`

	resp = doRequest(t, "PUT", domains+"/www"+frontDoorAPIVersion, domainBody)
	expectStatus(t, resp, 201)
	domain := decodeJSON(t, resp)
	resp.Body.Close()

	wantDomainID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cdn/profiles/p1/customDomains/www"
	if domain["id"] != wantDomainID {
		t.Fatalf("expected custom domain id=%s, got %v", wantDomainID, domain["id"])
	}
	if domain["type"] != "Microsoft.Cdn/profiles/customDomains" {
		t.Fatalf("expected custom domain type, got %v", domain["type"])
	}

	props, ok := domain["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected custom domain properties object, got %T", domain["properties"])
	}
	if props["hostName"] != "www.example.com" {
		t.Fatalf("expected hostName echoed, got %v", props["hostName"])
	}
	zone, _ := props["azureDnsZone"].(map[string]interface{})
	if zone["id"] != "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/dnsZones/example.com" {
		t.Fatalf("expected azureDnsZone echoed, got %v", props["azureDnsZone"])
	}
	tls, _ := props["tlsSettings"].(map[string]interface{})
	if tls["certificateType"] != "ManagedCertificate" || tls["minimumTlsVersion"] != "TLS12" {
		t.Fatalf("expected tlsSettings echoed, got %v", props["tlsSettings"])
	}
	if props["deploymentStatus"] != "NotStarted" {
		t.Fatalf("expected deploymentStatus=NotStarted, got %v", props["deploymentStatus"])
	}

	validation, ok := props["validationProperties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected validationProperties object, got %T", props["validationProperties"])
	}
	token, _ := validation["validationToken"].(string)
	tokenPattern := regexp.MustCompile(`^[0-9a-f]{32}$`)
	if !tokenPattern.MatchString(token) {
		t.Fatalf("expected validationToken matching %s, got %q", tokenPattern, token)
	}
	if validation["validationState"] != "Pending" {
		t.Fatalf("expected validationState=Pending, got %v", validation["validationState"])
	}
	if props["domainValidationState"] != "Pending" {
		t.Fatalf("expected domainValidationState=Pending, got %v", props["domainValidationState"])
	}
	expiration, _ := validation["expirationDate"].(string)
	if expiration == "" {
		t.Fatalf("expected non-empty expirationDate")
	}

	// GET returns the same validation token without regenerating it.
	resp = doRequest(t, "GET", domains+"/www"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 200)
	domain = decodeJSON(t, resp)
	resp.Body.Close()
	props, _ = domain["properties"].(map[string]interface{})
	validation, _ = props["validationProperties"].(map[string]interface{})
	if validation["validationToken"] != token {
		t.Fatalf("expected validationToken stable on GET, got %v", validation["validationToken"])
	}
	if validation["expirationDate"] != expiration {
		t.Fatalf("expected expirationDate stable on GET, got %v", validation["expirationDate"])
	}

	// A full PUT of the GET body with only tags changed keeps the token.
	domain["tags"] = map[string]interface{}{"env": "prod"}
	putBody, err := json.Marshal(domain)
	if err != nil {
		t.Fatal(err)
	}
	resp = doRequest(t, "PUT", domains+"/www"+frontDoorAPIVersion, string(putBody))
	expectStatus(t, resp, 200)
	domain = decodeJSON(t, resp)
	resp.Body.Close()
	props, _ = domain["properties"].(map[string]interface{})
	validation, _ = props["validationProperties"].(map[string]interface{})
	if validation["validationToken"] != token {
		t.Fatalf("expected validationToken unchanged after PUT, got %v", validation["validationToken"])
	}
	tags, _ := domain["tags"].(map[string]interface{})
	if tags["env"] != "prod" {
		t.Fatalf("expected tags.env=prod after PUT, got %v", domain["tags"])
	}

	// Associate the custom domain with two routes. The second route is created
	// with the association so PUT returns 201; the first is updated.
	routeBody := fmt.Sprintf(`{"properties":{"customDomains":[{"id":%q}]}}`, wantDomainID)
	resp = doRequest(t, "PUT", routes+"/r1"+frontDoorAPIVersion, routeBody)
	expectStatus(t, resp, 200)
	resp.Body.Close()

	resp = doRequest(t, "PUT", routes+"/r2"+frontDoorAPIVersion, routeBody)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	// Association cleanup is case-insensitive: r3 stores the same ARM ID with
	// the domain name in a different case.
	variantDomainID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Cdn/profiles/p1/customDomains/WWW"
	resp = doRequest(t, "PUT", routes+"/r3"+frontDoorAPIVersion,
		fmt.Sprintf(`{"properties":{"customDomains":[{"id":%q}]}}`, variantDomainID))
	expectStatus(t, resp, 201)
	resp.Body.Close()

	for _, name := range []string{"r1", "r2"} {
		resp = doRequest(t, "GET", routes+"/"+name+frontDoorAPIVersion, "")
		expectStatus(t, resp, 200)
		route := decodeJSON(t, resp)
		resp.Body.Close()
		routeProps, _ := route["properties"].(map[string]interface{})
		associated, _ := routeProps["customDomains"].([]interface{})
		if len(associated) != 1 {
			t.Fatalf("route %s: expected 1 custom domain, got %v", name, routeProps["customDomains"])
		}
		entry, _ := associated[0].(map[string]interface{})
		if entry["id"] != wantDomainID {
			t.Fatalf("route %s: expected custom domain id %s, got %v", name, wantDomainID, entry["id"])
		}
	}

	// Deleting the custom domain removes it from every route in the profile.
	resp = doRequest(t, "DELETE", domains+"/www"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 204)
	resp.Body.Close()

	for _, name := range []string{"r1", "r2", "r3"} {
		resp = doRequest(t, "GET", routes+"/"+name+frontDoorAPIVersion, "")
		expectStatus(t, resp, 200)
		route := decodeJSON(t, resp)
		resp.Body.Close()
		routeProps, _ := route["properties"].(map[string]interface{})
		associated, ok := routeProps["customDomains"].([]interface{})
		if !ok || len(associated) != 0 {
			t.Fatalf("route %s: expected empty customDomains after domain delete, got %v", name, routeProps["customDomains"])
		}
	}

	resp = doRequest(t, "GET", domains+"/www"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
}
