package endpoints

import "os"

const defaultHTTPPort = "4566"

// HTTPPort returns the port miniblue serves its local HTTP API and data plane on.
func HTTPPort() string {
	if port := os.Getenv("PORT"); port != "" {
		return port
	}
	return defaultHTTPPort
}

// HTTPBase returns the local HTTP base URL.
func HTTPBase() string {
	return "http://localhost:" + HTTPPort()
}

// StorageSuffix is the host suffix advertised by the cloud metadata endpoint.
// Storage account endpoints must use the same suffix so clients that validate
// one against the other (notably azurerm 5.x) can parse them.
func StorageSuffix() string {
	return "localhost:" + HTTPPort()
}
