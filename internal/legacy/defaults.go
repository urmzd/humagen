package legacy

// AutoDefault is what a default read before the dynamic router: "auto", the
// router that picked one agent per message. It is read as "*", everyone:
// every available agent decides for itself whether to take the message.
const AutoDefault = "auto"

// OpenDefault is the default AutoDefault is read as.
const OpenDefault = "*"

// Default returns the current spelling of a default, leaving other values
// unchanged.
func Default(name string) string {
	if name == AutoDefault {
		return OpenDefault
	}
	return name
}
