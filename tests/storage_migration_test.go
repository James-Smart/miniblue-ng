package tests

import (
	"testing"
)

func TestStorageAccountMigrationStatus(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	acctBase := blobARMBase(ts) + "/migrationacct"
	resp := doRequest(t, "PUT", acctBase, `{"location":"eastus","sku":{"name":"Standard_GRS"},"kind":"StorageV2"}`)
	defer resp.Body.Close()
	expectStatus(t, resp, 200)

	migrationURL := acctBase + "/accountMigrations/default"
	resp2 := doRequest(t, "GET", migrationURL, "")
	defer resp2.Body.Close()
	expectStatus(t, resp2, 200)

	m := decodeJSON(t, resp2)
	expectedID := "/subscriptions/" + blobSub + "/resourceGroups/" + blobRG + "/providers/Microsoft.Storage/storageAccounts/migrationacct/accountMigrations/default"
	if m["id"] != expectedID {
		t.Fatalf("expected id=%s, got %v", expectedID, m["id"])
	}
	if m["name"] != "default" {
		t.Fatalf("expected name=default, got %v", m["name"])
	}
	if m["type"] != "Microsoft.Storage/storageAccounts/accountMigrations" {
		t.Fatalf("expected accountMigrations type, got %v", m["type"])
	}

	props, ok := m["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %v", m["properties"])
	}
	if props["migrationStatus"] != "Complete" {
		t.Fatalf("expected migrationStatus=Complete, got %v", props["migrationStatus"])
	}
	if props["targetSkuName"] != "Standard_GRS" {
		t.Fatalf("expected targetSkuName=Standard_GRS, got %v", props["targetSkuName"])
	}
}

func TestStorageAccountMigrationStatusNotFound(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	resp := doRequest(t, "GET", blobARMBase(ts)+"/missingacct/accountMigrations/default", "")
	defer resp.Body.Close()
	expectStatus(t, resp, 404)
}
