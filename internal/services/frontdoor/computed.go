package frontdoor

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	profileType        = "Microsoft.Cdn/profiles"
	endpointType       = "Microsoft.Cdn/profiles/afdEndpoints"
	originGroupType    = "Microsoft.Cdn/profiles/originGroups"
	originType         = "Microsoft.Cdn/profiles/originGroups/origins"
	ruleSetType        = "Microsoft.Cdn/profiles/ruleSets"
	ruleType           = "Microsoft.Cdn/profiles/ruleSets/rules"
	routeType          = "Microsoft.Cdn/profiles/afdEndpoints/routes"
	customDomainType   = "Microsoft.Cdn/profiles/customDomains"
	secretType         = "Microsoft.Cdn/profiles/secrets"
	securityPolicyType = "Microsoft.Cdn/profiles/securityPolicies"
	wafPolicyType      = "Microsoft.Network/FrontDoorWebApplicationFirewallPolicies"
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
	rec := cloneInput(input)

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
	rec := cloneInput(input)

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
	rec := cloneInput(input)

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
	rec := cloneInput(input)

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
	rec := cloneInput(input)

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
	rec := cloneInput(input)

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
	// Only apply the empty-list defaults on create. A PATCH that explicitly
	// nulls the association list must leave it absent, including on later GETs.
	if _, ok := props["customDomains"]; !ok && existing == nil {
		props["customDomains"] = []interface{}{}
	}
	if _, ok := props["ruleSets"]; !ok && existing == nil {
		props["ruleSets"] = []interface{}{}
	}
	defaultString(props, "deploymentStatus", "NotStarted")
	rec["properties"] = props

	return rec
}

// customDomainResponse builds a custom domain response. The validation token
// and expiration are generated once and preserved from the existing record on
// later PUTs and PATCHes, so the provider never sees them drift.
func customDomainResponse(parent, input, existing map[string]interface{}) map[string]interface{} {
	rec := cloneInput(input)

	props := cloneMap(mapVal(rec["properties"]))
	if props == nil {
		props = map[string]interface{}{}
	}

	previous := mapVal(mapVal(existing["properties"])["validationProperties"])

	validation := cloneMap(mapVal(props["validationProperties"]))
	if validation == nil {
		validation = map[string]interface{}{}
	}

	token := stringVal(validation["validationToken"])
	if token == "" {
		token = stringVal(previous["validationToken"])
	}
	if token == "" {
		token = newValidationToken()
	}
	validation["validationToken"] = token

	expiration := stringVal(validation["expirationDate"])
	if expiration == "" {
		expiration = stringVal(previous["expirationDate"])
	}
	if expiration == "" {
		expiration = time.Now().UTC().Add(7 * 24 * time.Hour).Format(time.RFC3339)
	}
	validation["expirationDate"] = expiration

	defaultString(validation, "validationState", "Pending")
	props["validationProperties"] = validation
	defaultString(props, "domainValidationState", "Pending")
	defaultString(props, "deploymentStatus", "NotStarted")
	rec["properties"] = props

	return rec
}

// secretResponse builds a secret response. The tagged-union parameters are
// echoed as supplied. CustomerCertificate secrets get the expiration and
// subject alternative names Azure fills in, so the provider's flatten does not
// see zero values for fields it expects.
func secretResponse(parent, input, existing map[string]interface{}) map[string]interface{} {
	rec := cloneInput(input)

	props := cloneMap(mapVal(rec["properties"]))
	if props == nil {
		props = map[string]interface{}{}
	}

	// Only echo parameters when present. A PATCH that explicitly nulls them must
	// leave the key absent rather than recreating an empty tagged union.
	if params := cloneMap(mapVal(props["parameters"])); params != nil {
		if stringVal(params["type"]) == "CustomerCertificate" {
			if stringVal(params["expirationDate"]) == "" {
				params["expirationDate"] = time.Now().UTC().Add(7 * 24 * time.Hour).Format(time.RFC3339)
			}
			if v, ok := params["subjectAlternativeNames"]; !ok || v == nil {
				params["subjectAlternativeNames"] = []interface{}{}
			}
		}
		props["parameters"] = params
	}
	defaultString(props, "deploymentStatus", "NotStarted")
	rec["properties"] = props

	return rec
}

// securityPolicyResponse builds a security policy response. The tagged-union
// parameters are echoed as supplied; each association domain is marked active
// when the caller did not say otherwise, and the match patterns are preserved.
func securityPolicyResponse(parent, input, existing map[string]interface{}) map[string]interface{} {
	rec := cloneInput(input)

	props := cloneMap(mapVal(rec["properties"]))
	if props == nil {
		props = map[string]interface{}{}
	}

	params := cloneMap(mapVal(props["parameters"]))
	if params == nil {
		params = map[string]interface{}{}
	}
	if associations, ok := params["associations"].([]interface{}); ok {
		for _, entry := range associations {
			association := mapVal(entry)
			domains, ok := association["domains"].([]interface{})
			if !ok {
				continue
			}
			for _, domainEntry := range domains {
				domain := mapVal(domainEntry)
				if domain == nil {
					continue
				}
				if v, ok := domain["isActive"]; !ok || v == nil {
					domain["isActive"] = true
				}
			}
		}
	}
	props["parameters"] = params
	rec["properties"] = props

	return rec
}

// wafPolicyResponse builds a Front Door WAF policy response. WAF policies are
// provided by Microsoft.Network rather than Microsoft.Cdn, so they carry their
// own envelope and a smaller set of computed properties. Policy settings are
// merged over the Azure defaults, custom rules default to an empty rule list,
// and managed rules are echoed only when the caller supplied them (Premium
// SKU). The frontend endpoint links array is always present.
func wafPolicyResponse(sub, rg, name string, input, existing map[string]interface{}) map[string]interface{} {
	rec := mergeMaps(existing, input)

	rec["id"] = "/subscriptions/" + sub + "/resourceGroups/" + rg +
		"/providers/Microsoft.Network/FrontDoorWebApplicationFirewallPolicies/" + name
	rec["name"] = name
	rec["type"] = wafPolicyType
	rec["location"] = "Global"
	if tags, ok := rec["tags"].(map[string]interface{}); !ok || tags == nil {
		rec["tags"] = map[string]interface{}{}
	}

	props := cloneMap(mapVal(rec["properties"]))
	if props == nil {
		props = map[string]interface{}{}
	}

	settings := cloneMap(mapVal(props["policySettings"]))
	if settings == nil {
		settings = map[string]interface{}{}
	}
	defaultString(settings, "enabledState", "Enabled")
	defaultString(settings, "mode", "Prevention")
	defaultString(settings, "requestBodyCheck", "Enabled")
	props["policySettings"] = settings

	customRules := cloneMap(mapVal(props["customRules"]))
	if customRules == nil {
		customRules = map[string]interface{}{}
	}
	if _, ok := customRules["rules"]; !ok {
		customRules["rules"] = []interface{}{}
	}
	props["customRules"] = customRules

	props["frontendEndpointLinks"] = []interface{}{}
	props["provisioningState"] = "Succeeded"
	rec["properties"] = props

	return rec
}

// newValidationToken returns 32 lowercase hex characters, the shape Azure uses
// for a custom domain's DNS TXT validation token.
func newValidationToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	return hex.EncodeToString(b)
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
