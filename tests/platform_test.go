package tests

import (
	"testing"
)

func TestPlatformResourceLifecycle(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		body       string
		wantType   string
		wantMarker string
	}{
		{
			name:       "bastionHosts",
			path:       "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Network/bastionHosts/bastion1",
			body:       `{"location":"eastus","properties":{"marker":"bastion"}}`,
			wantType:   "Microsoft.Network/bastionHosts",
			wantMarker: "bastion",
		},
		{
			name:       "operationalInsightsWorkspaces",
			path:       "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.OperationalInsights/workspaces/ws1",
			body:       `{"location":"eastus","properties":{"marker":"workspace"}}`,
			wantType:   "Microsoft.OperationalInsights/workspaces",
			wantMarker: "workspace",
		},
		{
			name:       "maintenanceConfigurations",
			path:       "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Maintenance/maintenanceConfigurations/mc1",
			body:       `{"location":"eastus","properties":{"marker":"maintenance"}}`,
			wantType:   "Microsoft.Maintenance/maintenanceConfigurations",
			wantMarker: "maintenance",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := setupServer()
			defer ts.Close()
			url := ts.URL + tc.path

			resp := doRequest(t, "PUT", url, tc.body)
			expectStatus(t, resp, 201)
			created := decodeJSON(t, resp)
			resp.Body.Close()

			if created["id"] != tc.path {
				t.Errorf("expected id=%s, got %v", tc.path, created["id"])
			}
			if created["type"] != tc.wantType {
				t.Errorf("expected type=%s, got %v", tc.wantType, created["type"])
			}
			props, _ := created["properties"].(map[string]interface{})
			if props["provisioningState"] != "Succeeded" {
				t.Errorf("expected provisioningState=Succeeded, got %v", props["provisioningState"])
			}
			if props["marker"] != tc.wantMarker {
				t.Errorf("expected marker=%s, got %v", tc.wantMarker, props["marker"])
			}

			getResp := doRequest(t, "GET", url, "")
			expectStatus(t, getResp, 200)
			got := decodeJSON(t, getResp)
			getResp.Body.Close()
			if got["id"] != tc.path {
				t.Errorf("GET expected id=%s, got %v", tc.path, got["id"])
			}

			delResp := doRequest(t, "DELETE", url, "")
			expectStatus(t, delResp, 204)
			delResp.Body.Close()

			missingResp := doRequest(t, "GET", url, "")
			expectStatus(t, missingResp, 404)
			missingResp.Body.Close()
		})
	}
}

func TestOperationalInsightsDeletedWorkspacesList(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	paths := []string{
		"/subscriptions/sub1/providers/Microsoft.OperationalInsights/deletedWorkspaces",
		"/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.OperationalInsights/deletedWorkspaces",
	}
	for _, path := range paths {
		resp := doRequest(t, "GET", ts.URL+path+"?api-version=2025-07-01", "")
		expectStatus(t, resp, 200)
		body := decodeJSON(t, resp)
		resp.Body.Close()

		items, ok := body["value"].([]interface{})
		if !ok {
			t.Fatalf("%s: expected value array, got %v", path, body["value"])
		}
		if len(items) != 0 {
			t.Fatalf("%s: expected no deleted workspaces, got %d", path, len(items))
		}
	}
}

func TestOperationalInsightsWorkspaceCreateAfterDeletedWorkspaceCheck(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	checks := []string{
		"/subscriptions/sub1/providers/Microsoft.OperationalInsights/deletedWorkspaces",
		"/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.OperationalInsights/deletedWorkspaces",
	}
	for _, path := range checks {
		resp := doRequest(t, "GET", ts.URL+path+"?api-version=2025-07-01", "")
		expectStatus(t, resp, 200)
		resp.Body.Close()
	}

	workspace := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.OperationalInsights/workspaces/ws1"
	resp := doRequest(t, "PUT", ts.URL+workspace, `{"location":"eastus","properties":{"sku":{"name":"PerGB2018"}}}`)
	expectStatus(t, resp, 201)
	created := decodeJSON(t, resp)
	resp.Body.Close()

	props, _ := created["properties"].(map[string]interface{})
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState=Succeeded, got %v", props["provisioningState"])
	}

	getResp := doRequest(t, "GET", ts.URL+workspace, "")
	expectStatus(t, getResp, 200)
	getResp.Body.Close()
}

func TestOperationalInsightsWorkspaceSharedKeys(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	workspace := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.OperationalInsights/workspaces/ws1"
	resp := doRequest(t, "PUT", ts.URL+workspace, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	keysResp := doRequest(t, "POST", ts.URL+workspace+"/sharedKeys?api-version=2025-07-01", "")
	expectStatus(t, keysResp, 200)
	keys := decodeJSON(t, keysResp)
	keysResp.Body.Close()

	if keys["primarySharedKey"] == "" || keys["primarySharedKey"] == nil {
		t.Fatalf("expected primarySharedKey, got %v", keys["primarySharedKey"])
	}
	if keys["secondarySharedKey"] == "" || keys["secondarySharedKey"] == nil {
		t.Fatalf("expected secondarySharedKey, got %v", keys["secondarySharedKey"])
	}
}

func TestDiagnosticSettingsLifecycle(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	parent := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.OperationalInsights/workspaces/ws1"
	setting := parent + "/providers/Microsoft.Insights/diagnosticSettings/diag1"
	url := ts.URL + setting

	resp := doRequest(t, "PUT", url, `{"properties":{"workspaceId":"ws1","logs":[]}}`)
	expectStatus(t, resp, 200)
	created := decodeJSON(t, resp)
	resp.Body.Close()

	if created["id"] != setting {
		t.Errorf("expected id=%s, got %v", setting, created["id"])
	}
	if created["name"] != "diag1" {
		t.Errorf("expected name=diag1, got %v", created["name"])
	}
	if created["type"] != "Microsoft.Insights/diagnosticSettings" {
		t.Errorf("expected type=Microsoft.Insights/diagnosticSettings, got %v", created["type"])
	}
	props, _ := created["properties"].(map[string]interface{})
	if props["provisioningState"] != "Succeeded" {
		t.Errorf("expected provisioningState=Succeeded, got %v", props["provisioningState"])
	}
	if props["workspaceId"] != "ws1" {
		t.Errorf("expected workspaceId=ws1, got %v", props["workspaceId"])
	}

	updateResp := doRequest(t, "PUT", url, `{"properties":{"workspaceId":"ws2","logs":[{"category":"Audit","enabled":true}]}}`)
	expectStatus(t, updateResp, 200)
	updated := decodeJSON(t, updateResp)
	updateResp.Body.Close()
	updatedProps, _ := updated["properties"].(map[string]interface{})
	if updatedProps["workspaceId"] != "ws2" {
		t.Errorf("expected updated workspaceId=ws2, got %v", updatedProps["workspaceId"])
	}

	getResp := doRequest(t, "GET", url, "")
	expectStatus(t, getResp, 200)
	got := decodeJSON(t, getResp)
	getResp.Body.Close()
	if got["id"] != setting {
		t.Errorf("GET expected id=%s, got %v", setting, got["id"])
	}
	gotProps, _ := got["properties"].(map[string]interface{})
	if gotProps["workspaceId"] != "ws2" {
		t.Errorf("GET expected workspaceId=ws2, got %v", gotProps["workspaceId"])
	}

	listResp := doRequest(t, "GET", ts.URL+parent+"/providers/Microsoft.Insights/diagnosticSettings", "")
	expectStatus(t, listResp, 200)
	list := decodeJSON(t, listResp)
	listResp.Body.Close()
	items, _ := list["value"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("expected 1 diagnostic setting, got %d", len(items))
	}

	delResp := doRequest(t, "DELETE", url, "")
	expectStatus(t, delResp, 204)
	delResp.Body.Close()

	missingResp := doRequest(t, "GET", url, "")
	expectStatus(t, missingResp, 404)
	missingResp.Body.Close()

	missingDeleteResp := doRequest(t, "DELETE", url, "")
	expectStatus(t, missingDeleteResp, 404)
	missingDeleteResp.Body.Close()
}
