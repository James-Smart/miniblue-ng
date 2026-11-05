package tests

import (
	"strings"
	"testing"
)

const keyVaultARMVersion = "?api-version=2026-02-01"

func keyVaultURL(baseURL, sub, rg, name string) string {
	return baseURL + "/subscriptions/" + sub + "/resourceGroups/" + rg +
		"/providers/Microsoft.KeyVault/vaults/" + name
}

func deletedVaultURL(baseURL, sub, location, name string) string {
	return baseURL + "/subscriptions/" + sub +
		"/providers/Microsoft.KeyVault/locations/" + location + "/deletedVaults/" + name
}

func TestKeyVaultARMLifecycle(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	base := keyVaultURL(ts.URL, "sub1", "rg1", "myvault")
	body := `{
		"location":"eastus",
		"tags":{"env":"test"},
		"properties":{
			"tenantId":"00000000-0000-0000-0000-000000000001",
			"sku":{"family":"A","name":"standard"},
			"accessPolicies":[],
			"enableSoftDelete":true,
			"publicNetworkAccess":"Enabled"
		}
	}`

	resp := doRequest(t, "PUT", base+keyVaultARMVersion, body)
	expectStatus(t, resp, 201)
	created := decodeJSON(t, resp)
	resp.Body.Close()

	if created["id"] != "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.KeyVault/vaults/myvault" {
		t.Fatalf("unexpected id: %v", created["id"])
	}
	if created["type"] != "Microsoft.KeyVault/vaults" {
		t.Fatalf("unexpected type: %v", created["type"])
	}
	props := created["properties"].(map[string]interface{})
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState=Succeeded, got %v", props["provisioningState"])
	}
	vaultURI, _ := props["vaultUri"].(string)
	if !strings.Contains(vaultURI, "/keyvault/myvault") {
		t.Fatalf("expected local vaultUri, got %q", vaultURI)
	}

	resp = doRequest(t, "GET", base+keyVaultARMVersion, "")
	expectStatus(t, resp, 200)
	got := decodeJSON(t, resp)
	resp.Body.Close()
	if got["name"] != "myvault" {
		t.Fatalf("expected name=myvault, got %v", got["name"])
	}

	resp = doRequest(t, "PATCH", base+keyVaultARMVersion,
		`{"tags":{"env":"prod"},"properties":{"publicNetworkAccess":"Disabled"}}`)
	expectStatus(t, resp, 200)
	updated := decodeJSON(t, resp)
	resp.Body.Close()
	updatedProps := updated["properties"].(map[string]interface{})
	if updatedProps["publicNetworkAccess"] != "Disabled" {
		t.Fatalf("expected publicNetworkAccess=Disabled, got %v", updatedProps["publicNetworkAccess"])
	}
	if updatedProps["vaultUri"] != vaultURI {
		t.Fatalf("expected vaultUri preserved, want %q got %v", vaultURI, updatedProps["vaultUri"])
	}
	tags := updated["tags"].(map[string]interface{})
	if tags["env"] != "prod" {
		t.Fatalf("expected tag env=prod, got %v", tags["env"])
	}

	resp = doRequest(t, "GET", ts.URL+"/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.KeyVault/vaults"+keyVaultARMVersion, "")
	expectStatus(t, resp, 200)
	list := decodeJSON(t, resp)
	resp.Body.Close()
	if items, _ := list["value"].([]interface{}); len(items) != 1 {
		t.Fatalf("expected 1 vault in list, got %v", list["value"])
	}

	resp = doRequest(t, "DELETE", base+keyVaultARMVersion, "")
	expectStatus(t, resp, 200)
	resp.Body.Close()

	resp = doRequest(t, "GET", base+keyVaultARMVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()

	deletedBase := deletedVaultURL(ts.URL, "sub1", "eastus", "myvault")
	resp = doRequest(t, "GET", deletedBase+keyVaultARMVersion, "")
	expectStatus(t, resp, 200)
	deleted := decodeJSON(t, resp)
	resp.Body.Close()
	deletedProps := deleted["properties"].(map[string]interface{})
	if deletedProps["vaultId"] != created["id"] {
		t.Fatalf("expected deleted vault vaultId=%v, got %v", created["id"], deletedProps["vaultId"])
	}
	if _, ok := deletedProps["deletionDate"].(string); !ok {
		t.Fatalf("expected deletionDate, got %v", deletedProps["deletionDate"])
	}

	resp = doRequest(t, "POST", deletedBase+"/purge"+keyVaultARMVersion, "")
	expectStatus(t, resp, 202)
	location := resp.Header.Get("Location")
	resp.Body.Close()
	if location == "" {
		t.Fatal("expected purge operation Location header")
	}

	resp = doRequest(t, "GET", location, "")
	expectStatus(t, resp, 200)
	op := decodeJSON(t, resp)
	resp.Body.Close()
	if op["status"] != "Succeeded" {
		t.Fatalf("expected purge operation Succeeded, got %v", op["status"])
	}

	resp = doRequest(t, "GET", deletedBase+keyVaultARMVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
}

func TestKeyVaultARMNotFound(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	resp := doRequest(t, "GET", keyVaultURL(ts.URL, "sub1", "rg1", "missing")+keyVaultARMVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()

	resp = doRequest(t, "GET", deletedVaultURL(ts.URL, "sub1", "eastus", "missing")+keyVaultARMVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
}

func TestKeyVaultResourceGroupCascade(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	resp := doRequest(t, "PUT", ts.URL+"/subscriptions/sub1/resourceGroups/rg1?api-version=2023-07-01",
		`{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "PUT", keyVaultURL(ts.URL, "sub1", "rg1", "cascade-vault")+keyVaultARMVersion,
		`{"location":"eastus","properties":{"tenantId":"00000000-0000-0000-0000-000000000001","sku":{"family":"A","name":"standard"}}}`)
	expectStatus(t, resp, 201)
	resp.Body.Close()

	resp = doRequest(t, "DELETE", ts.URL+"/subscriptions/sub1/resourceGroups/rg1?api-version=2023-07-01", "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	resp = doRequest(t, "GET", keyVaultURL(ts.URL, "sub1", "rg1", "cascade-vault")+keyVaultARMVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
}
