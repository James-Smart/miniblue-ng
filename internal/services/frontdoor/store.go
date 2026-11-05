package frontdoor

import (
	"strings"

	"github.com/moabukar/miniblue/internal/store"
)

// Store key layout. Everything lives under fd:<sub>:<rg>:<kind>:... so a
// resource group's full Front Door footprint can be deleted with one prefix.
//
//	Profile         fd:<sub>:<rg>:profile:<profile>
//	Endpoint        fd:<sub>:<rg>:endpoint:<profile>:<endpoint>
//	Origin group    fd:<sub>:<rg>:origingroup:<profile>:<group>
//	Origin          fd:<sub>:<rg>:origin:<profile>:<group>:<origin>
//	Route           fd:<sub>:<rg>:route:<profile>:<endpoint>:<route>
//	Rule set        fd:<sub>:<rg>:ruleset:<profile>:<ruleSet>
//	Rule            fd:<sub>:<rg>:rule:<profile>:<ruleSet>:<rule>
//	Custom domain   fd:<sub>:<rg>:customdomain:<profile>:<domain>
//	Secret          fd:<sub>:<rg>:secret:<profile>:<secret>
//	Security policy fd:<sub>:<rg>:securitypolicy:<profile>:<policy>
//	WAF policy      fd:<sub>:<rg>:waf:<name>

// profileKey returns the store key for a Front Door profile.
func profileKey(sub, rg, name string) string {
	return "fd:" + sub + ":" + rg + ":profile:" + name
}

// rgPrefix returns the prefix covering every Front Door resource in a resource
// group, across subscriptions and profiles.
func rgPrefix(sub, rg string) string {
	return "fd:" + sub + ":" + rg + ":"
}

// resourceID appends a child segment and name to a parent ARM ID.
func resourceID(parentID, segment, name string) string {
	return strings.TrimRight(parentID, "/") + "/" + segment + "/" + name
}

// nestedKey returns the store key for a resource nested under a profile.
// parents holds any intermediate names between the profile and the resource
// (for example the origin group for an origin).
func nestedKey(sub, rg, keyKind, profile string, parents []string, name string) string {
	key := "fd:" + sub + ":" + rg + ":" + keyKind + ":" + profile
	for _, parent := range parents {
		key += ":" + parent
	}
	return key + ":" + name
}

// removeCustomDomainFromRoutes scrubs domainID from the customDomains array of
// every route under the profile. The provider models custom-domain association
// client-side, so the route array is the only place the link is recorded.
func removeCustomDomainFromRoutes(s *store.Store, sub, rg, profile, domainID string) {
	prefix := "fd:" + sub + ":" + rg + ":route:" + profile + ":"
	for _, key := range s.ListKeysByPrefix(prefix) {
		v, exists := s.Get(key)
		if !exists {
			continue
		}
		rec, isMap := v.(map[string]interface{})
		if !isMap {
			continue
		}

		props := mapVal(rec["properties"])
		domains, isList := props["customDomains"].([]interface{})
		if !isList {
			continue
		}

		kept := make([]interface{}, 0, len(domains))
		removed := false
		for _, domain := range domains {
			if stringVal(mapVal(domain)["id"]) == domainID {
				removed = true
				continue
			}
			kept = append(kept, domain)
		}
		if !removed {
			continue
		}

		props["customDomains"] = kept
		rec["properties"] = props
		s.Set(key, rec)
	}
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
