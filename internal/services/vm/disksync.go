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

// diskInUseError reports an explicit managed-disk ID that is already attached to
// a different virtual machine.
type diskInUseError struct {
	id        string
	managedBy string
}

func (e *diskInUseError) Error() string {
	return "Disk '" + e.id + "' is already attached to virtual machine '" + e.managedBy + "'."
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
			diskProps, _ := diskRec["properties"].(map[string]interface{})
			if managedBy := asString(diskProps["managedBy"]); managedBy != "" && managedBy != vmID {
				return &diskInUseError{id: id, managedBy: managedBy}
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

// osDiskBlock returns a deep copy of the VM's storageProfile.osDisk block
// without creating one. It is used to snapshot the block before an update so an
// explicit null can restore it unchanged.
func osDiskBlock(rec map[string]interface{}) map[string]interface{} {
	props, _ := rec["properties"].(map[string]interface{})
	if props == nil {
		return nil
	}
	storage, _ := props["storageProfile"].(map[string]interface{})
	if storage == nil {
		return nil
	}
	osDisk, _ := storage["osDisk"].(map[string]interface{})
	return cloneMap(osDisk)
}

// osDiskManagedDiskID returns the managed disk ID referenced by the VM's OS
// disk, or "" when the VM has no managed OS disk.
func osDiskManagedDiskID(rec map[string]interface{}) string {
	props, _ := rec["properties"].(map[string]interface{})
	if props == nil {
		return ""
	}
	storage, _ := props["storageProfile"].(map[string]interface{})
	if storage == nil {
		return ""
	}
	osDisk, _ := storage["osDisk"].(map[string]interface{})
	if osDisk == nil {
		return ""
	}
	md, _ := osDisk["managedDisk"].(map[string]interface{})
	return asString(md["id"])
}

// osDiskIDFromPatch extracts the managed disk ID an update request explicitly
// sets on storageProfile.osDisk. The second return value reports whether the
// patch mentions the managed disk at all; an explicit JSON null is reported as
// an empty ID with true so the caller can distinguish it from an omitted block.
func osDiskIDFromPatch(props map[string]interface{}) (string, bool) {
	if props == nil {
		return "", false
	}
	storage, _ := props["storageProfile"].(map[string]interface{})
	if storage == nil {
		return "", false
	}
	osDisk, _ := storage["osDisk"].(map[string]interface{})
	if osDisk == nil {
		return "", false
	}
	md, has := osDisk["managedDisk"]
	if !has {
		return "", false
	}
	if md == nil {
		return "", true
	}
	m, ok := md.(map[string]interface{})
	if !ok {
		return "", true
	}
	id, has := m["id"]
	if !has {
		return "", false
	}
	s, _ := id.(string)
	return s, true
}

// validateOSDiskForVM checks that an explicit OS disk exists and is not owned by
// another VM. It never mutates either record.
func (h *Handler) validateOSDiskForVM(id, vmID string) error {
	sub, rg, name, ok := disks.ParseID(id)
	if !ok {
		return &diskNotFoundError{id: id}
	}
	diskRec, found := disks.Get(h.store, sub, rg, name)
	if !found {
		return &diskNotFoundError{id: id}
	}
	diskProps, _ := diskRec["properties"].(map[string]interface{})
	if managedBy := asString(diskProps["managedBy"]); managedBy != "" && managedBy != vmID {
		return &diskInUseError{id: id, managedBy: managedBy}
	}
	return nil
}

// detachOSDiskByID detaches a disk from the VM, unless it belongs to another
// VM (in which case it is left alone).
func (h *Handler) detachOSDiskByID(id, vmID string) {
	if id == "" {
		return
	}
	sub, rg, name, ok := disks.ParseID(id)
	if !ok {
		return
	}
	diskRec, found := disks.Get(h.store, sub, rg, name)
	if !found {
		return
	}
	diskProps, _ := diskRec["properties"].(map[string]interface{})
	if managedBy := asString(diskProps["managedBy"]); managedBy != "" && managedBy != vmID {
		return
	}
	disks.Detach(h.store, sub, rg, name)
}

// resetOSDiskForReplacement drops fields derived from the previous OS disk so
// applyOSDisk refills them from the newly attached disk. Caller-supplied
// caching/createOption and the new managed-disk ID are kept.
func resetOSDiskForReplacement(rec map[string]interface{}) {
	osDisk := vmOSDisk(rec)
	delete(osDisk, "name")
	delete(osDisk, "diskSizeGB")
	delete(osDisk, "osType")
	if md, ok := osDisk["managedDisk"].(map[string]interface{}); ok {
		delete(md, "storageAccountType")
	}
}

// restoreOSDiskBlock puts the previous storageProfile.osDisk block back after an
// update removed it (or its managed-disk reference) with an explicit null. An
// empty managed-disk ID means "keep the existing disk", never "generate a new
// one", so every disk-derived field (name, size, OS type, SKU, caching and
// create option) must be restored, not just the reference.
func restoreOSDiskBlock(rec, oldOSDisk map[string]interface{}) {
	if oldOSDisk == nil {
		return
	}
	vmStorageProfile(rec)["osDisk"] = cloneMap(oldOSDisk)
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
