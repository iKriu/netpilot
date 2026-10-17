package app

import (
	"strings"
	"testing"
	"time"

	"netpilot/internal/model"
	"netpilot/internal/scan"
	"netpilot/internal/store"
)

func TestMergeStaleIPs(t *testing.T) {
	const (
		macTablet = "02:00:00:00:00:01"
		macPhone  = "02:00:00:00:00:02"
		macGuest  = "02:00:00:00:00:03"
		ipX       = "192.168.1.50"
		ipY       = "192.168.1.60"
	)
	now := time.Now().Unix()
	dev := func(id, mac, ip, route string, pinned bool) model.Device {
		return model.Device{ID: id, Name: id, MAC: mac, IP: ip, Route: route, Pinned: pinned, Tags: []string{}, SeenAt: now, FirstSeen: now}
	}
	cases := []struct {
		name     string
		devices  []model.Device
		hosts    []scan.Host
		wantIP   map[string]string // id → IP после merge; "new" — добавленное устройство
		warn     bool
		updated  bool
		noChange bool
	}{
		{
			name:    "ушедший планшет без интернета теряет адрес, гость его получает",
			devices: []model.Device{dev("tablet", macTablet, ipX, "block", false)},
			hosts:   []scan.Host{{IP: ipX, MAC: macGuest}},
			wantIP:  map[string]string{"tablet": "", "new": ipX},
			updated: true,
		},
		{
			name:    "известное устройство переехало на адрес ушедшего",
			devices: []model.Device{dev("tablet", macTablet, ipX, "direct", false), dev("phone", macPhone, ipY, "", false)},
			hosts:   []scan.Host{{IP: ipX, MAC: macPhone}},
			wantIP:  map[string]string{"tablet": "", "phone": ipX},
			updated: true,
		},
		{
			name:    "закреплённый адрес занят новичком: закрепление остаётся, новичку адрес не даём",
			devices: []model.Device{dev("tablet", macTablet, ipX, "direct", true)},
			hosts:   []scan.Host{{IP: ipX, MAC: macGuest}},
			wantIP:  map[string]string{"tablet": ipX, "new": ""},
			warn:    true,
			updated: true,
		},
		{
			name:    "закреплённый адрес занят известным устройством: его старый адрес снимается",
			devices: []model.Device{dev("tablet", macTablet, ipX, "block", true), dev("phone", macPhone, ipY, "proxy", false)},
			hosts:   []scan.Host{{IP: ipX, MAC: macPhone}},
			wantIP:  map[string]string{"tablet": ipX, "phone": ""},
			warn:    true,
			updated: true,
		},
		{
			name:    "закреплённое устройство на чужом адресе снимает устаревший адрес, свой сохраняет",
			devices: []model.Device{dev("tablet", macTablet, ipY, "", true), dev("phone", macPhone, ipX, "block", false)},
			hosts:   []scan.Host{{IP: ipX, MAC: macTablet}},
			wantIP:  map[string]string{"tablet": ipY, "phone": ""},
			updated: true,
		},
		{
			name:    "добавленное вручную без MAC не теряет адрес",
			devices: []model.Device{dev("manual", "", ipX, "direct", false), dev("phone", macPhone, ipY, "", false)},
			hosts:   []scan.Host{{IP: ipX, MAC: macPhone}},
			wantIP:  map[string]string{"manual": ipX, "phone": ipX},
			updated: true,
		},
		{
			name:     "устройство на своём адресе — без изменений",
			devices:  []model.Device{dev("tablet", macTablet, ipX, "block", false), dev("phone", macPhone, ipY, "", false)},
			hosts:    []scan.Host{{IP: ipX, MAC: macTablet}},
			wantIP:   map[string]string{"tablet": ipX, "phone": ipY},
			noChange: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, err := store.Open(t.TempDir(), nil)
			if err != nil {
				t.Fatal(err)
			}
			_ = st.Update(func(c *model.Config) error { c.Devices = tc.devices; return nil })
			a := &App{Store: st, DataDir: t.TempDir()}
			rep, err := a.merge(tc.hosts)
			if err != nil {
				t.Fatal(err)
			}
			if tc.updated && rep.Updated+rep.Added == 0 {
				t.Errorf("изменения не учтены: %+v", rep)
			}
			if tc.noChange && (rep.Updated != 0 || rep.Added != 0) {
				t.Errorf("лишние изменения: %+v", rep)
			}
			got := map[string]string{}
			for _, d := range st.Get().Devices {
				id := d.ID
				if id != "tablet" && id != "phone" && id != "manual" {
					id = "new"
				}
				got[id] = d.IP
			}
			for id, ip := range tc.wantIP {
				if g, ok := got[id]; !ok || g != ip {
					t.Errorf("%s: IP %q (есть: %v), ожидался %q", id, g, ok, ip)
				}
			}
			hasWarn := false
			for _, w := range a.net.warnings {
				hasWarn = hasWarn || strings.Contains(w, "закреплён")
			}
			if hasWarn != tc.warn {
				t.Errorf("предупреждение: %v, ожидалось %v (%v)", hasWarn, tc.warn, a.net.warnings)
			}
		})
	}
}
