//go:build linux

package theme

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestModeFromPortalScheme(t *testing.T) {
	tests := []struct {
		scheme uint32
		want   Mode
	}{
		{scheme: 0, want: ModeDark},
		{scheme: 1, want: ModeDark},
		{scheme: 2, want: ModeLight},
	}
	for _, test := range tests {
		if got := modeFromPortalScheme(test.scheme); got != test.want {
			t.Errorf("scheme %d: got %v, want %v", test.scheme, got, test.want)
		}
	}
}

func TestPortalSchemeUnwrapsNestedVariant(t *testing.T) {
	value := dbus.MakeVariant(dbus.MakeVariant(uint32(2)))
	if got, ok := portalScheme(value); !ok || got != 2 {
		t.Fatalf("portalScheme = %d, %t; want 2, true", got, ok)
	}
}
