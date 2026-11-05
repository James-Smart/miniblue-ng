package keyvault

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/store"
)

type Secret struct {
	ID         string            `json:"id"`
	Value      string            `json:"value"`
	Attributes map[string]string `json:"attributes"`
}

type Handler struct {
	store *store.Store
}

func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s}
}

func (h *Handler) Register(r chi.Router) {
	// Data-plane secret API.
	r.Route("/keyvault/{vaultName}/secrets", func(r chi.Router) {
		r.Get("/", h.ListSecrets)
		r.Route("/{secretName}", func(r chi.Router) {
			r.Put("/", h.SetSecret)
			r.Get("/", h.GetSecret)
			r.Delete("/", h.DeleteSecret)
		})
	})

	// ARM management-plane vault API used by azurerm 5.x.
	r.Route("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.KeyVault/vaults", func(r chi.Router) {
		r.Get("/", h.ListVaults)
		r.Route("/{vaultName}", func(r chi.Router) {
			r.Put("/", h.CreateOrUpdateVault)
			r.Get("/", h.GetVault)
			r.Patch("/", h.UpdateVault)
			r.Delete("/", h.DeleteVault)
		})
	})
	r.Route("/subscriptions/{subscriptionId}/providers/Microsoft.KeyVault/locations/{locationName}/deletedVaults/{vaultName}", func(r chi.Router) {
		r.Get("/", h.GetDeletedVault)
		r.Post("/purge", h.PurgeDeletedVault)
	})
	r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.KeyVault/operations/{operationName}", h.OperationResult)
}

func (h *Handler) key(vault, name string) string {
	return "kv:" + vault + ":" + name
}

func (h *Handler) SetSecret(w http.ResponseWriter, r *http.Request) {
	vault := chi.URLParam(r, "vaultName")
	name := chi.URLParam(r, "secretName")

	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		azerr.BadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	secret := Secret{
		ID:    "https://" + vault + ".vault.azure.net/secrets/" + name,
		Value: body.Value,
		Attributes: map[string]string{
			"enabled": "true",
			"created": time.Now().UTC().Format(time.RFC3339),
			"updated": time.Now().UTC().Format(time.RFC3339),
		},
	}

	h.store.Set(h.key(vault, name), secret)
	json.NewEncoder(w).Encode(secret)
}

func (h *Handler) GetSecret(w http.ResponseWriter, r *http.Request) {
	vault := chi.URLParam(r, "vaultName")
	name := chi.URLParam(r, "secretName")

	v, ok := h.store.Get(h.key(vault, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.KeyVault/vaults/secrets", name)
		return
	}
	json.NewEncoder(w).Encode(v)
}

func (h *Handler) DeleteSecret(w http.ResponseWriter, r *http.Request) {
	vault := chi.URLParam(r, "vaultName")
	name := chi.URLParam(r, "secretName")
	h.store.Delete(h.key(vault, name))
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) ListSecrets(w http.ResponseWriter, r *http.Request) {
	vault := chi.URLParam(r, "vaultName")
	items := h.store.ListByPrefix("kv:" + vault + ":")
	// Azure Key Vault list returns metadata only, NOT the secret value
	redacted := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if s, ok := item.(Secret); ok {
			redacted = append(redacted, map[string]interface{}{
				"id":         s.ID,
				"attributes": s.Attributes,
			})
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"value": redacted})
}

// vaultKey stores the ARM representation of a Key Vault. The deleted-vault
// tombstone is separate so a soft-deleted vault is not visible through the
// management-plane GET while still being discoverable at the location-scoped
// deletedVaults endpoint.
func (h *Handler) vaultKey(sub, rg, name string) string {
	return "keyvault:vault:" + sub + ":" + rg + ":" + name
}

func (h *Handler) deletedVaultKey(sub, location, name string) string {
	return "keyvault:deleted:" + sub + ":" + normalizeLocation(location) + ":" + name
}

func normalizeLocation(location string) string {
	location = strings.ToLower(strings.TrimSpace(location))
	location = strings.ReplaceAll(location, " ", "")
	if location == "" {
		return "eastus"
	}
	return location
}

func vaultURI(r *http.Request, name string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/keyvault/" + name
}

func writeVaultJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func (h *Handler) ListVaults(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	items := h.store.ListByPrefix("keyvault:vault:" + sub + ":" + rg + ":")
	writeVaultJSON(w, http.StatusOK, map[string]interface{}{"value": items})
}

func (h *Handler) CreateOrUpdateVault(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vaultName")

	var input map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		azerr.BadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	existing, exists := h.store.Get(h.vaultKey(sub, rg, name))
	existingMap, _ := existing.(map[string]interface{})
	vault := buildVaultResponse(r, sub, rg, name, input, existingMap)
	h.store.Set(h.vaultKey(sub, rg, name), vault)
	// Creating or recovering a vault removes any soft-delete tombstone for it.
	h.store.Delete(h.deletedVaultKey(sub, stringValue(vault["location"]), name))

	status := http.StatusCreated
	if exists {
		status = http.StatusOK
	}
	writeVaultJSON(w, status, vault)
}

func (h *Handler) GetVault(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vaultName")

	v, ok := h.store.Get(h.vaultKey(sub, rg, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.KeyVault/vaults", name)
		return
	}
	writeVaultJSON(w, http.StatusOK, v)
}

func (h *Handler) UpdateVault(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vaultName")

	existing, ok := h.store.Get(h.vaultKey(sub, rg, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.KeyVault/vaults", name)
		return
	}

	var input map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		azerr.BadRequest(w, "Invalid request body: "+err.Error())
		return
	}

	existingMap, _ := existing.(map[string]interface{})
	vault := buildVaultResponse(r, sub, rg, name, input, existingMap)
	h.store.Set(h.vaultKey(sub, rg, name), vault)
	writeVaultJSON(w, http.StatusOK, vault)
}

func (h *Handler) DeleteVault(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "vaultName")

	existing, ok := h.store.Get(h.vaultKey(sub, rg, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.KeyVault/vaults", name)
		return
	}
	vault, _ := existing.(map[string]interface{})
	props, _ := vault["properties"].(map[string]interface{})
	softDelete := true
	if v, ok := props["enableSoftDelete"].(bool); ok {
		softDelete = v
	}
	location := stringValue(vault["location"])
	h.store.Delete(h.vaultKey(sub, rg, name))
	// Secrets are scoped to the vault name, matching the data-plane API.
	h.store.DeleteByPrefix("kv:" + name + ":")

	if softDelete {
		now := time.Now().UTC()
		deleted := map[string]interface{}{
			"id":   "/subscriptions/" + sub + "/providers/Microsoft.KeyVault/locations/" + normalizeLocation(location) + "/deletedVaults/" + name,
			"name": name,
			"type": "Microsoft.KeyVault/locations/deletedVaults",
			"properties": map[string]interface{}{
				"vaultId":                vault["id"],
				"location":               normalizeLocation(location),
				"deletionDate":           now.Format(time.RFC3339),
				"scheduledPurgeDate":     now.AddDate(0, 0, 90).Format(time.RFC3339),
				"purgeProtectionEnabled": props["enablePurgeProtection"],
				"tags":                   vault["tags"],
			},
		}
		h.store.Set(h.deletedVaultKey(sub, location, name), deleted)
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) GetDeletedVault(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	location := chi.URLParam(r, "locationName")
	name := chi.URLParam(r, "vaultName")

	v, ok := h.store.Get(h.deletedVaultKey(sub, location, name))
	if !ok {
		azerr.NotFound(w, "Microsoft.KeyVault/locations/deletedVaults", name)
		return
	}
	writeVaultJSON(w, http.StatusOK, v)
}

func (h *Handler) PurgeDeletedVault(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	location := chi.URLParam(r, "locationName")
	name := chi.URLParam(r, "vaultName")

	if !h.store.Delete(h.deletedVaultKey(sub, location, name)) {
		azerr.NotFound(w, "Microsoft.KeyVault/locations/deletedVaults", name)
		return
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	opURL := fmt.Sprintf("%s://%s/subscriptions/%s/providers/Microsoft.KeyVault/operations/purge-%s?api-version=2026-02-01", scheme, r.Host, sub, name)
	w.Header().Set("Location", opURL)
	// The purge is synchronous; a short retry lets the provider's LRO poller
	// observe completion without waiting the default ten seconds.
	w.Header().Set("Retry-After", "1")
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) OperationResult(w http.ResponseWriter, r *http.Request) {
	writeVaultJSON(w, http.StatusOK, map[string]interface{}{"status": "Succeeded"})
}

func buildVaultResponse(r *http.Request, sub, rg, name string, input, existing map[string]interface{}) map[string]interface{} {
	vault := mergeMaps(existing, input)
	vault["id"] = "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.KeyVault/vaults/" + name
	vault["name"] = name
	vault["type"] = "Microsoft.KeyVault/vaults"

	location := stringValue(vault["location"])
	if location == "" {
		location = "eastus"
	}
	vault["location"] = location
	if _, ok := vault["tags"].(map[string]interface{}); !ok {
		vault["tags"] = map[string]interface{}{}
	}

	props, _ := vault["properties"].(map[string]interface{})
	if props == nil {
		props = map[string]interface{}{}
		vault["properties"] = props
	}
	if stringValue(props["tenantId"]) == "" {
		props["tenantId"] = "00000000-0000-0000-0000-000000000001"
	}
	if _, ok := props["sku"].(map[string]interface{}); !ok {
		props["sku"] = map[string]interface{}{"family": "A", "name": "standard"}
	}
	if stringValue(props["vaultUri"]) == "" {
		props["vaultUri"] = vaultURI(r, name)
	}
	if _, ok := props["enableSoftDelete"]; !ok {
		props["enableSoftDelete"] = true
	}
	if _, ok := props["softDeleteRetentionInDays"]; !ok {
		props["softDeleteRetentionInDays"] = 90
	}
	if stringValue(props["publicNetworkAccess"]) == "" {
		props["publicNetworkAccess"] = "Enabled"
	}
	if _, ok := props["enableRbacAuthorization"]; !ok {
		props["enableRbacAuthorization"] = false
	}
	props["provisioningState"] = "Succeeded"
	return vault
}

func stringValue(v interface{}) string {
	s, _ := v.(string)
	return s
}

func mergeMaps(base, overlay map[string]interface{}) map[string]interface{} {
	out := cloneMap(base)
	for k, v := range overlay {
		if v == nil {
			delete(out, k)
			continue
		}
		if om, ok := v.(map[string]interface{}); ok {
			bm, _ := out[k].(map[string]interface{})
			out[k] = mergeMaps(bm, om)
			continue
		}
		out[k] = cloneValue(v)
	}
	return out
}

func cloneMap(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(v interface{}) interface{} {
	switch typed := v.(type) {
	case map[string]interface{}:
		return cloneMap(typed)
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, item := range typed {
			out[i] = cloneValue(item)
		}
		return out
	default:
		return v
	}
}
