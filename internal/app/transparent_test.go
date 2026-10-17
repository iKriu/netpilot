package app

import (
	"slices"
	"testing"

	"netpilot/internal/model"
)

func TestFirewallSpecSkipsBadAddrs(t *testing.T) {
	a := &App{}
	a.net.v6 = map[string][]string{"tv": {"2001:db8::5", "не-адрес"}}
	cfg := &model.Config{Devices: []model.Device{
		{ID: "tv", IP: "192.168.1.5", Route: "block"},
		{ID: "gone", IP: "", Route: "block"},
		{ID: "typo", IP: "192.168.1.300", Route: "direct"},
		{ID: "zone", IP: "fe80::1%eth0", Route: "direct"},
		{ID: "pc", IP: "192.168.1.7", Route: "direct"},
	}}
	spec := a.firewallSpec(cfg)
	if want := []string{"2001:db8::5", "192.168.1.5"}; !slices.Equal(spec.Block, want) {
		t.Errorf("Block = %v, ожидалось %v", spec.Block, want)
	}
	if want := []string{"192.168.1.7"}; !slices.Equal(spec.Direct, want) {
		t.Errorf("Direct = %v, ожидалось %v", spec.Direct, want)
	}
}
