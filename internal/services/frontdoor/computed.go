package frontdoor

import (
	"strings"

	"github.com/google/uuid"
)

const (
	profileType  = "Microsoft.Cdn/profiles"
	endpointType = "Microsoft.Cdn/profiles/afdEndpoints"
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
