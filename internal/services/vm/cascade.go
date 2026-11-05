package vm

import (
	"os"

	"github.com/moabukar/miniblue/internal/store"
)

// CleanupVMsInRG removes the Docker containers, networks and identity-secret
// index entries belonging to every VM of the given subscription/resource
// group. Intended to be invoked by the resourcegroups handler during cascade
// delete, BEFORE it removes the vm:/vmsvc: keys from the store. Safe to call
// in stub mode and when the RG has no VMs; Docker errors are logged inside
// the helpers, never returned — the ARM-level delete must always succeed.
func CleanupVMsInRG(s *store.Store, sub, rg string) {
	dockerOK := checkDockerCached()
	// The detach helpers only need the store; a bare handler avoids a Docker
	// probe and keeps them shared with the HTTP delete path.
	h := &Handler{store: s}
	for _, item := range s.ListByPrefix("vm:" + sub + ":" + rg + ":") {
		rec, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := rec["name"].(string)
		if secret, _ := rec["_identitySecret"].(string); secret != "" {
			s.Delete(secretKey(secret))
		}
		// Extension children are owned by the VM and must never outlive it.
		if name != "" {
			s.DeleteByPrefix("vmext:" + sub + ":" + rg + ":" + name + ":")
		}
		// Detach NICs and managed disks before their own records are removed by
		// the resource group cascade. Cross-resource-group references stay
		// consistent, and the records themselves are left to the caller.
		h.syncNICLinks(asString(rec["id"]), rec, nil)
		h.detachVMDisks(rec)

		if !dockerOK || name == "" {
			continue
		}
		for _, svcItem := range s.ListByPrefix("vmsvc:" + sub + ":" + rg + ":" + name + ":") {
			svc, _ := svcItem.(map[string]interface{})
			if cn, _ := svc["_containerName"].(string); cn != "" {
				dockerRemove(cn)
			}
		}
		dockerRemove(vmContainerName(rg, name))
		dockerNetworkRemove(vmNetworkName(rg, name))
	}
}

// checkDockerCached memoizes the Docker probe for cascade calls, which run in
// the resourcegroups handler without access to the VM handler instance.
var dockerProbe *bool

func checkDockerCached() bool {
	if os.Getenv("MINIBLUE_DISABLE_DOCKER") != "" {
		return false
	}
	if dockerProbe == nil {
		v := checkDocker()
		dockerProbe = &v
	}
	return *dockerProbe
}
