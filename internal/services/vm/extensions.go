package vm

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/azerr"
)

// ---------------------------------------------------------------------------
// Virtual machine extensions
//
// Extensions are metadata-only: miniblue records what the provider asked for
// but never runs CustomScript or any other extension handler. Protected
// settings are kept for fidelity but never returned, matching Azure.
// ---------------------------------------------------------------------------

const extensionResourceType = "Microsoft.Compute/virtualMachines/extensions"

// extensionPropertyNames is the set of extension handler properties miniblue
// echoes. Unknown blocks are ignored, and protectedSettings is deliberately
// absent so it can never reach a response.
var extensionPropertyNames = []string{
	"publisher",
	"type",
	"typeHandlerVersion",
	"autoUpgradeMinorVersion",
	"enableAutomaticUpgrade",
	"settings",
	"suppressFailures",
	"provisionAfterExtensions",
	"protectedSettingsFromKeyVault",
}

func extensionKey(sub, rg, vmName, name string) string {
	return "vmext:" + sub + ":" + rg + ":" + vmName + ":" + name
}

func (h *Handler) extRecords(sub, rg, vmName string) []map[string]interface{} {
	items := h.store.ListByPrefix("vmext:" + sub + ":" + rg + ":" + vmName + ":")
	out := make([]map[string]interface{}, 0, len(items))
	for _, it := range items {
		if rec, ok := it.(map[string]interface{}); ok {
			out = append(out, rec)
		}
	}
	return out
}

// extensionPropertyPatch extracts the handler properties from a request body.
func extensionPropertyPatch(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	out := make(map[string]interface{}, len(extensionPropertyNames))
	for _, k := range extensionPropertyNames {
		if v, ok := src[k]; ok {
			out[k] = cloneValue(v)
		}
	}
	return out
}

// buildExtensionRecord merges an extension request into a newly created or
// existing stored record. Generated/read-only fields are always recomputed,
// and protected settings are stored separately from the public properties.
func buildExtensionRecord(sub, rg, vmName, extName string, vmRec, existing, input map[string]interface{}) map[string]interface{} {
	rec := existing
	if rec == nil {
		rec = map[string]interface{}{
			"id":         vmARMID(sub, rg, vmName) + "/extensions/" + extName,
			"name":       extName,
			"type":       extensionResourceType,
			"properties": map[string]interface{}{},
		}
	}

	// Extension bodies may omit location; inherit it from the VM.
	location := asString(input["location"])
	if location == "" {
		location = asString(rec["location"])
	}
	if location == "" {
		location = asString(vmRec["location"])
	}
	if location != "" {
		rec["location"] = location
	}

	if inTags, ok := input["tags"].(map[string]interface{}); ok {
		existingTags, _ := rec["tags"].(map[string]interface{})
		rec["tags"] = mergeMaps(existingTags, inTags)
	} else if _, ok := rec["tags"]; !ok {
		rec["tags"] = map[string]interface{}{}
	}

	props, _ := rec["properties"].(map[string]interface{})
	if props == nil {
		props = map[string]interface{}{}
	}
	inProps, _ := input["properties"].(map[string]interface{})
	if patch := extensionPropertyPatch(inProps); patch != nil {
		props = mergeMaps(props, patch)
	}
	if props["settings"] == nil {
		props["settings"] = map[string]interface{}{}
	}
	if props["provisionAfterExtensions"] == nil {
		props["provisionAfterExtensions"] = []interface{}{}
	}
	if _, ok := props["autoUpgradeMinorVersion"]; !ok {
		props["autoUpgradeMinorVersion"] = false
	}
	if _, ok := props["enableAutomaticUpgrade"]; !ok {
		props["enableAutomaticUpgrade"] = false
	}
	if _, ok := props["suppressFailures"]; !ok {
		props["suppressFailures"] = false
	}
	props["provisioningState"] = "Succeeded"
	// instanceView is derived on every read, and protectedSettings must never
	// live in the public properties.
	delete(props, "instanceView")
	delete(props, "protectedSettings")
	rec["properties"] = props

	// Write-only protected settings: keep them off the public properties map.
	if inProps != nil {
		if raw, ok := inProps["protectedSettings"]; ok {
			if raw == nil {
				delete(rec, "_protectedSettings")
			} else {
				rec["_protectedSettings"] = cloneValue(raw)
			}
		}
	}
	return rec
}

// extensionInstanceView builds the success status Azure reports for an
// extension handler.
func extensionInstanceView(rec map[string]interface{}) map[string]interface{} {
	props, _ := rec["properties"].(map[string]interface{})
	return map[string]interface{}{
		"name":               rec["name"],
		"type":               asString(props["type"]),
		"typeHandlerVersion": asString(props["typeHandlerVersion"]),
		"status": map[string]interface{}{
			"code":          "ProvisioningState/succeeded",
			"level":         "Info",
			"displayStatus": "Provisioning succeeded",
		},
	}
}

// extensionPublicView is publicView plus the derived instance view. Protected
// settings are stripped defensively in case an older record stored them in the
// properties block.
func extensionPublicView(rec map[string]interface{}) map[string]interface{} {
	out := publicView(rec)
	props, _ := rec["properties"].(map[string]interface{})
	viewProps := make(map[string]interface{}, len(props)+1)
	for k, v := range props {
		if k == "protectedSettings" {
			continue
		}
		viewProps[k] = v
	}
	viewProps["instanceView"] = extensionInstanceView(rec)
	out["properties"] = viewProps
	return out
}

func (h *Handler) CreateOrUpdateExtension(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	vmName := chi.URLParam(r, "vmName")
	extName := chi.URLParam(r, "extensionName")

	vmRec, ok := h.getVM(sub, rg, vmName)
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", vmName)
		return
	}
	input, err := decodeBody(r)
	if err != nil {
		azerr.BadRequest(w, "Could not parse request body: "+err.Error())
		return
	}

	key := extensionKey(sub, rg, vmName, extName)
	var existing map[string]interface{}
	if v, ok := h.store.Get(key); ok {
		existing, _ = v.(map[string]interface{})
	}
	rec := buildExtensionRecord(sub, rg, vmName, extName, vmRec, existing, input)
	h.store.Set(key, rec)

	verb := "created"
	if existing != nil {
		verb = "updated"
	}
	appendProvisionEvent(vmRec, "extension %s %s (%s)", extName, verb, asString(getProps(rec)["type"]))
	h.store.Set(vmKey(sub, rg, vmName), vmRec)

	if existing != nil {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(extensionPublicView(rec))
}

func (h *Handler) GetExtension(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	vmName := chi.URLParam(r, "vmName")
	extName := chi.URLParam(r, "extensionName")

	if _, ok := h.getVM(sub, rg, vmName); !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", vmName)
		return
	}
	v, ok := h.store.Get(extensionKey(sub, rg, vmName, extName))
	if !ok {
		azerr.NotFound(w, extensionResourceType, extName)
		return
	}
	rec, _ := v.(map[string]interface{})
	json.NewEncoder(w).Encode(extensionPublicView(rec))
}

func (h *Handler) ListExtensions(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	vmName := chi.URLParam(r, "vmName")

	if _, ok := h.getVM(sub, rg, vmName); !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", vmName)
		return
	}
	value := make([]interface{}, 0)
	for _, rec := range h.extRecords(sub, rg, vmName) {
		value = append(value, extensionPublicView(rec))
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"value": value})
}

func (h *Handler) PatchExtension(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	vmName := chi.URLParam(r, "vmName")
	extName := chi.URLParam(r, "extensionName")

	vmRec, ok := h.getVM(sub, rg, vmName)
	if !ok {
		azerr.NotFound(w, "Microsoft.Compute/virtualMachines", vmName)
		return
	}
	key := extensionKey(sub, rg, vmName, extName)
	v, ok := h.store.Get(key)
	if !ok {
		azerr.NotFound(w, extensionResourceType, extName)
		return
	}
	rec, _ := v.(map[string]interface{})
	input, err := decodeBody(r)
	if err != nil {
		azerr.BadRequest(w, "Could not parse request body: "+err.Error())
		return
	}
	rec = buildExtensionRecord(sub, rg, vmName, extName, vmRec, rec, input)
	h.store.Set(key, rec)
	json.NewEncoder(w).Encode(extensionPublicView(rec))
}

func (h *Handler) DeleteExtension(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	vmName := chi.URLParam(r, "vmName")
	extName := chi.URLParam(r, "extensionName")

	key := extensionKey(sub, rg, vmName, extName)
	if _, ok := h.store.Get(key); !ok {
		azerr.NotFound(w, extensionResourceType, extName)
		return
	}
	h.store.Delete(key)
	w.WriteHeader(http.StatusAccepted)
}
