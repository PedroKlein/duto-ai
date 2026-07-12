package config

// ResolveModel resolves a model name through the aliases map.
// If stepModel matches an alias key, returns the alias value.
// Otherwise returns stepModel as-is.
func ResolveModel(stepModel string, aliases map[string]string) string {
	if stepModel == "" {
		return ""
	}

	if resolved, ok := aliases[stepModel]; ok {
		return resolved
	}

	return stepModel
}
