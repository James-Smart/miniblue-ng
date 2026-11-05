package tests

import "testing"

// TestFrontDoorCascadeAndResourceGroupCleanup verifies two cleanup paths:
//
//  1. Deleting a Front Door profile removes every child kind (endpoint, route,
//     origin group, origin, rule set, rule, custom domain, secret and security
//     policy) without touching sibling profiles or the resource-group-scoped
//     WAF policy.
//  2. Deleting a resource group removes the whole fd:<sub>:<rg>: footprint,
//     including WAF policies, without crossing into other RGs or subscriptions.
func TestFrontDoorCascadeAndResourceGroupCleanup(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	createRG(t, ts, "sub1", "rg1")
	createRG(t, ts, "sub1", "rg2")
	createRG(t, ts, "sub2", "rg1")

	base := frontDoorProfileBase(ts.URL)
	wafBase := frontDoorWAFBase(ts.URL)

	put := func(path, body string) {
		t.Helper()
		resp := doRequest(t, "PUT", path+frontDoorAPIVersion, body)
		if resp.StatusCode != 201 && resp.StatusCode != 200 {
			t.Fatalf("PUT %s: expected 200 or 201, got %d", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
	get := func(path string) int {
		t.Helper()
		resp := doRequest(t, "GET", path+frontDoorAPIVersion, "")
		resp.Body.Close()
		return resp.StatusCode
	}

	// Profile p1 with one resource of every child kind.
	put(base+"/p1", `{"location":"global"}`)
	put(base+"/p1/afdEndpoints/e1", `{"properties":{}}`)
	put(base+"/p1/afdEndpoints/e1/routes/r1", `{"properties":{}}`)
	put(base+"/p1/originGroups/g1", `{"properties":{}}`)
	put(base+"/p1/originGroups/g1/origins/o1", `{"properties":{"hostName":"origin.example.com"}}`)
	put(base+"/p1/ruleSets/rs1", `{"properties":{}}`)
	put(base+"/p1/ruleSets/rs1/rules/r1", `{"properties":{}}`)
	put(base+"/p1/customDomains/www", `{"properties":{"hostName":"www.example.com"}}`)
	put(base+"/p1/secrets/s1", `{"properties":{"parameters":{"type":"CustomerCertificate"}}}`)
	put(base+"/p1/securityPolicies/sp1", `{"properties":{"parameters":{"type":"WebApplicationFirewall"}}}`)

	// WAF policies are resource-group scoped, not profile children.
	put(wafBase+"/waf1", `{"properties":{}}`)

	// A sibling profile in the same RG must be untouched by the profile delete.
	put(base+"/p1sibling", `{"location":"global"}`)

	resp := doRequest(t, "DELETE", base+"/p1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	childPaths := []string{
		base + "/p1/afdEndpoints/e1",
		base + "/p1/afdEndpoints/e1/routes/r1",
		base + "/p1/originGroups/g1",
		base + "/p1/originGroups/g1/origins/o1",
		base + "/p1/ruleSets/rs1",
		base + "/p1/ruleSets/rs1/rules/r1",
		base + "/p1/customDomains/www",
		base + "/p1/secrets/s1",
		base + "/p1/securityPolicies/sp1",
	}
	for _, path := range childPaths {
		if got := get(path); got != 404 {
			t.Errorf("child %s should be 404 after profile delete, got %d", path, got)
		}
	}

	if got := get(base + "/p1"); got != 404 {
		t.Errorf("deleted profile should be 404, got %d", got)
	}
	if got := get(base + "/p1sibling"); got != 200 {
		t.Errorf("sibling profile should survive profile delete, got %d", got)
	}
	if got := get(wafBase + "/waf1"); got != 200 {
		t.Errorf("WAF policy should survive profile delete, got %d", got)
	}

	// Re-create p1. Leaked top-level children would become visible again.
	put(base+"/p1", `{"location":"global"}`)
	topLevelPaths := []string{
		base + "/p1/afdEndpoints/e1",
		base + "/p1/originGroups/g1",
		base + "/p1/ruleSets/rs1",
		base + "/p1/customDomains/www",
		base + "/p1/secrets/s1",
		base + "/p1/securityPolicies/sp1",
	}
	for _, path := range topLevelPaths {
		if got := get(path); got != 404 {
			t.Errorf("child %s leaked past profile delete, got %d after recreating profile", path, got)
		}
	}

	// Re-create the intermediate parents. Leaked grandchildren would become
	// visible again.
	put(base+"/p1/afdEndpoints/e1", `{"properties":{}}`)
	put(base+"/p1/originGroups/g1", `{"properties":{}}`)
	put(base+"/p1/ruleSets/rs1", `{"properties":{}}`)
	nestedPaths := []string{
		base + "/p1/afdEndpoints/e1/routes/r1",
		base + "/p1/originGroups/g1/origins/o1",
		base + "/p1/ruleSets/rs1/rules/r1",
	}
	for _, path := range nestedPaths {
		if got := get(path); got != 404 {
			t.Errorf("child %s leaked past profile delete, got %d after recreating parents", path, got)
		}
	}

	// A profile in a second RG and one in another subscription with the same RG
	// name prove that RG cleanup is scoped to one subscription and RG.
	rg2Base := ts.URL + "/subscriptions/sub1/resourceGroups/rg2/providers/Microsoft.Cdn/profiles"
	put(rg2Base+"/p2", `{"location":"global"}`)
	otherSubBase := ts.URL + "/subscriptions/sub2/resourceGroups/rg1/providers/Microsoft.Cdn/profiles"
	put(otherSubBase+"/p3", `{"location":"global"}`)

	resp = doRequest(t, "DELETE", ts.URL+"/subscriptions/sub1/resourcegroups/rg1"+frontDoorAPIVersion, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	if got := get(rg2Base + "/p2"); got != 200 {
		t.Errorf("profile in another RG should survive RG delete, got %d", got)
	}
	if got := get(otherSubBase + "/p3"); got != 200 {
		t.Errorf("profile in another subscription with the same RG name should survive RG delete, got %d", got)
	}
	if got := get(wafBase + "/waf1"); got != 404 {
		t.Errorf("WAF policy should be removed by RG delete, got %d", got)
	}
	if got := get(base + "/p1"); got != 404 {
		t.Errorf("profile should be removed by RG delete, got %d", got)
	}
}
