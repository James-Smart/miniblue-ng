package vm

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/moabukar/miniblue/internal/services/disks"
)

// osDiskNameMaxLen is the maximum length Azure accepts for a managed disk
// name. Generated OS disk names are truncated to fit while preserving the
// distinguishing suffix.
const osDiskNameMaxLen = 80

const osDiskNameSuffix = "_OsDisk_1_"

// diskNotFoundError reports an explicit managed-disk ID that does not resolve
// to an existing disk record.
type diskNotFoundError struct {
	id string
}

func (e *diskNotFoundError) Error() string {
	return "The disk '" + e.id + "' could not be found."
}

// ensureOSDisk makes sure the VM has an attached OS disk. An explicit
// `storageProfile.osDisk.managedDisk.id` must resolve to an existing disk,
// which is attached without transferring ownership. Otherwise a managed disk is
// generated in the VM's resource group and owned by the VM.
func (h *Handler) ensureOSDisk(rec map[string]interface{}) error {
	sub, rg, vmName, ok := vmIdentity(rec)
	if !ok {
		return fmt.Errorf("invalid virtual machine ID %q", asString(rec["id"]))
	}
	props := getProps(rec)
	vmID := asString(props["vmId"])
	if vmID == "" {
		vmID = uuid.NewString()
		props["vmId"] = vmID
	}
	storage := vmStorageProfile(rec)
	osDisk := vmOSDisk(rec)

	if md, _ := osDisk["managedDisk"].(map[string]interface{}); md != nil {
		if id := asString(md["id"]); id != "" {
			dsub, drg, dname, ok := disks.ParseID(id)
			if !ok {
				return &diskNotFoundError{id: id}
			}
			diskRec, found := disks.Get(h.store, dsub, drg, dname)
			if !found {
				return &diskNotFoundError{id: id}
			}
			// Ownership stays with the disk resource; only link it.
			if !disks.Attach(h.store, dsub, drg, dname, vmID) {
				return &diskNotFoundError{id: id}
			}
			applyOSDisk(rec, diskRec)
			osDisk["name"] = dname
			return nil
		}
	}

	diskName := generatedOSDiskName(vmName)
	diskRec := disks.NewRecord(sub, rg, diskName, map[string]interface{}{
		"location": asString(rec["location"]),
		"sku": map[string]interface{}{
			"name": requestedOSDiskSKU(osDisk),
		},
		"properties": map[string]interface{}{
			"osType":       "Linux",
			"diskSizeGB":   requestedOSDiskSize(osDisk),
			"creationData": requestedOSDiskCreationData(storage, osDisk),
		},
	})
	diskRec["_vmOwned"] = true
	disks.Set(h.store, sub, rg, diskName, diskRec)
	disks.Attach(h.store, sub, rg, diskName, vmID)

	applyOSDisk(rec, diskRec)
	osDisk["name"] = diskName
	return nil
}

// detachVMDisks detaches every managed disk referenced by the VM's storage
// profile (OS and data) without deleting the disk resources.
func (h *Handler) detachVMDisks(rec map[string]interface{}) {
	props := getProps(rec)
	vmID := asString(props["vmId"])
	storage, _ := props["storageProfile"].(map[string]interface{})
	if storage == nil {
		return
	}

	ids := make([]string, 0, 1)
	if osDisk, _ := storage["osDisk"].(map[string]interface{}); osDisk != nil {
		if md, _ := osDisk["managedDisk"].(map[string]interface{}); md != nil {
			if id := asString(md["id"]); id != "" {
				ids = append(ids, id)
			}
		}
	}
	if dataDisks, _ := storage["dataDisks"].([]interface{}); dataDisks != nil {
		for _, item := range dataDisks {
			dataDisk, _ := item.(map[string]interface{})
			if dataDisk == nil {
				continue
			}
			md, _ := dataDisk["managedDisk"].(map[string]interface{})
			if md == nil {
				continue
			}
			if id := asString(md["id"]); id != "" {
				ids = append(ids, id)
			}
		}
	}

	for _, id := range ids {
		sub, rg, name, ok := disks.ParseID(id)
		if !ok {
			continue
		}
		diskRec, found := disks.Get(h.store, sub, rg, name)
		if !found {
			continue
		}
		diskProps, _ := diskRec["properties"].(map[string]interface{})
		if managedBy := asString(diskProps["managedBy"]); managedBy != "" && managedBy != vmID {
			// Never detach a disk that belongs to another VM.
			continue
		}
		disks.Detach(h.store, sub, rg, name)
	}
}

// vmStorageProfile returns the VM's storageProfile block, creating it when the
// request did not supply one.
func vmStorageProfile(rec map[string]interface{}) map[string]interface{} {
	props := getProps(rec)
	storage, _ := props["storageProfile"].(map[string]interface{})
	if storage == nil {
		storage = map[string]interface{}{}
		props["storageProfile"] = storage
	}
	return storage
}

// vmOSDisk returns the VM's storageProfile.osDisk block, creating it when the
// request did not supply one.
func vmOSDisk(rec map[string]interface{}) map[string]interface{} {
	storage := vmStorageProfile(rec)
	osDisk, _ := storage["osDisk"].(map[string]interface{})
	if osDisk == nil {
		osDisk = map[string]interface{}{}
		storage["osDisk"] = osDisk
	}
	return osDisk
}

// generatedOSDiskName builds `<vmName>_OsDisk_1_<8 hex>`, truncated to the
// Azure disk-name limit while keeping the suffix intact.
func generatedOSDiskName(vmName string) string {
	suffix := osDiskNameSuffix + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	if maxVMName := osDiskNameMaxLen - len(suffix); len(vmName) > maxVMName {
		vmName = vmName[:maxVMName]
	}
	return vmName + suffix
}

func requestedOSDiskSKU(osDisk map[string]interface{}) string {
	if md, _ := osDisk["managedDisk"].(map[string]interface{}); md != nil {
		if sku := asString(md["storageAccountType"]); sku != "" {
			return sku
		}
	}
	if sku := asString(osDisk["storageAccountType"]); sku != "" {
		return sku
	}
	return "Standard_LRS"
}

func requestedOSDiskSize(osDisk map[string]interface{}) float64 {
	if size, ok := asNumber(osDisk["diskSizeGB"]); ok && size > 0 {
		return size
	}
	return 30
}

func requestedOSDiskCreationData(storage, osDisk map[string]interface{}) map[string]interface{} {
	creationData := map[string]interface{}{}
	if option := asString(osDisk["createOption"]); option != "" {
		creationData["createOption"] = option
	} else {
		creationData["createOption"] = "FromImage"
	}
	if image, ok := storage["imageReference"]; ok && image != nil {
		creationData["imageReference"] = image
	}
	return creationData
}

// vmIdentity extracts the subscription, resource group and VM name from a VM
// ARM ID.
func vmIdentity(rec map[string]interface{}) (sub, rg, name string, ok bool) {
	parts := strings.Split(strings.Trim(asString(rec["id"]), "/"), "/")
	if len(parts) != 8 {
		return "", "", "", false
	}
	if !strings.EqualFold(parts[0], "subscriptions") ||
		!strings.EqualFold(parts[2], "resourceGroups") ||
		!strings.EqualFold(parts[4], "providers") ||
		!strings.EqualFold(parts[5], "Microsoft.Compute") ||
		!strings.EqualFold(parts[6], "virtualMachines") {
		return "", "", "", false
	}
	if parts[1] == "" || parts[3] == "" || parts[7] == "" {
		return "", "", "", false
	}
	return parts[1], parts[3], parts[7], true
}

// asString returns v when it is a string and "" otherwise.
func asString(v interface{}) string {
	s, _ := v.(string)
	return s
}

// asNumber returns v as a float64 for the numeric types JSON and the disk
// helpers produce.
func asNumber(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
