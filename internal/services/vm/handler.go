package vm

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/services/network"
	"github.com/moabukar/miniblue/internal/store"
)

// Handler serves Azure Virtual Machines (Microsoft.Compute/virtualMachines)
// endpoints. Each VM is backed by a Docker container; without Docker the
// handler runs in stub mode like ACI: records are fully manageable through
// the API, runtime operations return 409 DockerUnavailable.
type Handler struct {
	store       *store.Store
	dockerAvail bool
}

// NewHandler creates a new VM handler, probing Docker availability once at
// startup. MINIBLUE_DISABLE_DOCKER forces stub mode (used by tests to stay
// deterministic on machines that do have Docker).
func NewHandler(s *store.Store) *Handler {
	h := &Handler{store: s}
	if os.Getenv("MINIBLUE_DISABLE_DOCKER") == "" {
		h.dockerAvail = checkDocker()
	}
	if h.dockerAvail {
		log.Println("[vm] Docker is available – virtual machines will use real containers")
	} else {
		log.Println("[vm] Docker is NOT available – virtual machines will be stub-only")
	}
	return h
}

// Register mounts all VM ARM routes on the given router.
func (h *Handler) Register(r chi.Router) {
	r.Route("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Compute/virtualMachines", func(r chi.Router) {
		r.Get("/", h.ListVMs)
		r.Route("/{vmName}", func(r chi.Router) {
			r.Put("/", h.CreateOrUpdateVM)
			r.Patch("/", h.PatchVM)
			r.Get("/", h.GetVM)
			r.Delete("/", h.DeleteVM)
			r.Post("/start", h.StartVM)
			r.Post("/powerOff", h.PowerOffVM)
			r.Post("/restart", h.RestartVM)
			r.Post("/deallocate", h.DeallocateVM)
			r.Post("/runCommand", h.RunCommand)
			r.Get("/logs", h.GetLogs)
			r.Get("/instanceView", h.GetInstanceView)
			r.Get("/vmSizes", h.ListVMSizes)
			r.Route("/services", func(r chi.Router) {
				r.Get("/", h.ListServices)
				r.Route("/{serviceName}", func(r chi.Router) {
					r.Put("/", h.CreateOrUpdateService)
					r.Get("/", h.GetService)
					r.Delete("/", h.DeleteService)
				})
			})
			r.Route("/extensions", func(r chi.Router) {
				r.Get("/", h.ListExtensions)
				r.Route("/{extensionName}", func(r chi.Router) {
					r.Put("/", h.CreateOrUpdateExtension)
					r.Get("/", h.GetExtension)
					r.Patch("/", h.PatchExtension)
					r.Delete("/", h.DeleteExtension)
				})
			})
		})
	})
	r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.Compute/virtualMachines", h.ListAllVMs)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (h *Handler) getVM(sub, rg, name string) (map[string]interface{}, bool) {
	v, ok := h.store.Get(vmKey(sub, rg, name))
	if !ok {
		return nil, false
	}
	rec, ok := v.(map[string]interface{})
	return rec, ok
}

// refreshVM reconciles the stored power state with the real container state,
// so the API never reports "running" for a container that no longer runs
// (e.g. after an emulator or host restart).
func (h *Handler) refreshVM(rec map[string]interface{}) {
	if !h.dockerAvail {
		return
	}
	mb := getMinibluProps(rec)
	containerName, _ := mb["containerName"].(string)
	if containerName == "" {
		return
	}
	if dockerInspectRunning(containerName) {
		mb["powerState"] = "running"
	} else {
		mb["powerState"] = "stopped"
	}
}

func (h *Handler) serviceRecords(sub, rg, vmName string) []map[string]interface{} {
	items := h.store.ListByPrefix("vmsvc:" + sub + ":" + rg + ":" + vmName + ":")
	out := make([]map[string]interface{}, 0, len(items))
	for _, it := range items {
		if rec, ok := it.(map[string]interface{}); ok {
			out = append(out, rec)
		}
	}
	return out
}

func dockerUnavailable(w http.ResponseWriter) {
	azerr.WriteError(w, http.StatusConflict, "DockerUnavailable",
		"This operation requires the Docker backend, which is not available. Start the Docker daemon and restart miniblue to use real virtual machines.")
}

// decodeBody tolerates an empty request body (everything on a VM is
// defaultable except the name, which comes from the URL).
func decodeBody(r *http.Request) (map[string]interface{}, error) {
	var input map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		if errors.Is(err, io.EOF) {
			return map[string]interface{}{}, nil
		}
		return nil, err
	}
	if input == nil {
		input = map[string]interface{}{}
	}
	return input, nil
}

// ---------------------------------------------------------------------------
// VM CRUD
// ---------------------------------------------------------------------------

func (h *Handler) CreateOrUpdateVM(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vmName")

	input, err := decodeBody(r)
	if err != nil {
		azerr.BadRequest(w, "Could not parse request body: "+err.Error())
		return
	}

	if !h.store.Exists("rg:" + sub + ":" + rg) {
		azerr.WriteError(w, http.StatusNotFound, "ResourceGroupNotFound",
			"Resource group '"+rg+"' could not be found.")
		return
	}

	inProps, _ := input["properties"].(map[string]interface{})
	if inProps == nil {
		inProps = map[string]interface{}{}
	}
	image, err := resolveImage(inProps)
	if err != nil {
		azerr.BadRequest(w, err.Error())
		return
	}

	identityBlock, err := resolveIdentityBlock(h.store, input)
	if err != nil {
		azerr.BadRequest(w, err.Error())
		return
	}

	key := vmKey(sub, rg, name)
	existing, exists := h.getVM(sub, rg, name)

	if exists {
		// Azure PUT semantics: update in place. The backing container is not
		// re-imaged; the same merge path as PATCH refreshes metadata and
		// identity assignments.
		if err := h.updateVM(existing, input); err != nil {
			h.writeUpdateError(w, err)
			return
		}
		h.store.Set(key, existing)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(vmPublicView(existing))
		return
	}

	rec := buildVMRecord(sub, rg, name, input, image)
	if identityBlock != nil {
		rec["identity"] = identityBlock
	}
	secret := uuid.NewString()
	rec["_identitySecret"] = secret
	rec["_networkName"] = vmNetworkName(rg, name)
	appendProvisionEvent(rec, "virtual machine created (image %s)", image)

	if err := h.validateDataDisksForVM(rec, asString(getProps(rec)["vmId"])); err != nil {
		writeDiskError(w, err)
		return
	}
	if err := h.ensureOSDisk(rec); err != nil {
		if !writeDiskError(w, err) {
			azerr.WriteError(w, http.StatusInternalServerError, "InternalServerError", err.Error())
		}
		return
	}
	if err := h.syncDataDisks(asString(getProps(rec)["vmId"]), nil, rec); err != nil {
		if !writeDiskError(w, err) {
			azerr.WriteError(w, http.StatusInternalServerError, "InternalServerError", err.Error())
		}
		return
	}
	getProps(rec)["instanceView"] = instanceViewFor(rec)
	appendProvisionEvent(rec, "OS disk %s attached", osDiskName(rec))

	if h.dockerAvail {
		network := vmNetworkName(rg, name)
		containerName := vmContainerName(rg, name)
		if err := dockerNetworkCreate(network); err != nil {
			azerr.WriteError(w, http.StatusConflict, "ContainerStartFailed", err.Error())
			return
		}
		// A stale container with the same name would make `docker run` fail;
		// remove leftovers from a previous unclean delete first.
		dockerRemove(containerName)
		_, err := dockerRun(runOpts{
			name:    containerName,
			image:   image,
			env:     identityEnv(secret),
			network: network,
			// Portable keep-alive: busybox sleep (alpine) rejects "infinity".
			cmd: []string{"sh", "-c", "while true; do sleep 3600; done"},
		})
		if err != nil {
			// Fail fast and leave nothing behind (unlike ACI's silent stub
			// fallback): a VM whose compute environment never started must
			// not exist.
			dockerNetworkRemove(network)
			azerr.WriteError(w, http.StatusConflict, "ContainerStartFailed", err.Error())
			return
		}
		appendProvisionEvent(rec, "compute environment started (container %s)", containerName)
	}

	h.store.Set(key, rec)
	h.store.Set(secretKey(secret), map[string]interface{}{"vmKey": key})

	h.syncNICLinks(asString(rec["id"]), nil, rec)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(vmPublicView(rec))
}

// PatchVM applies a partial update to an existing VM. Unlike PUT it never
// creates a resource, so a missing VM returns 404.
func (h *Handler) PatchVM(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vmName")

	rec, ok := h.getVM(sub, rg, name)
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", name)
		return
	}

	input, err := decodeBody(r)
	if err != nil {
		azerr.BadRequest(w, "Could not parse request body: "+err.Error())
		return
	}

	if err := h.updateVM(rec, input); err != nil {
		h.writeUpdateError(w, err)
		return
	}
	h.store.Set(vmKey(sub, rg, name), rec)
	json.NewEncoder(w).Encode(vmPublicView(rec))
}

// updateVM applies an update request to a stored VM record using merge
// semantics: maps merge recursively, arrays and scalars replace, and JSON null
// removes a key. Generated/internal fields are never taken from the request.
// Identity and OS-disk references are validated before the record is mutated,
// so a rejected update leaves the VM and its disks untouched.
func (h *Handler) updateVM(rec, input map[string]interface{}) error {
	oldRec := cloneMap(rec)
	vmID := ""
	if props, _ := rec["properties"].(map[string]interface{}); props != nil {
		vmID = asString(props["vmId"])
	}

	identityPresent := false
	var identityBlock map[string]interface{}
	if raw, has := input["identity"]; has {
		identityPresent = true
		if raw != nil {
			block, err := resolveIdentityBlock(h.store, input)
			if err != nil {
				return err
			}
			identityBlock = block
		}
	}

	patch := map[string]interface{}{}
	for _, k := range []string{"location", "tags", "zones", "plan", "extendedLocation"} {
		if v, ok := input[k]; ok {
			patch[k] = v
		}
	}
	var propsPatch map[string]interface{}
	if inProps, ok := input["properties"].(map[string]interface{}); ok {
		propsPatch = cloneMap(inProps)
		for _, k := range []string{"miniblue", "provisioningState", "vmId", "instanceView"} {
			delete(propsPatch, k)
		}
		if osProfile, ok := propsPatch["osProfile"].(map[string]interface{}); ok {
			delete(osProfile, "adminPassword")
		}
		patch["properties"] = propsPatch
	}

	oldDiskID := osDiskManagedDiskID(rec)
	oldOSDisk := osDiskBlock(rec)
	newDiskID, explicitDisk := osDiskIDFromPatch(propsPatch)
	if !explicitDisk {
		newDiskID = oldDiskID
	}
	switchDisk := newDiskID != "" && newDiskID != oldDiskID
	if switchDisk {
		if err := h.validateOSDiskForVM(newDiskID, vmID); err != nil {
			return err
		}
	}

	merged := mergeMaps(rec, patch)
	// Data-disk references are validated before mutating the stored record, so
	// a rejected attachment leaves the VM and every disk untouched.
	if err := h.validateDataDisksForVM(merged, vmID); err != nil {
		return err
	}
	replaceRecord(rec, merged)
	props := getProps(rec)
	if asString(props["vmId"]) == "" {
		props["vmId"] = uuid.NewString()
	}

	if identityPresent {
		if identityBlock == nil {
			delete(rec, "identity")
		} else {
			rec["identity"] = identityBlock
		}
		appendProvisionEvent(rec, "identity assignments updated")
	}

	if switchDisk {
		h.detachOSDiskByID(oldDiskID, vmID)
		resetOSDiskForReplacement(rec)
		if err := h.ensureOSDisk(rec); err != nil {
			return err
		}
		if _, has := rec["_osDiskName"]; has {
			rec["_osDiskName"] = osDiskName(rec)
		}
		appendProvisionEvent(rec, "OS disk %s attached", osDiskName(rec))
	} else if oldDiskID != "" && osDiskManagedDiskID(rec) != oldDiskID {
		// An explicit null must not orphan or regenerate the existing disk, and
		// it must not drop the disk-derived fields either.
		restoreOSDiskBlock(rec, oldOSDisk)
	}

	if err := h.syncDataDisks(asString(getProps(rec)["vmId"]), oldRec, rec); err != nil {
		return err
	}

	h.syncNICLinks(asString(rec["id"]), oldRec, rec)

	h.refreshVM(rec)
	appendProvisionEvent(rec, "virtual machine updated")
	return nil
}

// syncNICLinks reconciles the VM's networkProfile.networkInterfaces references
// with the NIC records: newly referenced NICs are linked to the VM, and NICs
// that the VM no longer references are unlinked. Missing NICs and malformed
// references are ignored, matching the package's permissive reference style.
func (h *Handler) syncNICLinks(vmID string, oldRec, newRec map[string]interface{}) {
	if vmID == "" {
		return
	}
	oldIDs := networkInterfaceIDs(oldRec)
	newIDs := networkInterfaceIDs(newRec)

	for key, id := range newIDs {
		if _, ok := oldIDs[key]; !ok {
			network.AttachVMToNIC(h.store, id, vmID)
		}
	}
	for key, id := range oldIDs {
		if _, ok := newIDs[key]; !ok {
			network.DetachVMFromNIC(h.store, id)
		}
	}
}

// writeDiskError writes the ARM response for a managed-disk validation error
// and reports whether it handled err. Unrelated errors are left to the caller.
func writeDiskError(w http.ResponseWriter, err error) bool {
	var missing *diskNotFoundError
	if errors.As(err, &missing) {
		azerr.WriteError(w, http.StatusNotFound, "DiskNotFound", missing.Error())
		return true
	}
	var inUse *diskInUseError
	if errors.As(err, &inUse) {
		azerr.WriteError(w, http.StatusConflict, "DiskInUse", inUse.Error())
		return true
	}
	return false
}

// writeUpdateError maps update failures onto ARM error responses.
func (h *Handler) writeUpdateError(w http.ResponseWriter, err error) {
	if writeDiskError(w, err) {
		return
	}
	azerr.BadRequest(w, err.Error())
}

func (h *Handler) GetVM(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vmName")

	rec, ok := h.getVM(sub, rg, name)
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", name)
		return
	}
	h.refreshVM(rec)
	h.store.Set(vmKey(sub, rg, name), rec)
	json.NewEncoder(w).Encode(vmPublicView(rec))
}

// GetInstanceView returns the instance view for a VM unwrapped, matching the
// ARM .../instanceView response shape.
func (h *Handler) GetInstanceView(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vmName")

	rec, ok := h.getVM(sub, rg, name)
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", name)
		return
	}
	h.refreshVM(rec)
	h.store.Set(vmKey(sub, rg, name), rec)
	json.NewEncoder(w).Encode(instanceViewFor(rec))
}

// ListVMSizes returns the static catalogue of VM sizes the provider may choose
// from. miniblue does not enforce sizes, so a broad superset is returned.
func (h *Handler) ListVMSizes(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{"value": vmSizesCatalog()})
}

func (h *Handler) listVMs(w http.ResponseWriter, prefix string) {
	items := h.store.ListByPrefix(prefix)
	value := make([]interface{}, 0, len(items))
	for _, it := range items {
		rec, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		h.refreshVM(rec)
		value = append(value, vmPublicView(rec))
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"value": value})
}

func (h *Handler) ListVMs(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	h.listVMs(w, "vm:"+sub+":"+rg+":")
}

func (h *Handler) ListAllVMs(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	h.listVMs(w, "vm:"+sub+":")
}

func (h *Handler) DeleteVM(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vmName")

	rec, ok := h.getVM(sub, rg, name)
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", name)
		return
	}

	if h.dockerAvail {
		for _, svc := range h.serviceRecords(sub, rg, name) {
			if cn, _ := svc["_containerName"].(string); cn != "" {
				dockerRemove(cn)
			}
		}
		dockerRemove(vmContainerName(rg, name))
		dockerNetworkRemove(vmNetworkName(rg, name))
	}

	h.store.DeleteByPrefix("vmsvc:" + sub + ":" + rg + ":" + name + ":")
	if secret, _ := rec["_identitySecret"].(string); secret != "" {
		h.store.Delete(secretKey(secret))
	}
	h.detachVMDisks(rec)
	h.syncNICLinks(asString(rec["id"]), rec, nil)
	h.store.Delete(vmKey(sub, rg, name))

	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Power actions
// ---------------------------------------------------------------------------

func (h *Handler) powerAction(w http.ResponseWriter, r *http.Request, action string) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vmName")

	rec, ok := h.getVM(sub, rg, name)
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", name)
		return
	}
	if !h.dockerAvail {
		// Without Docker there is no runtime to control, but power actions are
		// still meaningful metadata: providers power-cycle VMs during updates
		// and Docker-free CI must not fail those calls. Record the requested
		// state and return the public view; operations that genuinely need a
		// container (runCommand, logs, services, SSH) keep their 409.
		switch action {
		case "start", "restart":
			getMinibluProps(rec)["powerState"] = "running"
		case "powerOff", "deallocate":
			getMinibluProps(rec)["powerState"] = "stopped"
		}
		appendProvisionEvent(rec, "power action %s completed (state %s)", action, powerState(rec))
		h.store.Set(vmKey(sub, rg, name), rec)
		json.NewEncoder(w).Encode(vmPublicView(rec))
		return
	}

	containerName := vmContainerName(rg, name)
	services := h.serviceRecords(sub, rg, name)

	stop := func() error {
		for _, svc := range services {
			if cn, _ := svc["_containerName"].(string); cn != "" {
				dockerStop(cn)
			}
		}
		return dockerStop(containerName)
	}
	start := func() error {
		if err := dockerStart(containerName); err != nil {
			return err
		}
		for _, svc := range services {
			if cn, _ := svc["_containerName"].(string); cn != "" {
				dockerStart(cn)
			}
		}
		return nil
	}

	h.refreshVM(rec)
	var err error
	switch action {
	case "start":
		if powerState(rec) != "running" {
			err = start()
		}
	case "powerOff", "deallocate":
		if powerState(rec) != "stopped" {
			err = stop()
		}
	case "restart":
		stop()
		err = start()
	}
	if err != nil {
		azerr.WriteError(w, http.StatusConflict, "ContainerStartFailed", err.Error())
		return
	}

	h.refreshVM(rec)
	appendProvisionEvent(rec, "power action %s completed (state %s)", action, powerState(rec))
	h.store.Set(vmKey(sub, rg, name), rec)
	json.NewEncoder(w).Encode(vmPublicView(rec))
}

func (h *Handler) StartVM(w http.ResponseWriter, r *http.Request) {
	h.powerAction(w, r, "start")
}

func (h *Handler) PowerOffVM(w http.ResponseWriter, r *http.Request) {
	h.powerAction(w, r, "powerOff")
}

func (h *Handler) RestartVM(w http.ResponseWriter, r *http.Request) {
	h.powerAction(w, r, "restart")
}

func (h *Handler) DeallocateVM(w http.ResponseWriter, r *http.Request) {
	h.powerAction(w, r, "deallocate")
}
