package frontdoor

import (
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/azerr"
)

// wafKey returns the store key for a Front Door WAF policy. WAF policies are a
// Microsoft.Network resource, not a child of a CDN profile, so they live
// directly under the resource group.
func wafKey(sub, rg, name string) string {
	return "fd:" + sub + ":" + rg + ":waf:" + name
}

// registerWAFPolicyRoutes registers the WAF policy routes under both the
// lower-case and capitalised path spellings; ARM callers differ in casing.
func (h *Handler) registerWAFPolicyRoutes(r chi.Router) {
	for _, segment := range []string{
		"frontDoorWebApplicationFirewallPolicies",
		"FrontDoorWebApplicationFirewallPolicies",
	} {
		r.Route("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Network/"+segment, func(r chi.Router) {
			r.Get("/", h.ListWAFPolicies)
			r.Route("/{policyName}", func(r chi.Router) {
				r.Put("/", h.CreateOrUpdateWAFPolicy)
				r.Get("/", h.GetWAFPolicy)
				r.Patch("/", h.PatchWAFPolicy)
				r.Delete("/", h.DeleteWAFPolicy)
			})
		})
		r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.Network/"+segment, h.ListWAFPoliciesInSubscription)
	}
}

// ---------- WAF policy handlers ----------

func (h *Handler) CreateOrUpdateWAFPolicy(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "policyName")

	input, ok := decodeBody(w, r)
	if !ok {
		return
	}

	existing, exists := h.getWAFPolicy(sub, rg, name)
	rec := wafPolicyResponse(sub, rg, name, input, existing)
	h.store.Set(wafKey(sub, rg, name), rec)

	status := http.StatusCreated
	if exists {
		status = http.StatusOK
	}
	writeRecord(w, status, rec)
}

func (h *Handler) GetWAFPolicy(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "policyName")

	existing, ok := h.getWAFPolicy(sub, rg, name)
	if !ok {
		azerr.NotFound(w, wafPolicyType, name)
		return
	}
	writeRecord(w, http.StatusOK, wafPolicyResponse(sub, rg, name, nil, existing))
}

func (h *Handler) PatchWAFPolicy(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "policyName")

	existing, ok := h.getWAFPolicy(sub, rg, name)
	if !ok {
		azerr.NotFound(w, wafPolicyType, name)
		return
	}

	input, ok := decodeBody(w, r)
	if !ok {
		return
	}

	rec := wafPolicyResponse(sub, rg, name, input, existing)
	h.store.Set(wafKey(sub, rg, name), rec)
	writeRecord(w, http.StatusOK, rec)
}

func (h *Handler) DeleteWAFPolicy(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "policyName")

	if !h.store.Delete(wafKey(sub, rg, name)) {
		azerr.NotFound(w, wafPolicyType, name)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListWAFPolicies(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")

	prefix := "fd:" + sub + ":" + rg + ":waf:"
	keys := h.store.ListKeysByPrefix(prefix)
	sort.Strings(keys)

	value := make([]interface{}, 0, len(keys))
	for _, key := range keys {
		name := strings.TrimPrefix(key, prefix)
		if rec, ok := h.getWAFPolicy(sub, rg, name); ok {
			value = append(value, wafPolicyResponse(sub, rg, name, nil, rec))
		}
	}
	writeList(w, value)
}

func (h *Handler) ListWAFPoliciesInSubscription(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")

	prefix := "fd:" + sub + ":"
	keys := h.store.ListKeysByPrefix(prefix)

	value := make([]interface{}, 0, len(keys))
	for _, key := range keys {
		// Only WAF records: fd:<sub>:<rg>:waf:<name>.
		rest := strings.TrimPrefix(key, prefix)
		parts := strings.Split(rest, ":")
		if len(parts) != 3 || parts[1] != "waf" {
			continue
		}
		if rec, ok := h.getWAFPolicy(sub, parts[0], parts[2]); ok {
			value = append(value, wafPolicyResponse(sub, parts[0], parts[2], nil, rec))
		}
	}
	writeList(w, value)
}

// ---------- store helpers ----------

func (h *Handler) getWAFPolicy(sub, rg, name string) (map[string]interface{}, bool) {
	v, ok := h.store.Get(wafKey(sub, rg, name))
	if !ok {
		return nil, false
	}
	rec, ok := v.(map[string]interface{})
	return rec, ok
}
