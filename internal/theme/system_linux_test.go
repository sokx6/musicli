//go:build linux

package theme

import "testing"

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
