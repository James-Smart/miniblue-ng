package frontdoor

import (
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/store"
)

// resourceSpec describes one kind of resource in the Front Door tree. The
// generic nested handler uses it to serve CRUD for every child resource.
type resourceSpec struct {
	kind     string   // "endpoint", "originGroup", ...
	keyKind  string   // kind segment in the store key, e.g. "endpoint"
	segment  string   // URL segment, e.g. "afdEndpoints"
	armType  string   // ARM resource type, e.g. "Microsoft.Cdn/profiles/afdEndpoints"
	parents  []string // chi params needed to build the parent key, e.g. profileName
	computed func(parent map[string]interface{}, input map[string]interface{}, existing map[string]interface{}) map[string]interface{}
	children []string // key kinds removed when this resource is deleted
}

// nested serves CRUD for a resource described by a resourceSpec.
//
// The resource name is always the final URL parameter of the matched route.
// Parent chi parameters are listed in spec.parents and are resolved into store
// keys by lowercasing the parameter name without its "Name" suffix (profileName
// -> profile, originGroupName -> origingroup).
//
// computed receives the merged record (existing overlaid with the request
// body) as input, so it can be written as a pure enrichment function. It also
// receives the previous record so generated values can be preserved.
type nested struct {
	store *store.Store
	spec  resourceSpec
}

func (n *nested) create(w http.ResponseWriter, r *http.Request) {
	parent, sub, rg, parents, ok := n.parentContext(w, r)
	if !ok {
		return
	}

	input, ok := decodeBody(w, r)
	if !ok {
		return
	}

	name := resourceName(r)
	key := n.resourceKey(sub, rg, parents, name)
	existing, exists := n.existing(key)

	rec := n.record(parent, name, input, existing)
	n.store.Set(key, rec)

	status := http.StatusCreated
	if exists {
		status = http.StatusOK
	}
	writeRecord(w, status, rec)
}

func (n *nested) get(w http.ResponseWriter, r *http.Request) {
	parent, sub, rg, parents, ok := n.parentContext(w, r)
	if !ok {
		return
	}

	name := resourceName(r)
	existing, exists := n.existing(n.resourceKey(sub, rg, parents, name))
	if !exists {
		azerr.NotFound(w, n.spec.armType, name)
		return
	}
	writeRecord(w, http.StatusOK, n.record(parent, name, nil, existing))
}

func (n *nested) patch(w http.ResponseWriter, r *http.Request) {
	parent, sub, rg, parents, ok := n.parentContext(w, r)
	if !ok {
		return
	}

	name := resourceName(r)
	key := n.resourceKey(sub, rg, parents, name)
	existing, exists := n.existing(key)
	if !exists {
		azerr.NotFound(w, n.spec.armType, name)
		return
	}

	input, ok := decodeBody(w, r)
	if !ok {
		return
	}

	rec := n.record(parent, name, input, existing)
	n.store.Set(key, rec)
	writeRecord(w, http.StatusOK, rec)
}

func (n *nested) delete(w http.ResponseWriter, r *http.Request) {
	_, sub, rg, parents, ok := n.parentContext(w, r)
	if !ok {
		return
	}

	name := resourceName(r)
	if !n.store.Delete(n.resourceKey(sub, rg, parents, name)) {
		azerr.NotFound(w, n.spec.armType, name)
		return
	}

	for _, child := range n.spec.children {
		prefix := nestedKey(sub, rg, child, profileOf(parents), parentsAfterProfile(parents), name) + ":"
		n.store.DeleteByPrefix(prefix)
	}
	w.WriteHeader(http.StatusAccepted)
}

func (n *nested) list(w http.ResponseWriter, r *http.Request) {
	parent, sub, rg, parents, ok := n.parentContext(w, r)
	if !ok {
		return
	}

	prefix := n.scopePrefix(sub, rg, parents) + ":"
	keys := n.store.ListKeysByPrefix(prefix)
	sort.Strings(keys)

	value := make([]interface{}, 0, len(keys))
	for _, key := range keys {
		existing, exists := n.existing(key)
		if !exists {
			continue
		}
		name := strings.TrimPrefix(key, prefix)
		value = append(value, n.record(parent, name, nil, existing))
	}
	writeList(w, value)
}

// ---------- key helpers ----------

// parentContext resolves every parent in the chain, returning the closest
// parent record. It writes a 404 response when a parent is missing.
func (n *nested) parentContext(w http.ResponseWriter, r *http.Request) (map[string]interface{}, string, string, []string, bool) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")

	names := make([]string, 0, len(n.spec.parents))
	var parent map[string]interface{}
	for _, param := range n.spec.parents {
		name := chi.URLParam(r, param)
		names = append(names, name)

		key := "fd:" + sub + ":" + rg + ":" + parentKeyKind(param) + ":" + strings.Join(names, ":")
		v, exists := n.store.Get(key)
		if !exists {
			azerr.NotFound(w, parentARMType(param), name)
			return nil, "", "", nil, false
		}
		rec, isMap := v.(map[string]interface{})
		if !isMap {
			azerr.NotFound(w, parentARMType(param), name)
			return nil, "", "", nil, false
		}
		parent = rec
	}
	return parent, sub, rg, names, true
}

// resourceKey builds the store key for the resource at name.
func (n *nested) resourceKey(sub, rg string, parents []string, name string) string {
	return nestedKey(sub, rg, n.spec.keyKind, profileOf(parents), parentsAfterProfile(parents), name)
}

// scopePrefix is the key prefix shared by every sibling of the resource.
func (n *nested) scopePrefix(sub, rg string, parents []string) string {
	return "fd:" + sub + ":" + rg + ":" + n.spec.keyKind + ":" + strings.Join(parents, ":")
}

func (n *nested) existing(key string) (map[string]interface{}, bool) {
	v, exists := n.store.Get(key)
	if !exists {
		return nil, false
	}
	rec, isMap := v.(map[string]interface{})
	return rec, isMap
}

// ---------- record helpers ----------

// record merges input over existing, applies computed, then stamps the ARM
// envelope and the common computed properties.
func (n *nested) record(parent map[string]interface{}, name string, input, existing map[string]interface{}) map[string]interface{} {
	merged := mergeMaps(existing, input)
	// computed needs the name to derive values such as endpoint host names.
	merged["name"] = name

	rec := merged
	if n.spec.computed != nil {
		if computed := n.spec.computed(parent, merged, existing); computed != nil {
			rec = computed
		}
	}

	rec["id"] = resourceID(stringVal(parent["id"]), n.spec.segment, name)
	rec["name"] = name
	rec["type"] = n.spec.armType
	rec["etag"] = "W/\"miniblue\""
	if stringVal(rec["location"]) == "" {
		rec["location"] = "global"
	}
	if tags, ok := rec["tags"].(map[string]interface{}); !ok || tags == nil {
		rec["tags"] = map[string]interface{}{}
	}

	props := cloneMap(mapVal(rec["properties"]))
	if props == nil {
		props = map[string]interface{}{}
	}
	props["provisioningState"] = "Succeeded"
	props["resourceState"] = "Active"
	rec["properties"] = props

	return rec
}

// ---------- small helpers ----------

// resourceName is the final URL parameter, which is always the resource name
// for the routes registered with the generic handler.
func resourceName(r *http.Request) string {
	ctx := chi.RouteContext(r.Context())
	if ctx == nil || len(ctx.URLParams.Values) == 0 {
		return ""
	}
	return ctx.URLParams.Values[len(ctx.URLParams.Values)-1]
}

// parentKeyKind maps a chi parent parameter to its store key kind.
func parentKeyKind(param string) string {
	return strings.ToLower(strings.TrimSuffix(param, "Name"))
}

// parentARMType returns the ARM type used in not-found errors for a parent.
func parentARMType(param string) string {
	switch param {
	case "endpointName":
		return "Microsoft.Cdn/profiles/afdEndpoints"
	case "originGroupName":
		return "Microsoft.Cdn/profiles/originGroups"
	case "ruleSetName":
		return "Microsoft.Cdn/profiles/ruleSets"
	default:
		return "Microsoft.Cdn/profiles"
	}
}

func profileOf(parents []string) string {
	if len(parents) == 0 {
		return ""
	}
	return parents[0]
}

func parentsAfterProfile(parents []string) []string {
	if len(parents) <= 1 {
		return nil
	}
	return parents[1:]
}

// endpointSpec is the AFD endpoint child of a profile.
var endpointSpec = resourceSpec{
	kind:     "endpoint",
	keyKind:  "endpoint",
	segment:  "afdEndpoints",
	armType:  endpointType,
	parents:  []string{"profileName"},
	computed: endpointResponse,
	children: []string{"route"},
}

// originGroupSpec is the origin group child of a profile.
var originGroupSpec = resourceSpec{
	kind:     "originGroup",
	keyKind:  "origingroup",
	segment:  "originGroups",
	armType:  originGroupType,
	parents:  []string{"profileName"},
	computed: originGroupResponse,
	children: []string{"origin"},
}

// originSpec is the origin child of an origin group.
var originSpec = resourceSpec{
	kind:     "origin",
	keyKind:  "origin",
	segment:  "origins",
	armType:  originType,
	parents:  []string{"profileName", "originGroupName"},
	computed: originResponse,
}
