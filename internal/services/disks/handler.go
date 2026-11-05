package disks

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/store"
)

const diskType = "Microsoft.Compute/disks"

const bytesPerGiB = float64(1024 * 1024 * 1024)

// Handler serves the Microsoft.Compute/disks ARM resource.
type Handler struct {
	store *store.Store
}

func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s}
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Compute/disks", func(r chi.Router) {
		r.Get("/", h.List)
		r.Route("/{diskName}", func(r chi.Router) {
			r.Put("/", h.CreateOrUpdate)
			r.Get("/", h.Get)
			r.Patch("/", h.Patch)
			r.Delete("/", h.Delete)
		})
	})
	r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.Compute/disks", h.ListInSubscription)
}

// ---------- store helpers ----------

// Key returns the store key for a managed disk.
func Key(sub, rg, name string) string {
	return "disk:" + sub + ":" + rg + ":" + name
}

// Get reads a managed disk record from the store. The returned record retains
// internal fields such as _vmOwned; HTTP handlers strip them before responding.
func Get(s *store.Store, sub, rg, name string) (map[string]interface{}, bool) {
	v, ok := s.Get(Key(sub, rg, name))
	if !ok {
		return nil, false
	}
	rec, ok := v.(map[string]interface{})
	return rec, ok
}

// Set writes a managed disk record to the store.
func Set(s *store.Store, sub, rg, name string, rec map[string]interface{}) {
	s.Set(Key(sub, rg, name), rec)
}

// Attach links a disk to a virtual machine. It reports false when the disk
// does not exist.
func Attach(s *store.Store, sub, rg, name, vmID string) bool {
	rec, ok := Get(s, sub, rg, name)
	if !ok {
		return false
	}
	rec = mergeMaps(rec, map[string]interface{}{
		"properties": map[string]interface{}{
			"managedBy": vmID,
			"diskState": "Attached",
		},
	})
	Set(s, sub, rg, name, rec)
	return true
}

// Detach clears a disk's virtual machine link. It reports false when the disk
// does not exist.
func Detach(s *store.Store, sub, rg, name string) bool {
	rec, ok := Get(s, sub, rg, name)
	if !ok {
		return false
	}
	rec = mergeMaps(rec, map[string]interface{}{
		"properties": map[string]interface{}{
			"managedBy": "",
			"diskState": "Unattached",
		},
	})
	Set(s, sub, rg, name, rec)
	return true
}

// ParseID extracts the subscription, resource group and disk name from a
// managed disk ARM ID. Matching is case-insensitive.
func ParseID(id string) (sub, rg, name string, ok bool) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(id), "/"), "/")
	if len(parts) != 8 {
		return "", "", "", false
	}
	if !strings.EqualFold(parts[0], "subscriptions") ||
		!strings.EqualFold(parts[2], "resourceGroups") ||
		!strings.EqualFold(parts[4], "providers") ||
		!strings.EqualFold(parts[5], "Microsoft.Compute") ||
		!strings.EqualFold(parts[6], "disks") {
		return "", "", "", false
	}
	if parts[1] == "" || parts[3] == "" || parts[7] == "" {
		return "", "", "", false
	}
	return parts[1], parts[3], parts[7], true
}

// NewRecord builds a complete managed disk record from a request body (or from
// an existing record merged with one). Generated and read-only fields already
// present on input are preserved; missing Azure defaults are filled in.
func NewRecord(sub, rg, name string, input map[string]interface{}) map[string]interface{} {
	rec := mergeMaps(nil, input)

	rec["id"] = "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.Compute/disks/" + name
	rec["name"] = name
	rec["type"] = diskType
	if stringVal(rec["location"]) == "" {
		rec["location"] = "eastus"
	}
	if tags, ok := rec["tags"].(map[string]interface{}); !ok || tags == nil {
		rec["tags"] = map[string]interface{}{}
	}
	rec["etag"] = "W/\"miniblue\""

	props, _ := rec["properties"].(map[string]interface{})
	props = cloneMap(props)
	if props == nil {
		props = map[string]interface{}{}
	}

	props["provisioningState"] = "Succeeded"

	// timeCreated is generated once and never changes.
	if stringVal(props["timeCreated"]) == "" {
		props["timeCreated"] = time.Now().UTC().Format(time.RFC3339)
	}

	sizeGB, ok := numberVal(props["diskSizeGB"])
	if !ok || sizeGB <= 0 {
		sizeGB = 30
	}
	props["diskSizeGB"] = sizeGB
	if _, ok := numberVal(props["diskSizeBytes"]); !ok {
		props["diskSizeBytes"] = sizeGB * bytesPerGiB
	}

	creationData, _ := props["creationData"].(map[string]interface{})
	creationData = cloneMap(creationData)
	if creationData == nil {
		creationData = map[string]interface{}{}
	}
	if stringVal(creationData["createOption"]) == "" {
		creationData["createOption"] = "Empty"
	}
	props["creationData"] = creationData

	if stringVal(props["diskState"]) == "" {
		props["diskState"] = "Unattached"
	}
	if _, ok := props["managedBy"]; !ok {
		props["managedBy"] = ""
	}
	if _, ok := numberVal(props["diskIOPSReadWrite"]); !ok {
		props["diskIOPSReadWrite"] = float64(500)
	}
	if _, ok := numberVal(props["diskMBpsReadWrite"]); !ok {
		props["diskMBpsReadWrite"] = float64(60)
	}
	if stringVal(props["networkAccessPolicy"]) == "" {
		props["networkAccessPolicy"] = "AllowAll"
	}
	if stringVal(props["publicNetworkAccess"]) == "" {
		props["publicNetworkAccess"] = "Enabled"
	}

	encryption, _ := props["encryption"].(map[string]interface{})
	encryption = cloneMap(encryption)
	if encryption == nil {
		encryption = map[string]interface{}{}
	}
	if stringVal(encryption["type"]) == "" {
		encryption["type"] = "EncryptionAtRestWithPlatformKey"
	}
	props["encryption"] = encryption
	rec["properties"] = props

	sku, _ := rec["sku"].(map[string]interface{})
	sku = cloneMap(sku)
	if sku == nil {
		sku = map[string]interface{}{}
	}
	if stringVal(sku["name"]) == "" {
		sku["name"] = "Standard_LRS"
	}
	if stringVal(sku["tier"]) == "" {
		sku["tier"] = skuTier(stringVal(sku["name"]))
	}
	rec["sku"] = sku

	if _, ok := rec["_vmOwned"]; !ok {
		rec["_vmOwned"] = false
	}
	return rec
}

// skuTier derives the Azure SKU tier from a disk SKU name.
func skuTier(name string) string {
	if i := strings.Index(name, "_"); i > 0 {
		return name[:i]
	}
	return name
}

// ---------- HTTP handlers ----------

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	writeList(w, h.store.ListByPrefix("disk:"+sub+":"+rg+":"))
}

func (h *Handler) ListInSubscription(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	writeList(w, h.store.ListByPrefix("disk:"+sub+":"))
}

func writeList(w http.ResponseWriter, items []interface{}) {
	value := make([]interface{}, 0, len(items))
	for _, item := range items {
		if rec, ok := item.(map[string]interface{}); ok {
			value = append(value, publicRecord(rec))
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"value": value})
}

func (h *Handler) CreateOrUpdate(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "diskName")

	input, ok := decodeBody(w, r)
	if !ok {
		return
	}

	existing, exists := Get(h.store, sub, rg, name)
	rec := buildRecord(sub, rg, name, input, existing)
	Set(h.store, sub, rg, name, rec)

	status := http.StatusCreated
	if exists {
		status = http.StatusOK
	}
	writeDisk(w, status, rec)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "diskName")

	rec, ok := Get(h.store, sub, rg, name)
	if !ok {
		azerr.NotFound(w, diskType, name)
		return
	}
	writeDisk(w, http.StatusOK, rec)
}

func (h *Handler) Patch(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "diskName")

	existing, ok := Get(h.store, sub, rg, name)
	if !ok {
		azerr.NotFound(w, diskType, name)
		return
	}

	input, ok := decodeBody(w, r)
	if !ok {
		return
	}

	rec := buildRecord(sub, rg, name, input, existing)
	Set(h.store, sub, rg, name, rec)
	writeDisk(w, http.StatusOK, rec)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "diskName")

	rec, ok := Get(h.store, sub, rg, name)
	if !ok {
		azerr.NotFound(w, diskType, name)
		return
	}
	props, _ := rec["properties"].(map[string]interface{})
	if vmID := stringVal(props["managedBy"]); vmID != "" {
		azerr.WriteError(w, http.StatusConflict, "DiskAttached",
			"Disk '"+name+"' is attached to virtual machine '"+vmID+"' and cannot be deleted.")
		return
	}

	h.store.Delete(Key(sub, rg, name))
	w.WriteHeader(http.StatusAccepted)
}

// buildRecord merges a request over the stored record and re-derives
// diskSizeBytes when the caller changes diskSizeGB without supplying bytes.
func buildRecord(sub, rg, name string, input, existing map[string]interface{}) map[string]interface{} {
	merged := mergeMaps(existing, input)
	if inputProps, ok := input["properties"].(map[string]interface{}); ok {
		if _, ok := inputProps["diskSizeGB"]; ok {
			if _, explicit := inputProps["diskSizeBytes"]; !explicit {
				if mergedProps, ok := merged["properties"].(map[string]interface{}); ok {
					delete(mergedProps, "diskSizeBytes")
				}
			}
		}
	}
	return NewRecord(sub, rg, name, merged)
}

func writeDisk(w http.ResponseWriter, status int, rec map[string]interface{}) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(publicRecord(rec))
}

// publicRecord returns a response-safe deep copy with internal fields removed.
func publicRecord(rec map[string]interface{}) map[string]interface{} {
	out := cloneMap(rec)
	stripInternalFields(out)
	return out
}

func decodeBody(w http.ResponseWriter, r *http.Request) (map[string]interface{}, bool) {
	var input map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		if errors.Is(err, io.EOF) {
			return map[string]interface{}{}, true
		}
		azerr.BadRequest(w, "The request body could not be parsed as JSON.")
		return nil, false
	}
	if input == nil {
		input = map[string]interface{}{}
	}
	return input, true
}

// ---------- small helpers ----------

func stringVal(v interface{}) string {
	s, _ := v.(string)
	return s
}

func numberVal(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// cloneValue deep-copies maps and slices so merges never alias stored records.
func cloneValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		return cloneMap(t)
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, item := range t {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return v
	}
}

func cloneMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = cloneValue(v)
	}
	return out
}

// mergeMaps recursively merges patch into dst. Maps merge, arrays and scalars
// replace, and a JSON null removes the key. The inputs are never mutated.
func mergeMaps(dst, patch map[string]interface{}) map[string]interface{} {
	out := cloneMap(dst)
	if out == nil {
		out = map[string]interface{}{}
	}
	for k, v := range patch {
		if v == nil {
			delete(out, k)
			continue
		}
		if pm, ok := v.(map[string]interface{}); ok {
			dm, _ := out[k].(map[string]interface{})
			out[k] = mergeMaps(dm, pm)
			continue
		}
		out[k] = cloneValue(v)
	}
	return out
}

// stripInternalFields removes miniblue-internal "_"-prefixed keys.
func stripInternalFields(m map[string]interface{}) {
	for k, v := range m {
		if strings.HasPrefix(k, "_") {
			delete(m, k)
			continue
		}
		switch t := v.(type) {
		case map[string]interface{}:
			stripInternalFields(t)
		case []interface{}:
			for _, item := range t {
				if child, ok := item.(map[string]interface{}); ok {
					stripInternalFields(child)
				}
			}
		}
	}
}
