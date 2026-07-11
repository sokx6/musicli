//go:build !linux

package theme

import (
	"context"
	"fmt"
)

func SystemMode(context.Context) (Mode, error) {
	return ModeDark, fmt.Errorf("system theme detection unavailable")
}
func watchSystemMode(context.Context, func(Mode)) {}
