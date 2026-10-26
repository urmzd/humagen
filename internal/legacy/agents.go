package legacy

// BuiltinAgents maps the handles from before the rename to their current
// handles. The old revisions remain addressable for saved runs and grants.
func BuiltinAgents() map[string]string {
	return map[string]string{
		"warp-writer":  "humagen-writer",
		"warp-analyst": "humagen-analyst",
		"warp-builder": "humagen-builder",
	}
}

// IsBuiltinAgent reports whether name is a handle from before the rename.
func IsBuiltinAgent(name string) bool { _, ok := BuiltinAgents()[name]; return ok }

// BuiltinAgent returns the current spelling, leaving other names unchanged.
func BuiltinAgent(name string) string {
	if current, ok := BuiltinAgents()[name]; ok {
		return current
	}
	return name
}

// BuiltinAgentKey keeps an agent's color reservation at its original key.
func BuiltinAgentKey(name string) string {
	for old, current := range BuiltinAgents() {
		if name == current {
			return old
		}
	}
	return name
}
