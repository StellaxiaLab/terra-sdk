package pkgformat

import (
	"strings"
)

const contributionsServiceKey = "service"

const contributionsServiceProviderIDKey = "providerId"

const contributionsServiceBaseURLKey = "baseUrl"

// ManifestWrapsExternalService reports whether the manifest declares the
// external service contribution with both fields the Gateway needs to bind it.
// A partial declaration answers false: half an address is not an
// implementation, and admitting it would let a module install with nothing
// behind its routes.
func ManifestWrapsExternalService(manifest Manifest) bool {
	service, ok := manifest.Contributions[contributionsServiceKey].(map[string]any)
	if !ok {
		return false
	}
	providerID, _ := service[contributionsServiceProviderIDKey].(string)
	baseURL, _ := service[contributionsServiceBaseURLKey].(string)
	return strings.TrimSpace(providerID) != "" && strings.TrimSpace(baseURL) != ""
}
