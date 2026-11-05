package tests

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

const sshKeyAPIVersion = "?api-version=2024-11-01"

func sshKeyBase(tsURL string) string {
	return tsURL + "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/sshPublicKeys"
}

func sshKeyPublicKey(t *testing.T, m map[string]interface{}) string {
	t.Helper()
	props, ok := m["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties object, got %T", m["properties"])
	}
	key, ok := props["publicKey"].(string)
	if !ok {
		t.Fatalf("expected properties.publicKey string, got %T", props["publicKey"])
	}
	return key
}

func TestSSHKeyLifecycle(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	base := sshKeyBase(ts.URL)

	// PUT with a supplied public key echoes it back and creates the resource.
	resp := doRequest(t, "PUT", base+"/key1"+sshKeyAPIVersion,
		`{"location":"eastus","properties":{"publicKey":"ssh-rsa AAAA test"}}`)
	expectStatus(t, resp, 201)
	created := decodeJSON(t, resp)
	resp.Body.Close()

	wantID := "/subscriptions/sub1/resourceGroups/rg1/providers/Microsoft.Compute/sshPublicKeys/key1"
	if created["id"] != wantID {
		t.Fatalf("expected id=%s, got %v", wantID, created["id"])
	}
	if created["name"] != "key1" {
		t.Fatalf("expected name=key1, got %v", created["name"])
	}
	if created["type"] != "Microsoft.Compute/sshPublicKeys" {
		t.Fatalf("expected type=Microsoft.Compute/sshPublicKeys, got %v", created["type"])
	}
	if created["location"] != "eastus" {
		t.Fatalf("expected location=eastus, got %v", created["location"])
	}
	if got := sshKeyPublicKey(t, created); got != "ssh-rsa AAAA test" {
		t.Fatalf("expected publicKey echoed, got %v", got)
	}
	props := created["properties"].(map[string]interface{})
	if props["provisioningState"] != "Succeeded" {
		t.Fatalf("expected provisioningState=Succeeded, got %v", props["provisioningState"])
	}

	// GET returns the same key.
	resp = doRequest(t, "GET", base+"/key1"+sshKeyAPIVersion, "")
	expectStatus(t, resp, 200)
	got := decodeJSON(t, resp)
	resp.Body.Close()
	if key := sshKeyPublicKey(t, got); key != "ssh-rsa AAAA test" {
		t.Fatalf("GET: expected same publicKey, got %v", key)
	}

	// List returns the key in the resource group.
	resp = doRequest(t, "GET", base+sshKeyAPIVersion, "")
	expectStatus(t, resp, 200)
	list := decodeJSON(t, resp)
	resp.Body.Close()
	items, ok := list["value"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 ssh key, got %v", list["value"])
	}

	// PATCH tags succeeds and leaves the key unchanged.
	resp = doRequest(t, "PATCH", base+"/key1"+sshKeyAPIVersion, `{"tags":{"env":"test"}}`)
	expectStatus(t, resp, 200)
	patched := decodeJSON(t, resp)
	resp.Body.Close()
	if key := sshKeyPublicKey(t, patched); key != "ssh-rsa AAAA test" {
		t.Fatalf("PATCH: expected key unchanged, got %v", key)
	}
	tags, ok := patched["tags"].(map[string]interface{})
	if !ok || tags["env"] != "test" {
		t.Fatalf("PATCH: expected tags.env=test, got %v", patched["tags"])
	}

	// PUT without a publicKey generates a real key.
	resp = doRequest(t, "PUT", base+"/key2"+sshKeyAPIVersion, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	generated := decodeJSON(t, resp)
	resp.Body.Close()
	generatedKey := sshKeyPublicKey(t, generated)
	if !strings.HasPrefix(generatedKey, "ssh-rsa ") {
		t.Fatalf("expected generated publicKey to start with 'ssh-rsa ', got %q", generatedKey)
	}

	// GET returns the same generated key.
	resp = doRequest(t, "GET", base+"/key2"+sshKeyAPIVersion, "")
	expectStatus(t, resp, 200)
	got = decodeJSON(t, resp)
	resp.Body.Close()
	if key := sshKeyPublicKey(t, got); key != generatedKey {
		t.Fatalf("GET: expected generated key stable, want %q, got %q", generatedKey, key)
	}

	// PUT the full GET response with only tags changed; the generated key stays.
	got["tags"] = map[string]interface{}{"env": "prod"}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	resp = doRequest(t, "PUT", base+"/key2"+sshKeyAPIVersion, string(body))
	expectStatus(t, resp, 200)
	updated := decodeJSON(t, resp)
	resp.Body.Close()
	if key := sshKeyPublicKey(t, updated); key != generatedKey {
		t.Fatalf("PUT: expected generated key unchanged, want %q, got %q", generatedKey, key)
	}

	// POST generateKeyPair returns a valid OpenSSH public/private key pair.
	resp = doRequest(t, "POST", base+"/key1/generateKeyPair"+sshKeyAPIVersion, "")
	expectStatus(t, resp, 200)
	pair := decodeJSON(t, resp)
	resp.Body.Close()
	pairPub, _ := pair["publicKey"].(string)
	if !strings.HasPrefix(pairPub, "ssh-rsa ") {
		t.Fatalf("expected generated pair publicKey to start with 'ssh-rsa ', got %q", pairPub)
	}
	if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pairPub)); err != nil {
		t.Fatalf("expected parseable public key, got error: %v", err)
	}
	pairPriv, _ := pair["privateKey"].(string)
	if !strings.Contains(pairPriv, "BEGIN RSA PRIVATE KEY") && !strings.Contains(pairPriv, "BEGIN PRIVATE KEY") {
		t.Fatalf("expected PEM private key, got %q", pairPriv)
	}

	// DELETE then GET returns 404.
	resp = doRequest(t, "DELETE", base+"/key1"+sshKeyAPIVersion, "")
	expectStatus(t, resp, 202)
	resp.Body.Close()

	resp = doRequest(t, "GET", base+"/key1"+sshKeyAPIVersion, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()
}

func TestSSHKeyGeneratedKeyStable(t *testing.T) {
	ts := setupServer()
	defer ts.Close()

	base := sshKeyBase(ts.URL)

	resp := doRequest(t, "PUT", base+"/stable"+sshKeyAPIVersion, `{"location":"eastus"}`)
	expectStatus(t, resp, 201)
	created := decodeJSON(t, resp)
	resp.Body.Close()
	original := sshKeyPublicKey(t, created)
	if !strings.HasPrefix(original, "ssh-rsa ") {
		t.Fatalf("expected generated key, got %q", original)
	}

	resp = doRequest(t, "GET", base+"/stable"+sshKeyAPIVersion, "")
	expectStatus(t, resp, 200)
	got := decodeJSON(t, resp)
	resp.Body.Close()
	if key := sshKeyPublicKey(t, got); key != original {
		t.Fatalf("GET: expected key stable, want %q, got %q", original, key)
	}

	got["tags"] = map[string]interface{}{"changed": "true"}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	resp = doRequest(t, "PUT", base+"/stable"+sshKeyAPIVersion, string(body))
	expectStatus(t, resp, 200)
	updated := decodeJSON(t, resp)
	resp.Body.Close()
	if key := sshKeyPublicKey(t, updated); key != original {
		t.Fatalf("PUT: expected generated key unchanged, want %q, got %q", original, key)
	}

	// Re-encoding the raw response body must also preserve the key.
	raw, err := json.Marshal(updated)
	if err != nil {
		t.Fatal(err)
	}
	resp = doRequest(t, "PUT", base+"/stable"+sshKeyAPIVersion, string(bytes.TrimSpace(raw)))
	expectStatus(t, resp, 200)
	last := decodeJSON(t, resp)
	resp.Body.Close()
	if key := sshKeyPublicKey(t, last); key != original {
		t.Fatalf("PUT again: expected generated key unchanged, want %q, got %q", original, key)
	}
}
