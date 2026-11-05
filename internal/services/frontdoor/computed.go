package frontdoor

import "github.com/google/uuid"

const profileType = "Microsoft.Cdn/profiles"

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
