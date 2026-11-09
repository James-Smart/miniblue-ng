package platform

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/store"
)

const diagnosticSettingsType = "Microsoft.Insights/diagnosticSettings"

const diagnosticSettingsMarker = "/providers/" + diagnosticSettingsType

type resourceSpec struct {
	namespace    string
	resourceType string
}

var plainResources = []resourceSpec{
	{namespace: "Microsoft.Network", resourceType: "bastionHosts"},
	{namespace: "Microsoft.OperationalInsights", resourceType: "workspaces"},
	{namespace: "Microsoft.Maintenance", resourceType: "maintenanceConfigurations"},
}

// Handler provides minimal ARM create/read/list/delete support for platform
// resources that miniblue does not otherwise model. Request bodies are stored
// verbatim and echoed back with the ARM identity and a succeeded state.
type Handler struct {
	store *store.Store
}

func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s}
}

func (h *Handler) Register(r chi.Router) {
	for _, spec := range plainResources {
		spec := spec
		r.Route("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/"+spec.namespace+"/"+spec.resourceType, func(r chi.Router) {
			r.Get("/", func(w http.ResponseWriter, req *http.Request) {
				h.list(w, req, spec)
			})
			r.Route("/{resourceName}", func(r chi.Router) {
				r.Put("/", func(w http.ResponseWriter, req *http.Request) {
					h.createOrUpdate(w, req, spec)
				})
				r.Get("/", func(w http.ResponseWriter, req *http.Request) {
					h.get(w, req, spec)
				})
				r.Delete("/", func(w http.ResponseWriter, req *http.Request) {
					h.delete(w, req, spec)
				})
				if spec.namespace == "Microsoft.OperationalInsights" && spec.resourceType == "workspaces" {
					r.Post("/sharedKeys", func(w http.ResponseWriter, req *http.Request) {
						h.sharedKeys(w, req, spec)
					})
				}
			})
		})
	}

	// Register these as plain routes rather than nested subrouters so they
	// cannot shadow the existing workspaces routes under the same provider.
	r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.OperationalInsights/deletedWorkspaces", h.listDeletedWorkspaces)
	r.Get("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.OperationalInsights/deletedWorkspaces", h.listDeletedWorkspaces)

	// Diagnostic settings are sub-resources and can hang off any resource ID.
	// A chi wildcard cannot catch these when a static route already matched the
	// parent resource prefix but not its trailing segments, so handle the
	// diagnostic-settings path from the router's not-found handler.
	r.NotFound(h.diagnosticSettingsOrNotFound)
}

func (h *Handler) diagnosticSettingsOrNotFound(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(strings.ToLower(r.URL.Path), strings.ToLower(diagnosticSettingsMarker)) {
		http.NotFound(w, r)
		return
	}
	h.diagnosticSettings(w, r)
}

func (h *Handler) resourcePrefix(spec resourceSpec, sub, rg string) string {
	return "platform:" + spec.namespace + "/" + spec.resourceType + ":" + sub + ":" + rg + ":"
}

func (h *Handler) resourceKey(spec resourceSpec, sub, rg, name string) string {
	return h.resourcePrefix(spec, sub, rg) + name
}

func (h *Handler) createOrUpdate(w http.ResponseWriter, r *http.Request, spec resourceSpec) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "resourceName")

	body, ok := decodeBody(w, r)
	if !ok {
		return
	}

	id := "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/" + spec.namespace + "/" + spec.resourceType + "/" + name
	record := resourceResponse(body, id, name, spec.namespace+"/"+spec.resourceType)

	key := h.resourceKey(spec, sub, rg, name)
	_, exists := h.store.Get(key)
	h.store.Set(key, record)

	if exists {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(record)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, spec resourceSpec) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "resourceName")

	value, ok := h.store.Get(h.resourceKey(spec, sub, rg, name))
	if !ok {
		azerr.NotFound(w, spec.namespace+"/"+spec.resourceType, name)
		return
	}
	json.NewEncoder(w).Encode(value)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, spec resourceSpec) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "resourceName")

	if !h.store.Delete(h.resourceKey(spec, sub, rg, name)) {
		azerr.NotFound(w, spec.namespace+"/"+spec.resourceType, name)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, spec resourceSpec) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	items := h.store.ListByPrefix(h.resourcePrefix(spec, sub, rg))
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

// listDeletedWorkspaces reports no soft-deleted workspaces: miniblue deletes
// workspaces immediately, so there is nothing to recover.
func (h *Handler) listDeletedWorkspaces(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{"value": []interface{}{}})
}

func (h *Handler) sharedKeys(w http.ResponseWriter, r *http.Request, spec resourceSpec) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "resourceName")

	if !h.store.Exists(h.resourceKey(spec, sub, rg, name)) {
		azerr.NotFound(w, spec.namespace+"/"+spec.resourceType, name)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"primarySharedKey":   sharedKey(sub, rg, name, "primary"),
		"secondarySharedKey": sharedKey(sub, rg, name, "secondary"),
	})
}

func sharedKey(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func (h *Handler) diagnosticSettings(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	index := strings.LastIndex(strings.ToLower(path), strings.ToLower(diagnosticSettingsMarker))
	if index < 0 {
		azerr.NotFound(w, diagnosticSettingsType, "")
		return
	}

	parentID := path[:index]
	name := strings.TrimPrefix(path[index+len(diagnosticSettingsMarker):], "/")
	if strings.Contains(name, "/") {
		azerr.NotFound(w, diagnosticSettingsType, name)
		return
	}

	switch r.Method {
	case http.MethodPut:
		if name == "" {
			azerr.NotFound(w, diagnosticSettingsType, name)
			return
		}
		h.createOrUpdateDiagnosticSetting(w, r, parentID, name)
	case http.MethodGet:
		if name == "" {
			h.listDiagnosticSettings(w, parentID)
			return
		}
		h.getDiagnosticSetting(w, parentID, name)
	case http.MethodDelete:
		if name == "" {
			azerr.NotFound(w, diagnosticSettingsType, name)
			return
		}
		h.deleteDiagnosticSetting(w, parentID, name)
	default:
		azerr.MethodNotAllowed(w, r.Method)
	}
}

func (h *Handler) diagnosticSettingsPrefix(parentID string) string {
	return "platform:diagnosticSettings:" + parentID + ":"
}

func (h *Handler) diagnosticSettingKey(parentID, name string) string {
	return h.diagnosticSettingsPrefix(parentID) + name
}

func (h *Handler) createOrUpdateDiagnosticSetting(w http.ResponseWriter, r *http.Request, parentID, name string) {
	body, ok := decodeBody(w, r)
	if !ok {
		return
	}

	id := parentID + diagnosticSettingsMarker + "/" + name
	record := resourceResponse(body, id, name, diagnosticSettingsType)

	key := h.diagnosticSettingKey(parentID, name)
	_, exists := h.store.Get(key)
	h.store.Set(key, record)

	if exists {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(record)
}

func (h *Handler) getDiagnosticSetting(w http.ResponseWriter, parentID, name string) {
	value, ok := h.store.Get(h.diagnosticSettingKey(parentID, name))
	if !ok {
		azerr.NotFound(w, diagnosticSettingsType, name)
		return
	}
	json.NewEncoder(w).Encode(value)
}

func (h *Handler) deleteDiagnosticSetting(w http.ResponseWriter, parentID, name string) {
	if !h.store.Delete(h.diagnosticSettingKey(parentID, name)) {
		azerr.NotFound(w, diagnosticSettingsType, name)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listDiagnosticSettings(w http.ResponseWriter, parentID string) {
	items := h.store.ListByPrefix(h.diagnosticSettingsPrefix(parentID))
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func decodeBody(w http.ResponseWriter, r *http.Request) (map[string]interface{}, bool) {
	body := map[string]interface{}{}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			azerr.BadRequest(w, "invalid request body")
			return nil, false
		}
	}
	if body == nil {
		body = map[string]interface{}{}
	}
	return body, true
}

func resourceResponse(body map[string]interface{}, id, name, resourceType string) map[string]interface{} {
	body["id"] = id
	body["name"] = name
	body["type"] = resourceType

	properties, ok := body["properties"].(map[string]interface{})
	if !ok {
		properties = map[string]interface{}{}
		body["properties"] = properties
	}
	properties["provisioningState"] = "Succeeded"

	return body
}
