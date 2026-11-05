package vm

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Store keys, container naming, response shaping
// ---------------------------------------------------------------------------

func vmKey(sub, rg, name string) string {
	return "vm:" + sub + ":" + rg + ":" + name
}

func svcKey(sub, rg, vmName, svc string) string {
	return "vmsvc:" + sub + ":" + rg + ":" + vmName + ":" + svc
}

func secretKey(secret string) string {
	return "vmidsecret:" + secret
}

func vmContainerName(rg, name string) string {
	return "miniblue-vm-" + rg + "-" + name
}

func vmNetworkName(rg, name string) string {
	return "miniblue-vmnet-" + rg + "-" + name
}

func svcContainerName(rg, vmName, svc string) string {
	return "miniblue-vmsvc-" + rg + "-" + vmName + "-" + svc
}

func vmARMID(sub, rg, name string) string {
	return "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.Compute/virtualMachines/" + name
}

// identityEnv builds the managed-identity environment variables injected into
// the VM container and every service container. The endpoint points back at
// the miniblue host port through the host gateway alias; MSI_* are the legacy
// names older Azure SDKs probe for.
func identityEnv(secret string) []string {
	port := os.Getenv("PORT")
	if port == "" {
		port = "4566"
	}
	endpoint := "http://host.docker.internal:" + port + "/metadata/identity/oauth2/token"
	return []string{
		"IDENTITY_ENDPOINT=" + endpoint,
		"IDENTITY_HEADER=" + secret,
		"MSI_ENDPOINT=" + endpoint,
		"MSI_SECRET=" + secret,
	}
}

// appendProvisionEvent records a timestamped lifecycle event on the VM record
// (the `_provisionLog` internal field backing the VM-level log view).
func appendProvisionEvent(rec map[string]interface{}, format string, args ...interface{}) {
	events, _ := rec["_provisionLog"].([]interface{})
	line := time.Now().UTC().Format(time.RFC3339) + " " + fmt.Sprintf(format, args...)
	rec["_provisionLog"] = append(events, line)
}

func provisionEvents(rec map[string]interface{}) []string {
	events, _ := rec["_provisionLog"].([]interface{})
	out := make([]string, 0, len(events))
	for _, e := range events {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// publicView returns a copy of a stored record with internal fields (those
// prefixed by "_") stripped, suitable for API responses.
func publicView(rec map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(rec))
	for k, v := range rec {
		if strings.HasPrefix(k, "_") {
			continue
		}
		out[k] = v
	}
	return out
}

// vmPublicView is publicView plus the derived instance view. The instance view
// is always recomputed from the current stored power state so a response can
// never report a stale status after a power action.
func vmPublicView(rec map[string]interface{}) map[string]interface{} {
	out := publicView(rec)
	props, _ := rec["properties"].(map[string]interface{})
	viewProps := make(map[string]interface{}, len(props)+1)
	for k, v := range props {
		viewProps[k] = v
	}
	viewProps["instanceView"] = instanceViewFor(rec)
	out["properties"] = viewProps
	return out
}

func getProps(rec map[string]interface{}) map[string]interface{} {
	props, _ := rec["properties"].(map[string]interface{})
	if props == nil {
		props = map[string]interface{}{}
		rec["properties"] = props
	}
	return props
}

func getMinibluProps(rec map[string]interface{}) map[string]interface{} {
	props := getProps(rec)
	mb, _ := props["miniblue"].(map[string]interface{})
	if mb == nil {
		mb = map[string]interface{}{}
		props["miniblue"] = mb
	}
	return mb
}

func powerState(rec map[string]interface{}) string {
	mb := getMinibluProps(rec)
	s, _ := mb["powerState"].(string)
	return s
}

// osDiskName returns the OS disk name from the storage profile, falling back
// to the internal field used by disk provisioning. It returns "" when the VM
// has no OS disk yet (for example, a stub-mode VM created without one).
func osDiskName(rec map[string]interface{}) string {
	if name, _ := rec["_osDiskName"].(string); name != "" {
		return name
	}
	props := getProps(rec)
	storage, _ := props["storageProfile"].(map[string]interface{})
	if storage == nil {
		return ""
	}
	osDisk, _ := storage["osDisk"].(map[string]interface{})
	name, _ := osDisk["name"].(string)
	return name
}

// instanceViewFor builds the ARM instance view for a VM record from its
// current power state and, when known, its OS disk.
func instanceViewFor(rec map[string]interface{}) map[string]interface{} {
	now := time.Now().UTC().Format(time.RFC3339)

	powerCode, powerDisplay := "PowerState/running", "VM running"
	switch powerState(rec) {
	case "stopped":
		powerCode, powerDisplay = "PowerState/stopped", "VM stopped"
	case "deallocated":
		powerCode, powerDisplay = "PowerState/deallocated", "VM deallocated"
	}

	iv := map[string]interface{}{
		"statuses": []interface{}{
			map[string]interface{}{
				"code":          "ProvisioningState/succeeded",
				"level":         "Info",
				"displayStatus": "Provisioning succeeded",
				"time":          now,
			},
			map[string]interface{}{
				"code":          powerCode,
				"level":         "Info",
				"displayStatus": powerDisplay,
			},
		},
		"vmAgent": map[string]interface{}{
			"vmAgentVersion": "2.0.0",
			"statuses": []interface{}{
				map[string]interface{}{
					"code":          "ProvisioningState/succeeded",
					"level":         "Info",
					"displayStatus": "Ready",
				},
			},
		},
	}

	if name := osDiskName(rec); name != "" {
		iv["disks"] = []interface{}{
			map[string]interface{}{
				"name": name,
				"statuses": []interface{}{
					map[string]interface{}{
						"code":          "ProvisioningState/succeeded",
						"level":         "Info",
						"displayStatus": "Provisioning succeeded",
					},
				},
			},
		}
	}
	return iv
}

// copyPropertyBlocks copies pass-through property blocks from a request onto a
// stored VM, skipping fields miniblue generates itself and write-only secrets.
func copyPropertyBlocks(dst, src map[string]interface{}) {
	for k, v := range src {
		switch k {
		case "miniblue", "provisioningState", "vmId", "instanceView":
			continue
		}
		dst[k] = v
	}
	if osProfile, ok := dst["osProfile"].(map[string]interface{}); ok {
		delete(osProfile, "adminPassword")
	}
}

// vmSizesCatalog is a broad superset of common Azure VM sizes. The provider
// only uses this endpoint to decide whether it must deallocate before a
// resize, so returning more sizes than miniblue can actually run is fine.
func vmSizesCatalog() []interface{} {
	type size struct {
		name                 string
		cores, memoryMB      int
		osDiskMB, resourceMB int
		maxDataDisks         int
	}
	sizes := []size{
		{"Standard_B1s", 1, 1024, 1047552, 4096, 2},
		{"Standard_B1ms", 1, 2048, 1047552, 4096, 2},
		{"Standard_B2s", 2, 4096, 1047552, 8192, 4},
		{"Standard_B2ms", 2, 8192, 1047552, 8192, 4},
		{"Standard_B4ms", 4, 16384, 1047552, 16384, 8},
		{"Standard_DS1_v2", 1, 3584, 1047552, 7168, 2},
		{"Standard_DS2_v2", 2, 7168, 1047552, 14336, 4},
		{"Standard_D2s_v3", 2, 8192, 1047552, 16384, 4},
		{"Standard_D4s_v3", 4, 16384, 1047552, 32768, 8},
		{"Standard_D8s_v3", 8, 32768, 1047552, 65536, 16},
		{"Standard_E2s_v3", 2, 16384, 1047552, 32768, 4},
		{"Standard_E4s_v3", 4, 32768, 1047552, 65536, 8},
		{"Standard_F2s_v2", 2, 4096, 1047552, 8192, 4},
	}
	out := make([]interface{}, 0, len(sizes))
	for _, s := range sizes {
		out = append(out, map[string]interface{}{
			"name":                 s.name,
			"numberOfCores":        s.cores,
			"osDiskSizeInMB":       s.osDiskMB,
			"resourceDiskSizeInMB": s.resourceMB,
			"memoryInMB":           s.memoryMB,
			"maxDataDiskCount":     s.maxDataDisks,
		})
	}
	return out
}

// buildVMRecord assembles the stored VM record from the request input,
// echoing every accepted property block (not just the computed subset) and
// adding miniblue fields.
func buildVMRecord(sub, rg, name string, input map[string]interface{}, image string) map[string]interface{} {
	location, _ := input["location"].(string)
	if location == "" {
		location = "eastus"
	}
	inProps, _ := input["properties"].(map[string]interface{})
	if inProps == nil {
		inProps = map[string]interface{}{}
	}

	props := map[string]interface{}{
		"provisioningState": "Succeeded",
		"miniblue": map[string]interface{}{
			"image":         image,
			"powerState":    "running",
			"containerName": vmContainerName(rg, name),
		},
	}
	copyPropertyBlocks(props, inProps)
	if props["hardwareProfile"] == nil {
		props["hardwareProfile"] = map[string]interface{}{"vmSize": "Standard_B1s"}
	}
	props["vmId"] = uuid.NewString()

	rec := map[string]interface{}{
		"id":         vmARMID(sub, rg, name),
		"name":       name,
		"type":       "Microsoft.Compute/virtualMachines",
		"location":   location,
		"properties": props,
	}
	for _, k := range []string{"tags", "zones", "plan", "extendedLocation"} {
		if v, ok := input[k]; ok {
			rec[k] = v
		}
	}
	props["instanceView"] = instanceViewFor(rec)
	return rec
}
