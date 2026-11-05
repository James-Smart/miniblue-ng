package tests

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// setupFailingRunDocker puts a fake `docker` executable on PATH whose `info`
// probe succeeds but whose `run` command fails while the returned marker file
// exists. Removing the marker makes `run` succeed, which lets a retry exercise
// the success path against the same handler.
func setupFailingRunDocker(t *testing.T) (ts *httptest.Server, failMarker string) {
	t.Helper()
	dir := t.TempDir()
	failMarker = filepath.Join(dir, "fail-run")
	if err := os.WriteFile(failMarker, []byte("fail"), 0o644); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
case "$1" in
  info)
    exit 0
    ;;
  run)
    if [ -f "` + failMarker + `" ]; then
      echo "fake docker run failure" >&2
      exit 1
    fi
    echo "fake-container-id"
    exit 0
    ;;
  network)
    exit 0
    ;;
  rm|stop|start|inspect|exec|logs)
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
`
	dockerPath := filepath.Join(dir, "docker")
	if err := os.WriteFile(dockerPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MINIBLUE_DISABLE_DOCKER", "")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ts = setupServer()
	t.Cleanup(ts.Close)
	return ts, failMarker
}

func listDisks(t *testing.T, ts *httptest.Server) []interface{} {
	t.Helper()
	resp := doRequest(t, "GET", diskBase(ts.URL)+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	list := decodeJSON(t, resp)
	resp.Body.Close()
	items, _ := list["value"].([]interface{})
	return items
}

// TestVMCreateContainerFailureLeavesNoDisks pins atomic VM creation: when
// `docker run` fails, no generated OS disk or data-disk record survives in the
// resource group and the VM is not stored.
func TestVMCreateContainerFailureLeavesNoDisks(t *testing.T) {
	ts, _ := setupFailingRunDocker(t)
	createRG(t, ts, "sub1", "rg1")

	body := `{"location":"eastus","properties":{
		"storageProfile":{"osDisk":{"diskSizeGB":64}},
		"hardwareProfile":{"vmSize":"Standard_B2s"}
	}}`
	resp := doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm")+vmAV, body)
	expectStatus(t, resp, 409)
	e := decodeError(t, resp)
	resp.Body.Close()
	if e.Error.Code != "ContainerStartFailed" {
		t.Fatalf("expected ContainerStartFailed, got %s", e.Error.Code)
	}

	resp = doRequest(t, "GET", vmURL(ts, "sub1", "rg1", "vm")+vmAV, "")
	expectStatus(t, resp, 404)
	resp.Body.Close()

	if disks := listDisks(t, ts); len(disks) != 0 {
		t.Fatalf("expected no disk records after failed VM create, got %d: %v", len(disks), disks)
	}
}

// TestVMCreateExplicitDiskRetryAfterContainerFailure verifies that a failed
// create leaves an explicitly referenced disk detached, so retrying the same
// VM PUT succeeds instead of failing with DiskInUse/DiskAttached.
func TestVMCreateExplicitDiskRetryAfterContainerFailure(t *testing.T) {
	ts, failMarker := setupFailingRunDocker(t)
	createRG(t, ts, "sub1", "rg1")

	resp := doRequest(t, "PUT", diskBase(ts.URL)+"/existing"+diskAPIVersion,
		`{"location":"eastus","sku":{"name":"Premium_LRS"},"properties":{"diskSizeGB":128,"creationData":{"createOption":"Empty"}}}`)
	expectStatus(t, resp, 200)
	disk := decodeJSON(t, resp)
	resp.Body.Close()
	diskID, _ := disk["id"].(string)
	if diskID == "" {
		t.Fatal("expected pre-created disk id")
	}

	body := `{"location":"eastus","properties":{
		"storageProfile":{"osDisk":{"managedDisk":{"id":"` + diskID + `"}}}
	}}`
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm-retry")+vmAV, body)
	expectStatus(t, resp, 409)
	resp.Body.Close()

	// The failed attempt must not have attached the explicit disk.
	resp = doRequest(t, "GET", ts.URL+diskID+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	disk = decodeJSON(t, resp)
	resp.Body.Close()
	diskProps := disk["properties"].(map[string]interface{})
	if diskProps["managedBy"] != "" || diskProps["diskState"] != "Unattached" {
		t.Fatalf("expected failed create to leave disk detached, got managedBy=%v diskState=%v",
			diskProps["managedBy"], diskProps["diskState"])
	}

	if err := os.Remove(failMarker); err != nil {
		t.Fatal(err)
	}

	// Retrying the same request must succeed now that `docker run` works.
	resp = doRequest(t, "PUT", vmURL(ts, "sub1", "rg1", "vm-retry")+vmAV, body)
	expectStatus(t, resp, 201)
	created := decodeJSON(t, resp)
	resp.Body.Close()
	vmID := created["properties"].(map[string]interface{})["vmId"].(string)

	resp = doRequest(t, "GET", ts.URL+diskID+diskAPIVersion, "")
	expectStatus(t, resp, 200)
	disk = decodeJSON(t, resp)
	resp.Body.Close()
	diskProps = disk["properties"].(map[string]interface{})
	if diskProps["managedBy"] != vmID || diskProps["diskState"] != "Attached" {
		t.Fatalf("expected retried create to attach disk, got managedBy=%v diskState=%v",
			diskProps["managedBy"], diskProps["diskState"])
	}
}
