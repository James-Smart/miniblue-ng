package frontdoor

import (
	"strings"

	"github.com/google/uuid"
)

const (
	profileType     = "Microsoft.Cdn/profiles"
	endpointType    = "Microsoft.Cdn/profiles/afdEndpoints"
	originGroupType = "Microsoft.Cdn/profiles/originGroups"
	originType      = "Microsoft.Cdn/profiles/originGroups/origins"
	ruleSetType     = "Microsoft.Cdn/profiles/ruleSets"
	ruleType        = "Microsoft.Cdn/profiles/ruleSets/rules"
	routeType       = "Microsoft.Cdn/profiles/afdEndpoints/routes"
)

// profileResponse builds the full profile response. It merges input over the
// existing record, then fills in the ARM envelope and the computed properties
// the provider reads. Generated values already present on the existing record
// are preserved so GETs and PATCHes never change them.
func profileResponse(sub, rg, name string, input, existing map[string]interface{}) map[string]interface{} {
	rec := mergeMaps(existing, input)

	rec["id"] = "/subscriptions/" + sub + "/resourceGroups/" + rg +
		"/providers/Microsoft.Cdn/profiles/" + name
	rec["name"] = name
	rec["type"] = profileType
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

	// frontDoorId is generated once and must survive PATCH and full-model PUTs.
	previousFrontDoorID := stringVal(mapVal(existing["properties"])["frontDoorId"])
	if previousFrontDoorID != "" {
		props["frontDoorId"] = previousFrontDoorID
	} else if stringVal(props["frontDoorId"]) == "" {
		props["frontDoorId"] = uuid.NewString()
	}

	if _, ok := props["originResponseTimeoutSeconds"]; !ok {
		props["originResponseTimeoutSeconds"] = float64(60)
	}
	props["provisioningState"] = "Succeeded"
	props["resourceState"] = "Active"
	rec["properties"] = props

	return rec
}

// endpointResponse builds an AFD endpoint response. The host name is generated
// once at create time and preserved from the existing record on updates.
func endpointResponse(parent, input, existing map[string]interface{}) map[string]interface{} {
	rec := mergeMaps(existing, input)

	props := cloneMap(mapVal(rec["properties"]))
	if props == nil {
		props = map[string]interface{}{}
	}

	hostName := stringVal(mapVal(existing["properties"])["hostName"])
	if hostName == "" {
		hostName = generatedEndpointHostName(stringVal(rec["name"]))
	}
	props["hostName"] = hostName

	if stringVal(props["enabledState"]) == "" {
		props["enabledState"] = "Enabled"
	}
	if stringVal(props["deploymentStatus"]) == "" {
		props["deploymentStatus"] = "NotStarted"
	}
	rec["properties"] = props

	return rec
}

// generatedEndpointHostName returns the stable-looking Azure Front Door host
// name for an endpoint: <lowercased name>-<6 hex>.z01.azurefd.net.
func generatedEndpointHostName(name string) string {
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:6]
	return strings.ToLower(name) + "-" + suffix + ".z01.azurefd.net"
}

// originGroupResponse builds an origin group response. Health probe and load
// balancing settings are echoed, with the provider's defaults filled in for
// any omitted field.
func originGroupResponse(parent, input, existing map[string]interface{}) map[string]interface{} {
	rec := mergeMaps(existing, input)

	props := cloneMap(mapVal(rec["properties"]))
	if props == nil {
		props = map[string]interface{}{}
	}

	health := cloneMap(mapVal(props["healthProbeSettings"]))
	if health == nil {
		health = map[string]interface{}{}
	}
	defaultNumber(health, "probeIntervalInSeconds", 30)
	defaultString(health, "probePath", "/")
	defaultString(health, "probeRequestType", "HEAD")
	defaultString(health, "probeProtocol", "Http")
	props["healthProbeSettings"] = health

	loadBalancing := cloneMap(mapVal(props["loadBalancingSettings"]))
	if loadBalancing == nil {
		loadBalancing = map[string]interface{}{}
	}
	defaultNumber(loadBalancing, "sampleSize", 4)
	defaultNumber(loadBalancing, "successfulSamplesRequired", 3)
	defaultNumber(loadBalancing, "additionalLatencyInMilliseconds", 0)
	props["loadBalancingSettings"] = loadBalancing

	if stringVal(props["sessionAffinityState"]) == "" {
		props["sessionAffinityState"] = "Disabled"
	}
	if _, ok := props["trafficRestorationTimeToHealedOrNewEndpointsInMinutes"]; !ok {
		props["trafficRestorationTimeToHealedOrNewEndpointsInMinutes"] = float64(10)
	}
	rec["properties"] = props

	return rec
}

// originResponse builds an origin response. It records the parent origin group
// and fills in the defaults the provider expects for omitted fields.
func originResponse(parent, input, existing map[string]interface{}) map[string]interface{} {
	rec := mergeMaps(existing, input)

	props := cloneMap(mapVal(rec["properties"]))
	if props == nil {
		props = map[string]interface{}{}
	}

	props["originGroupName"] = stringVal(parent["name"])

	if stringVal(props["originHostHeader"]) == "" {
		props["originHostHeader"] = stringVal(props["hostName"])
	}
	defaultNumber(props, "httpPort", 80)
	defaultNumber(props, "httpsPort", 443)
	defaultNumber(props, "priority", 1)
	defaultNumber(props, "weight", 1000)
	if stringVal(props["enabledState"]) == "" {
		props["enabledState"] = "Enabled"
	}
	if _, ok := props["enforceCertificateNameCheck"]; !ok {
		props["enforceCertificateNameCheck"] = true
	}
	rec["properties"] = props

	return rec
}

// ruleSetResponse builds a rule set response. Rule sets carry only the common
// computed properties.
func ruleSetResponse(parent, input, existing map[string]interface{}) map[string]interface{} {
	rec := mergeMaps(existing, input)

	props := cloneMap(mapVal(rec["properties"]))
	if props == nil {
		props = map[string]interface{}{}
	}
	defaultString(props, "deploymentStatus", "NotStarted")
	rec["properties"] = props

	return rec
}

// ruleResponse builds a rule response. Conditions and actions are opaque: they
// are echoed exactly as supplied, without validating their shape.
func ruleResponse(parent, input, existing map[string]interface{}) map[string]interface{} {
	rec := mergeMaps(existing, input)

	props := cloneMap(mapVal(rec["properties"]))
	if props == nil {
		props = map[string]interface{}{}
	}

	defaultString(props, "matchProcessingBehavior", "Continue")
	if _, ok := props["conditions"]; !ok {
		props["conditions"] = []interface{}{}
	}
	if _, ok := props["actions"]; !ok {
		props["actions"] = []interface{}{}
	}
	defaultString(props, "deploymentStatus", "NotStarted")
	rec["properties"] = props

	return rec
}

// routeResponse builds a route response. Association lists and the routing
// settings are echoed with the provider's defaults filled in. The custom domain
// list is preserved exactly as written, including its order.
func routeResponse(parent, input, existing map[string]interface{}) map[string]interface{} {
	rec := mergeMaps(existing, input)

	props := cloneMap(mapVal(rec["properties"]))
	if props == nil {
		props = map[string]interface{}{}
	}

	props["endpointName"] = stringVal(parent["name"])

	if _, ok := props["supportedProtocols"]; !ok {
		props["supportedProtocols"] = []interface{}{"Https"}
	}
	if _, ok := props["patternsToMatch"]; !ok {
		props["patternsToMatch"] = []interface{}{"/*"}
	}
	defaultString(props, "forwardingProtocol", "MatchRequest")
	defaultString(props, "linkToDefaultDomain", "Enabled")
	defaultString(props, "httpsRedirect", "Enabled")
	defaultString(props, "enabledState", "Enabled")
	if _, ok := props["customDomains"]; !ok {
		props["customDomains"] = []interface{}{}
	}
	if _, ok := props["ruleSets"]; !ok {
		props["ruleSets"] = []interface{}{}
	}
	defaultString(props, "deploymentStatus", "NotStarted")
	rec["properties"] = props

	return rec
}

// defaultNumber sets key to value when the map does not already hold it.
func defaultNumber(m map[string]interface{}, key string, value float64) {
	if _, ok := m[key]; !ok {
		m[key] = value
	}
}

// defaultString sets key to value when the map does not already hold a
// non-empty value.
func defaultString(m map[string]interface{}, key, value string) {
	if stringVal(m[key]) == "" {
		m[key] = value
	}
}
