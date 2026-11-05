package frontdoor

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/store"
)

// Handler serves the Microsoft.Cdn Front Door resources.
type Handler struct {
	store *store.Store
}

func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s}
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Cdn/profiles", func(r chi.Router) {
		r.Get("/", h.List)
		r.Route("/{profileName}", func(r chi.Router) {
			r.Put("/", h.CreateOrUpdate)
			r.Get("/", h.Get)
			r.Patch("/", h.Patch)
			r.Delete("/", h.Delete)

			r.Route("/afdEndpoints", func(r chi.Router) {
				endpoints := &nested{store: h.store, spec: endpointSpec}
				r.Get("/", endpoints.list)
				r.Route("/{endpointName}", func(r chi.Router) {
					r.Put("/", endpoints.create)
					r.Get("/", endpoints.get)
					r.Patch("/", endpoints.patch)
					r.Delete("/", endpoints.delete)

					r.Route("/routes", func(r chi.Router) {
						routes := &nested{store: h.store, spec: routeSpec}
						r.Get("/", routes.list)
						r.Route("/{routeName}", func(r chi.Router) {
							r.Put("/", routes.create)
							r.Get("/", routes.get)
							r.Patch("/", routes.patch)
							r.Delete("/", routes.delete)
						})
					})
				})
			})

			r.Route("/originGroups", func(r chi.Router) {
				groups := &nested{store: h.store, spec: originGroupSpec}
				r.Get("/", groups.list)
				r.Route("/{originGroupName}", func(r chi.Router) {
					r.Put("/", groups.create)
					r.Get("/", groups.get)
					r.Patch("/", groups.patch)
					r.Delete("/", groups.delete)

					r.Route("/origins", func(r chi.Router) {
						origins := &nested{store: h.store, spec: originSpec}
						r.Get("/", origins.list)
						r.Route("/{originName}", func(r chi.Router) {
							r.Put("/", origins.create)
							r.Get("/", origins.get)
							r.Patch("/", origins.patch)
							r.Delete("/", origins.delete)
						})
					})
				})
			})

			r.Route("/ruleSets", func(r chi.Router) {
				ruleSets := &nested{store: h.store, spec: ruleSetSpec}
				r.Get("/", ruleSets.list)
				r.Route("/{ruleSetName}", func(r chi.Router) {
					r.Put("/", ruleSets.create)
					r.Get("/", ruleSets.get)
					r.Patch("/", ruleSets.patch)
					r.Delete("/", ruleSets.delete)

					r.Route("/rules", func(r chi.Router) {
						rules := &nested{store: h.store, spec: ruleSpec}
						r.Get("/", rules.list)
						r.Route("/{ruleName}", func(r chi.Router) {
							r.Put("/", rules.create)
							r.Get("/", rules.get)
							r.Patch("/", rules.patch)
							r.Delete("/", rules.delete)
						})
					})
				})
			})
		})
	})
	r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.Cdn/profiles", h.ListInSubscription)
}

// ---------- profile handlers ----------

func (h *Handler) CreateOrUpdate(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "profileName")

	input, ok := decodeBody(w, r)
	if !ok {
		return
	}

	existing, exists := h.getProfile(sub, rg, name)
	rec := profileResponse(sub, rg, name, input, existing)
	h.store.Set(profileKey(sub, rg, name), rec)

	status := http.StatusCreated
	if exists {
		status = http.StatusOK
	}
	writeRecord(w, status, rec)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "profileName")

	existing, ok := h.getProfile(sub, rg, name)
	if !ok {
		azerr.NotFound(w, profileType, name)
		return
	}
	writeRecord(w, http.StatusOK, profileResponse(sub, rg, name, nil, existing))
}

func (h *Handler) Patch(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "profileName")

	existing, ok := h.getProfile(sub, rg, name)
	if !ok {
		azerr.NotFound(w, profileType, name)
		return
	}

	input, ok := decodeBody(w, r)
	if !ok {
		return
	}

	rec := profileResponse(sub, rg, name, input, existing)
	h.store.Set(profileKey(sub, rg, name), rec)
	writeRecord(w, http.StatusOK, rec)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "profileName")

	if !h.store.Delete(profileKey(sub, rg, name)) {
		azerr.NotFound(w, profileType, name)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")

	prefix := rgPrefix(sub, rg) + "profile:"
	keys := h.store.ListKeysByPrefix(prefix)
	value := make([]interface{}, 0, len(keys))
	for _, key := range keys {
		name := strings.TrimPrefix(key, prefix)
		if rec, ok := h.getProfile(sub, rg, name); ok {
			value = append(value, profileResponse(sub, rg, name, nil, rec))
		}
	}
	writeList(w, value)
}

func (h *Handler) ListInSubscription(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")

	prefix := "fd:" + sub + ":"
	keys := h.store.ListKeysByPrefix(prefix)
	value := make([]interface{}, 0, len(keys))
	for _, key := range keys {
		// Only profile records: fd:<sub>:<rg>:profile:<name>.
		rest := strings.TrimPrefix(key, prefix)
		parts := strings.Split(rest, ":")
		if len(parts) != 3 || parts[1] != "profile" {
			continue
		}
		if rec, ok := h.getProfile(sub, parts[0], parts[2]); ok {
			value = append(value, profileResponse(sub, parts[0], parts[2], nil, rec))
		}
	}
	writeList(w, value)
}

// ---------- store helpers ----------

func (h *Handler) getProfile(sub, rg, name string) (map[string]interface{}, bool) {
	v, ok := h.store.Get(profileKey(sub, rg, name))
	if !ok {
		return nil, false
	}
	rec, ok := v.(map[string]interface{})
	return rec, ok
}

// ---------- response helpers ----------

func writeRecord(w http.ResponseWriter, status int, rec map[string]interface{}) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(rec)
}

func writeList(w http.ResponseWriter, value []interface{}) {
	json.NewEncoder(w).Encode(map[string]interface{}{"value": value})
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
