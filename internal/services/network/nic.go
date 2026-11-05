package network

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/moabukar/miniblue/internal/azerr"
	"github.com/moabukar/miniblue/internal/store"
)

const nicType = "Microsoft.Network/networkInterfaces"

// NICKey returns the store key for a network interface.
func NICKey(sub, rg, name string) string {
	return "nic:" + sub + ":" + rg + ":" + name
}

// canonicalNICKey resolves the store key for a network-interface reference.
// ARM IDs are case-insensitive, so a case-only reference must reuse the record
// that was created first instead of looking up a different key.
func canonicalNICKey(s *store.Store, sub, rg, name string) string {
	key := NICKey(sub, rg, name)
	if s.Exists(key) {
		return key
	}
	for _, candidate := range s.List() {
		if strings.EqualFold(candidate, key) {
			return candidate
		}
	}
	return key
}

// GetNIC reads a network interface from the store.
func GetNIC(s *store.Store, sub, rg, name string) (map[string]interface{}, bool) {
	v, ok := s.Get(canonicalNICKey(s, sub, rg, name))
	if !ok {
		return nil, false
	}
	rec, ok := v.(map[string]interface{})
	return rec, ok
}

// SetNIC writes a network interface to the store. A case-only reference updates
// the canonical key rather than creating a second record.
func SetNIC(s *store.Store, sub, rg, name string, rec map[string]interface{}) {
	s.Set(canonicalNICKey(s, sub, rg, name), rec)
}

// ParseNICID extracts the subscription, resource group and NIC name from a
// network interface ARM ID. Matching is case-insensitive.
func ParseNICID(id string) (sub, rg, name string, ok bool) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(id), "/"), "/")
	if len(parts) != 8 {
		return "", "", "", false
	}
	if !strings.EqualFold(parts[0], "subscriptions") ||
		!strings.EqualFold(parts[2], "resourceGroups") ||
		!strings.EqualFold(parts[4], "providers") ||
		!strings.EqualFold(parts[5], "Microsoft.Network") ||
		!strings.EqualFold(parts[6], "networkInterfaces") {
		return "", "", "", false
	}
	if parts[1] == "" || parts[3] == "" || parts[7] == "" {
		return "", "", "", false
	}
	return parts[1], parts[3], parts[7], true
}

// AttachVMToNIC links a network interface to a virtual machine by setting the
// NIC's properties.virtualMachine.id. It reports whether the NIC exists.
func AttachVMToNIC(s *store.Store, nicID, vmID string) bool {
	sub, rg, name, ok := ParseNICID(nicID)
	if !ok {
		return false
	}
	rec, ok := GetNIC(s, sub, rg, name)
	if !ok {
		return false
	}
	rec = mergeMaps(rec, map[string]interface{}{
		"properties": map[string]interface{}{
			"virtualMachine": map[string]interface{}{"id": vmID},
		},
	})
	SetNIC(s, sub, rg, name, rec)
	return true
}

// DetachVMFromNIC removes the NIC's properties.virtualMachine link. It reports
// whether the NIC exists.
func DetachVMFromNIC(s *store.Store, nicID string) bool {
	sub, rg, name, ok := ParseNICID(nicID)
	if !ok {
		return false
	}
	rec, ok := GetNIC(s, sub, rg, name)
	if !ok {
		return false
	}
	rec = mergeMaps(rec, map[string]interface{}{
		"properties": map[string]interface{}{
			"virtualMachine": nil,
		},
	})
	SetNIC(s, sub, rg, name, rec)
	return true
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

func stringVal(v interface{}) string {
	s, _ := v.(string)
	return s
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func generateMAC() string {
	u := uuid.New()
	return strings.ToUpper(fmt.Sprintf("%02X-%02X-%02X-%02X-%02X-%02X",
		u[0], u[1], u[2], u[3], u[4], u[5]))
}

func generateDNSSuffix(rg string) string {
	hex := strings.ReplaceAll(uuid.New().String(), "-", "")[:8]
	return rg + "." + hex + ".internal.cloudapp.net"
}

func nicIPConfigurations(rec map[string]interface{}) []interface{} {
	props, _ := rec["properties"].(map[string]interface{})
	configs, _ := props["ipConfigurations"].([]interface{})
	return configs
}

func ipConfigName(cfg map[string]interface{}, index int) string {
	if name := stringVal(cfg["name"]); name != "" {
		return name
	}
	return fmt.Sprintf("ipconfig%d", index+1)
}

func ipToUint32(ip net.IP) uint32 {
	b := ip.To4()
	if b == nil {
		return 0
	}
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func uint32ToIP(n uint32) net.IP {
	return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}

// parseSubnetID extracts the identifiers from an ARM subnet resource ID.
func parseSubnetID(id string) (sub, rg, vnet, subnet string, ok bool) {
	parts := strings.Split(strings.Trim(id, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		switch strings.ToLower(parts[i]) {
		case "subscriptions":
			sub = parts[i+1]
		case "resourcegroups":
			rg = parts[i+1]
		case "virtualnetworks":
			vnet = parts[i+1]
		case "subnets":
			subnet = parts[i+1]
		}
	}
	ok = sub != "" && rg != "" && vnet != "" && subnet != ""
	return
}

// subnetPrefixFromID looks up the referenced subnet in the network store and
// returns its first address prefix.
func subnetPrefixFromID(s *store.Store, subnetID string) (string, bool) {
	sub, rg, vnet, subnet, ok := parseSubnetID(subnetID)
	if !ok {
		return "", false
	}
	v, ok := s.Get("subnet:" + sub + ":" + rg + ":" + vnet + ":" + subnet)
	if !ok {
		return "", false
	}
	rec, ok := v.(map[string]interface{})
	if !ok {
		return "", false
	}
	props, _ := rec["properties"].(map[string]interface{})
	if prefix := stringVal(props["addressPrefix"]); prefix != "" {
		return prefix, true
	}
	if prefixes, ok := props["addressPrefixes"].([]interface{}); ok && len(prefixes) > 0 {
		if prefix := stringVal(prefixes[0]); prefix != "" {
			return prefix, true
		}
	}
	return "", false
}

// usedNICAddresses collects private IPs already allocated on the given subnet.
func usedNICAddresses(s *store.Store, subnetID string) map[string]bool {
	used := map[string]bool{}
	for _, item := range s.ListByPrefix("nic:") {
		rec, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		for _, raw := range nicIPConfigurations(rec) {
			cfg, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			props, _ := cfg["properties"].(map[string]interface{})
			ip := stringVal(props["privateIPAddress"])
			if ip == "" {
				continue
			}
			if subnetID != "" {
				subnetRef, _ := props["subnet"].(map[string]interface{})
				if !strings.EqualFold(stringVal(subnetRef["id"]), subnetID) {
					continue
				}
			}
			used[ip] = true
		}
	}
	return used
}

// allocatePrivateIP returns the first free address in the subnet, starting at
// the fourth host. It falls back to 10.0.0.4 upward when the subnet cannot be
// resolved. used is shared across the NIC's configurations so two dynamic
// configs on the same request cannot receive the same address.
func allocatePrivateIP(s *store.Store, subnetID string, used map[string]bool) string {
	if used == nil {
		used = map[string]bool{}
	}
	start := ipToUint32(net.IPv4(10, 0, 0, 4))
	var ipnet *net.IPNet

	if prefix, ok := subnetPrefixFromID(s, subnetID); ok {
		if _, parsed, err := net.ParseCIDR(prefix); err == nil {
			if base := parsed.IP.To4(); base != nil {
				start = ipToUint32(base) + 4
				ipnet = parsed
			}
		}
	}

	for i := uint32(0); i < 1<<20; i++ {
		ip := uint32ToIP(start + i)
		if ipnet != nil && !ipnet.Contains(ip) {
			break
		}
		if !used[ip.String()] {
			return ip.String()
		}
	}

	fallback := ipToUint32(net.IPv4(10, 0, 0, 4))
	for i := uint32(0); i < 1<<20; i++ {
		ip := uint32ToIP(fallback + i)
		if !used[ip.String()] {
			return ip.String()
		}
	}
	return "10.0.0.4"
}

// hasIPConfigurations reports whether the request supplies usable configs.
func hasIPConfigurations(input map[string]interface{}) bool {
	props, _ := input["properties"].(map[string]interface{})
	configs, _ := props["ipConfigurations"].([]interface{})
	return len(configs) > 0
}

// applyIPAllocation fills in an address for every request config that does not
// already have one (and does not reuse the NIC's stored allocation). The
// response builder performs the final preservation pass.
func (h *Handler) applyIPAllocation(input, existing map[string]interface{}) {
	props, _ := input["properties"].(map[string]interface{})
	if props == nil {
		return
	}
	configs, _ := props["ipConfigurations"].([]interface{})

	var existingConfigs map[string]map[string]interface{}
	if existing != nil {
		existingConfigs = map[string]map[string]interface{}{}
		for i, raw := range nicIPConfigurations(existing) {
			cfg, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			existingConfigs[ipConfigName(cfg, i)] = cfg
		}
	}

	// used is shared by every config in this request. Without it, allocating
	// address N for one config and then recomputing the used set for the next
	// config returns N again because the NIC is not stored yet.
	used := map[string]bool{}
	seededSubnets := map[string]bool{}
	seedSubnet := func(subnetID string) {
		key := strings.ToLower(subnetID)
		if seededSubnets[key] {
			return
		}
		seededSubnets[key] = true
		for ip := range usedNICAddresses(h.store, subnetID) {
			used[ip] = true
		}
	}

	type configAllocation struct {
		props     map[string]interface{}
		subnetID  string
		preserved bool
	}
	allocations := make([]configAllocation, 0, len(configs))

	// First pass: seed the shared used set with existing allocations and any
	// caller-supplied static addresses so a dynamic config cannot take one.
	for i, raw := range configs {
		cfg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		cfgProps, _ := cfg["properties"].(map[string]interface{})
		if cfgProps == nil {
			cfgProps = map[string]interface{}{}
			cfg["properties"] = cfgProps
		}

		method := stringVal(cfgProps["privateIPAllocationMethod"])
		if method == "" {
			method = "Dynamic"
			cfgProps["privateIPAllocationMethod"] = method
		}

		subnetRef, _ := cfgProps["subnet"].(map[string]interface{})
		subnetID := stringVal(subnetRef["id"])
		seedSubnet(subnetID)

		preserved := false
		if method == "Static" && stringVal(cfgProps["privateIPAddress"]) != "" {
			preserved = true
		} else if existingCfg, ok := existingConfigs[ipConfigName(cfg, i)]; ok {
			existingProps, _ := existingCfg["properties"].(map[string]interface{})
			if existingIP := stringVal(existingProps["privateIPAddress"]); existingIP != "" {
				// The builder preserves the stored allocation.
				used[existingIP] = true
				preserved = true
			}
		}
		if ip := stringVal(cfgProps["privateIPAddress"]); ip != "" {
			used[ip] = true
		}

		allocations = append(allocations, configAllocation{
			props:     cfgProps,
			subnetID:  subnetID,
			preserved: preserved,
		})
	}

	// Second pass: allocate for configs that have no IP of their own, adding
	// every allocation to the shared used set as it is handed out.
	for _, allocation := range allocations {
		if allocation.preserved {
			continue
		}
		ip := allocatePrivateIP(h.store, allocation.subnetID, used)
		allocation.props["privateIPAddress"] = ip
		used[ip] = true
	}
}

// buildNICResponse returns the ARM representation of a NIC. It merges the
// request over the stored record, preserves generated/read-only fields, and
// fills in Azure defaults.
func buildNICResponse(sub, rg, name string, input, existing map[string]interface{}) map[string]interface{} {
	nicID := "/subscriptions/" + sub + "/resourceGroups/" + rg + "/providers/Microsoft.Network/networkInterfaces/" + name

	base := map[string]interface{}{}
	if existing != nil {
		base = existing
	}
	merged := mergeMaps(base, input)

	var existingProps map[string]interface{}
	if existing != nil {
		existingProps, _ = existing["properties"].(map[string]interface{})
	}

	props, _ := merged["properties"].(map[string]interface{})
	props = cloneMap(props)
	if props == nil {
		props = map[string]interface{}{}
	}

	// Generated values are created once and preserved across updates.
	resourceGuid := firstNonEmpty(
		stringVal(props["resourceGuid"]),
		stringVal(existingProps["resourceGuid"]),
		uuid.New().String(),
	)
	props["resourceGuid"] = resourceGuid

	mac := firstNonEmpty(
		stringVal(props["macAddress"]),
		stringVal(existingProps["macAddress"]),
		generateMAC(),
	)
	props["macAddress"] = mac

	props["provisioningState"] = "Succeeded"
	if _, ok := props["primary"]; !ok {
		props["primary"] = true
	}
	if _, ok := props["enableAcceleratedNetworking"]; !ok {
		props["enableAcceleratedNetworking"] = false
	}
	if _, ok := props["enableIPForwarding"]; !ok {
		props["enableIPForwarding"] = false
	}

	dns, _ := props["dnsSettings"].(map[string]interface{})
	dns = cloneMap(dns)
	if dns == nil {
		dns = map[string]interface{}{}
	}
	existingDNS, _ := existingProps["dnsSettings"].(map[string]interface{})
	suffix := firstNonEmpty(
		stringVal(existingDNS["internalDomainNameSuffix"]),
		stringVal(dns["internalDomainNameSuffix"]),
		generateDNSSuffix(rg),
	)
	if _, ok := dns["dnsServers"]; !ok {
		dns["dnsServers"] = []interface{}{}
	}
	if _, ok := dns["appliedDnsServers"]; !ok {
		dns["appliedDnsServers"] = []interface{}{}
	}
	if _, ok := dns["internalDnsNameLabel"]; !ok {
		dns["internalDnsNameLabel"] = ""
	}
	dns["internalFqdn"] = name + "." + suffix
	dns["internalDomainNameSuffix"] = suffix
	props["dnsSettings"] = dns

	// IP configurations.
	existingByName := map[string]map[string]interface{}{}
	for i, raw := range nicIPConfigurations(existing) {
		cfg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		existingByName[ipConfigName(cfg, i)] = cfg
	}

	configsIn, _ := props["ipConfigurations"].([]interface{})
	if len(configsIn) == 0 {
		configsIn = []interface{}{map[string]interface{}{
			"name":       "ipconfig1",
			"properties": map[string]interface{}{},
		}}
	}

	explicitPrimary := false
	for _, raw := range configsIn {
		cfg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		cfgProps, _ := cfg["properties"].(map[string]interface{})
		if primary, ok := cfgProps["primary"].(bool); ok && primary {
			explicitPrimary = true
		}
	}

	builtConfigs := make([]interface{}, 0, len(configsIn))
	for i, raw := range configsIn {
		cfg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		cfg = cloneMap(cfg)
		cfgProps, _ := cfg["properties"].(map[string]interface{})
		cfgProps = cloneMap(cfgProps)
		if cfgProps == nil {
			cfgProps = map[string]interface{}{}
		}

		cfgName := ipConfigName(cfg, i)
		cfgID := ""
		if existingCfg, ok := existingByName[cfgName]; ok {
			cfgID = stringVal(existingCfg["id"])
		}
		if cfgID == "" {
			cfgID = stringVal(cfg["id"])
		}
		if cfgID == "" {
			cfgID = nicID + "/ipConfigurations/" + cfgName
		}

		method := stringVal(cfgProps["privateIPAllocationMethod"])
		if method == "" {
			method = "Dynamic"
		}
		cfgProps["privateIPAllocationMethod"] = method

		privateIP := stringVal(cfgProps["privateIPAddress"])
		if !(method == "Static" && privateIP != "") {
			if existingCfg, ok := existingByName[cfgName]; ok {
				existingCfgProps, _ := existingCfg["properties"].(map[string]interface{})
				if stored := stringVal(existingCfgProps["privateIPAddress"]); stored != "" {
					privateIP = stored
				}
			}
		}
		cfgProps["privateIPAddress"] = privateIP

		if stringVal(cfgProps["privateIPAddressVersion"]) == "" {
			cfgProps["privateIPAddressVersion"] = "IPv4"
		}
		cfgProps["provisioningState"] = "Succeeded"
		if explicitPrimary {
			primary, _ := cfgProps["primary"].(bool)
			cfgProps["primary"] = primary
		} else {
			cfgProps["primary"] = i == 0
		}
		if _, ok := cfgProps["applicationGatewayBackendAddressPools"]; !ok {
			cfgProps["applicationGatewayBackendAddressPools"] = []interface{}{}
		}
		if _, ok := cfgProps["loadBalancerBackendAddressPools"]; !ok {
			cfgProps["loadBalancerBackendAddressPools"] = []interface{}{}
		}
		if _, ok := cfgProps["loadBalancerInboundNatRules"]; !ok {
			cfgProps["loadBalancerInboundNatRules"] = []interface{}{}
		}

		cfg["id"] = cfgID
		cfg["name"] = cfgName
		cfg["type"] = "Microsoft.Network/networkInterfaces/ipConfigurations"
		cfg["etag"] = "W/\"miniblue\""
		cfg["properties"] = cfgProps
		builtConfigs = append(builtConfigs, cfg)
	}
	props["ipConfigurations"] = builtConfigs

	out := cloneMap(merged)
	if out == nil {
		out = map[string]interface{}{}
	}
	out["id"] = nicID
	out["name"] = name
	out["type"] = nicType
	if location := stringVal(out["location"]); location == "" {
		out["location"] = "eastus"
	}
	if tags, ok := out["tags"].(map[string]interface{}); !ok || tags == nil {
		out["tags"] = map[string]interface{}{}
	}
	out["etag"] = "W/\"miniblue\""
	out["properties"] = props
	stripInternalFields(out)
	return out
}

// stripInternalFields removes miniblue-internal "_"-prefixed keys before a
// record is returned to a client.
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

func (h *Handler) ListNetworkInterfaces(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	items := h.store.ListByPrefix("nic:" + sub + ":" + rg + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func (h *Handler) ListNetworkInterfacesInSubscription(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	items := h.store.ListByPrefix("nic:" + sub + ":")
	json.NewEncoder(w).Encode(map[string]interface{}{"value": items})
}

func (h *Handler) CreateOrUpdateNIC(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "nicName")

	var input map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		input = map[string]interface{}{}
	}
	if input == nil {
		input = map[string]interface{}{}
	}

	existing, exists := GetNIC(h.store, sub, rg, name)

	// A create without configs still gets Azure's default configuration.
	if !exists && !hasIPConfigurations(input) {
		props, _ := input["properties"].(map[string]interface{})
		if props == nil {
			props = map[string]interface{}{}
			input["properties"] = props
		}
		props["ipConfigurations"] = []interface{}{map[string]interface{}{
			"name":       "ipconfig1",
			"properties": map[string]interface{}{},
		}}
	}

	h.applyIPAllocation(input, existing)
	nic := buildNICResponse(sub, rg, name, input, existing)
	SetNIC(h.store, sub, rg, name, nic)

	if exists {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(nic)
}

func (h *Handler) GetNetworkInterface(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "nicName")

	nic, ok := GetNIC(h.store, sub, rg, name)
	if !ok {
		azerr.NotFound(w, nicType, name)
		return
	}
	json.NewEncoder(w).Encode(nic)
}

func (h *Handler) PatchNetworkInterface(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "nicName")

	existing, ok := GetNIC(h.store, sub, rg, name)
	if !ok {
		azerr.NotFound(w, nicType, name)
		return
	}

	var input map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		input = map[string]interface{}{}
	}
	if input == nil {
		input = map[string]interface{}{}
	}

	h.applyIPAllocation(input, existing)
	nic := buildNICResponse(sub, rg, name, input, existing)
	SetNIC(h.store, sub, rg, name, nic)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(nic)
}

func (h *Handler) DeleteNetworkInterface(w http.ResponseWriter, r *http.Request) {
	sub := chi.URLParam(r, "subscriptionId")
	rg := chi.URLParam(r, "resourceGroupName")
	name := chi.URLParam(r, "nicName")

	if !h.store.Delete(canonicalNICKey(h.store, sub, rg, name)) {
		azerr.NotFound(w, nicType, name)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
