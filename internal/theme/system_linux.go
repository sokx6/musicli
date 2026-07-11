//go:build linux

package theme

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

func SystemMode(_ context.Context) (Mode, error) {
	if mode, err := portalMode(); err == nil {
		return mode, nil
	}
	out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "color-scheme").Output()
	if err != nil {
		return ModeDark, err
	}
	if strings.Contains(string(out), "prefer-light") {
		return ModeLight, nil
	}
	return ModeDark, nil
}

func portalMode() (Mode, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return ModeDark, err
	}
	defer conn.Close()
	var value dbus.Variant
	err = conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop").Call(
		"org.freedesktop.portal.Settings.Read", 0, "org.freedesktop.appearance", "color-scheme").Store(&value)
	if err != nil {
		return ModeDark, err
	}
	if scheme, ok := portalScheme(value); ok {
		return modeFromPortalScheme(scheme), nil
	}
	return ModeDark, nil
}

// The portal standard defines 0 as no preference, 1 as prefer dark, and 2 as
// prefer light. Keep no preference on the application's dark fallback.
func modeFromPortalScheme(scheme uint32) Mode {
	if scheme == 2 {
		return ModeLight
	}
	return ModeDark
}

// portalScheme unwraps the variant returned by the portal settings API. The
// API commonly returns a variant containing another typed variant.
func portalScheme(value dbus.Variant) (uint32, bool) {
	raw := value.Value()
	for {
		nested, ok := raw.(dbus.Variant)
		if !ok {
			break
		}
		raw = nested.Value()
	}
	scheme, ok := raw.(uint32)
	return scheme, ok
}

func watchSystemMode(ctx context.Context, send func(Mode)) {
	if watchPortalMode(ctx, send) {
		return
	}
	current, err := SystemMode(ctx)
	if err != nil {
		return
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next, err := SystemMode(ctx)
			if err == nil && next != current {
				current = next
				send(next)
			}
		}
	}
}

func watchPortalMode(ctx context.Context, send func(Mode)) bool {
	current, err := portalMode()
	if err != nil {
		// A session bus can exist without the settings portal. Fall back to
		// gsettings polling instead of waiting forever for unavailable signals.
		return false
	}
	conn, err := dbus.SessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	if err := conn.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.portal.Settings"), dbus.WithMatchMember("SettingChanged")); err != nil {
		return false
	}
	signals := make(chan *dbus.Signal, 4)
	conn.Signal(signals)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	notify := func(next Mode) {
		if next != current {
			current = next
			send(next)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return true
		case <-ticker.C:
			// Some portal backends update Settings.Read but omit SettingChanged.
			// Polling the authoritative portal value keeps desktop bar toggles in
			// sync without giving up immediate signal-based updates.
			next, err := portalMode()
			if err != nil {
				return false
			}
			notify(next)
		case signal := <-signals:
			if signal == nil || len(signal.Body) < 3 {
				continue
			}
			namespace, _ := signal.Body[0].(string)
			key, _ := signal.Body[1].(string)
			if namespace != "org.freedesktop.appearance" || key != "color-scheme" {
				continue
			}
			value, ok := signal.Body[2].(dbus.Variant)
			if !ok {
				continue
			}
			scheme, ok := portalScheme(value)
			if !ok {
				continue
			}
			notify(modeFromPortalScheme(scheme))
		}
	}
}
