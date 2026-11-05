package sshkeys

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/store"
	"golang.org/x/crypto/ssh"
)

const sshKeyType = "Microsoft.Compute/sshPublicKeys"

// Handler serves the Microsoft.Compute/sshPublicKeys ARM resource.
type Handler struct {
	store *store.Store
}

func NewHandler(s *store.Store) *Handler {
	return &Handler{store: s}
}

func (h *Handler) Register(r chi.Router) {
	r.Route("/subscriptions/{subscriptionId}/resourceGroups/{resourceGroupName}/providers/Microsoft.Compute/sshPublicKeys", func(r chi.Router) {
		r.Get("/", h.List)
		r.Route("/{sshPublicKeyName}", func(r chi.Router) {
			r.Put("/", h.CreateOrUpdate)
			r.Get("/", h.Get)
			r.Patch("/", h.Patch)
			r.Delete("/", h.Delete)
			r.Post("/generateKeyPair", h.GenerateKeyPair)
		})
	})
	r.Get("/subscriptions/{subscriptionId}/providers/Microsoft.Compute/sshPublicKeys", h.ListInSubscription)
}

// Key returns the store key for an SSH public key.
func Key(sub, rg, name string) string {
	return "sshkey:" + sub + ":" + rg + ":" + name
}

// ---------- handlers ----------

func (h *Handler) CreateOrUpdate(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "sshPublicKeyName")

	input, ok := decodeBody(w, r)
	if !ok {
		return
	}

	existing, exists := get(h.store, sub, rg, name)
	rec, err := newRecord(sub, rg, name, input, existing)
	if err != nil {
		azerr.WriteError(w, http.StatusInternalServerError, "InternalServerError",
			"The SSH public key could not be generated.")
		return
	}
	h.store.Set(Key(sub, rg, name), rec)

	status := http.StatusCreated
	if exists {
		status = http.StatusOK
	}
	writeRecord(w, status, rec)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "sshPublicKeyName")

	rec, ok := get(h.store, sub, rg, name)
	if !ok {
		azerr.NotFound(w, sshKeyType, name)
		return
	}
	writeRecord(w, http.StatusOK, rec)
}

func (h *Handler) Patch(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "sshPublicKeyName")

	existing, ok := get(h.store, sub, rg, name)
	if !ok {
		azerr.NotFound(w, sshKeyType, name)
		return
	}

	input, ok := decodeBody(w, r)
	if !ok {
		return
	}

	rec, err := newRecord(sub, rg, name, input, existing)
	if err != nil {
		azerr.WriteError(w, http.StatusInternalServerError, "InternalServerError",
			"The SSH public key could not be generated.")
		return
	}
	h.store.Set(Key(sub, rg, name), rec)
	writeRecord(w, http.StatusOK, rec)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "sshPublicKeyName")

	if !h.store.Delete(Key(sub, rg, name)) {
		azerr.NotFound(w, sshKeyType, name)
		return
	}
	// The azurerm SSH-public-key poller rejects a bare 202; the real Compute
	// sshPublicKeys endpoint deletes synchronously, so return 204.
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	items := h.store.ListByPrefix("sshkey:" + sub + ":" + rg + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func (h *Handler) ListInSubscription(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	items := h.store.ListByPrefix("sshkey:" + sub + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

// GenerateKeyPair returns a fresh 2048-bit RSA key pair. The stored public key
// is left untouched so later GETs keep returning the resource's key.
func (h *Handler) GenerateKeyPair(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "sshPublicKeyName")

	if _, ok := get(h.store, sub, rg, name); !ok {
		azerr.NotFound(w, sshKeyType, name)
		return
	}

	publicKey, privateKey, err := newKeyPair()
	if err != nil {
		azerr.WriteError(w, http.StatusInternalServerError, "InternalServerError",
			"The SSH key pair could not be generated.")
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"publicKey":  publicKey,
		"privateKey": privateKey,
	})
}

// ---------- record helpers ----------

func get(s *store.Store, sub, rg, name string) (map[string]interface{}, bool) {
	v, ok := s.Get(Key(sub, rg, name))
	if !ok {
		return nil, false
	}
	rec, ok := v.(map[string]interface{})
	return rec, ok
}

// newRecord builds the stored record. A public key supplied in the body is
// echoed; otherwise the existing key is preserved, and a key is generated only
// when neither is available.
func newRecord(sub, rg, name string, input, existing map[string]interface{}) (map[string]interface{}, error) {
	rec := mergeMaps(existing, input)

	rec["id"] = "/subscriptions/" + sub + "/resourceGroups/" + rg +
		"/providers/Microsoft.Compute/sshPublicKeys/" + name
	rec["name"] = name
	rec["type"] = sshKeyType
	if stringVal(rec["location"]) == "" {
		rec["location"] = "eastus"
	}
	if tags, ok := rec["tags"].(map[string]interface{}); !ok || tags == nil {
		rec["tags"] = map[string]interface{}{}
	}

	props := cloneMap(mapVal(rec["properties"]))
	if props == nil {
		props = map[string]interface{}{}
	}
	if stringVal(props["publicKey"]) == "" {
		if previous := stringVal(mapVal(existing["properties"])["publicKey"]); previous != "" {
			props["publicKey"] = previous
		} else {
			publicKey, _, err := newKeyPair()
			if err != nil {
				return nil, err
			}
			props["publicKey"] = publicKey
		}
	}
	props["provisioningState"] = "Succeeded"
	rec["properties"] = props

	return rec, nil
}

func newKeyPair() (publicKey, privateKey string, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}

	sshPublic, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		return "", "", err
	}
	publicKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPublic)))

	privateKey = string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
	return publicKey, privateKey, nil
}

func writeRecord(w http.ResponseWriter, status int, rec map[string]interface{}) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(rec)
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

func mapVal(v interface{}) map[string]interface{} {
	m, _ := v.(map[string]interface{})
	return m
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
		if patchMap, ok := v.(map[string]interface{}); ok {
			out[k] = mergeMaps(mapVal(out[k]), patchMap)
			continue
		}
		out[k] = cloneValue(v)
	}
	return out
}
