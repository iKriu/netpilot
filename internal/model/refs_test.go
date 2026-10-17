package model

import (
	"slices"
	"testing"
)

func TestReferences(t *testing.T) {
	cfg := Defaults()
	cfg.Settings.DefaultTarget = "n1"
	cfg.Nodes = []Node{{ID: "n1", Name: "A"}, {ID: "n2", Name: "B"}}
	cfg.Groups = []Group{
		{ID: "g1", Name: "Европа", NodeIDs: []string{"n1", "n2"}},
		{ID: "g2", Name: "Пустая"},
	}
	cfg.Rules = []Rule{
		{ID: "r1", Name: "YouTube", Where: Where{Action: "proxy", Target: "n1"}},
		{ID: "r2", Name: "Банки", Where: Where{Action: "direct"}},
		{ID: "r3", Where: Where{Action: "proxy", Target: "g1"}},
	}
	cfg.Devices = []Device{
		{ID: "d1", Name: "ТВ", Route: "proxy", RouteTarget: "g1"},
		{ID: "d2", IP: "192.168.1.5", Route: "proxy", RouteTarget: "n1"},
	}
	cases := []struct {
		id   string
		want []string
	}{
		{"n1", []string{"сервер по умолчанию в настройках", "правило «YouTube»", "устройство «192.168.1.5»", "группа «Европа»"}},
		{"n2", []string{"группа «Европа»"}},
		{"g1", []string{"правило «r3»", "устройство «ТВ»"}},
		{"g2", nil},
		{"", nil},
	}
	for _, c := range cases {
		if got := References(cfg, c.id); !slices.Equal(got, c.want) {
			t.Errorf("References(%q) = %q, ожидалось %q", c.id, got, c.want)
		}
	}
	if References(nil, "n1") != nil {
		t.Error("nil-конфигурация")
	}
}
