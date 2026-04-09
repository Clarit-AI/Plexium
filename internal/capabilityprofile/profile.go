package capabilityprofile

import "strings"

const (
	ConstrainedLocal     = "constrained-local"
	Balanced             = "balanced"
	FrontierLargeContext = "frontier-large-context"
	Default              = Balanced
)

func Normalize(profile string) string {
	switch strings.TrimSpace(strings.ToLower(profile)) {
	case ConstrainedLocal:
		return ConstrainedLocal
	case FrontierLargeContext:
		return FrontierLargeContext
	case Balanced, "":
		return Balanced
	default:
		return ""
	}
}
