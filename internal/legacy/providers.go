package legacy

// providerNames are model provider names stored data may hold that have
// since been renamed, each with the name it now reads as. The development
// spec and the built-in agents' chain once named Google's provider gemini.
var providerNames = map[string]string{"gemini": "google"}

// ProviderName is what a stored model provider name reads as, and whether it
// was renamed. A caller reads it so only where nothing by the old name
// exists: a deployment that still declares the old name keeps it.
func ProviderName(name string) (string, bool) {
	to, ok := providerNames[name]
	return to, ok
}
