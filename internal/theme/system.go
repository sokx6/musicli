package theme

import (
	"context"
	"fmt"
)

// ResolveMode returns the requested fixed mode or asks the platform when auto
// is selected. Platform errors fall back to dark and remain non-fatal.
func ResolveMode(requested string) (Mode, []string) {
	switch requested {
	case "light":
		return ModeLight, nil
	case "dark", "":
		return ModeDark, nil
	case "auto":
		mode, err := SystemMode(context.Background())
		if err != nil {
			return ModeDark, []string{fmt.Sprintf("system theme detection: %v; using dark", err)}
		}
		return mode, nil
	default:
		return ModeDark, []string{fmt.Sprintf("theme.mode %q invalid; using dark", requested)}
	}
}

// WatchSystemMode sends mode changes until ctx is cancelled. Unsupported
// platforms return immediately; fixed dark/light configurations never call it.
func WatchSystemMode(ctx context.Context, send func(Mode)) { watchSystemMode(ctx, send) }
