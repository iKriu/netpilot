package app

import (
	"testing"
	"time"

	"netpilot/internal/model"
)

func TestPrettyName(t *testing.T) {
	for in, want := range map[string]string{
		"Ivans-iPhone":       "Ivans iPhone",
		"iphone":             "iPhone",
		"Galaxy-S23":         "Galaxy S23",
		"android-3f2a9c01bd": "Android",
		"DESKTOP-7K2L9QF":    "DESKTOP-7K2L9QF",
		"Redmi_Note_12":      "Redmi Note 12",
		"iPhoneNikita":       "iPhone Nikita",
	} {
		if got := prettyName(in); got != want {
			t.Errorf("prettyName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAutoNamed(t *testing.T) {
	mac := "02:11:22:04:4e:7b"
	for _, c := range []struct {
		d       model.Device
		oldHost string
		want    bool
	}{
		{model.Device{Name: "Устройство 044E7B", MAC: mac}, "", true},
		{model.Device{Name: "Apple 044E7B", MAC: mac, Vendor: "Apple"}, "", true},
		{model.Device{Name: "Ivans-iPhone", MAC: mac}, "Ivans-iPhone", true},
		{model.Device{Name: "Телефон Маши", MAC: mac}, "", false},
		{model.Device{Name: "Телефон Маши", MAC: mac}, "Ivans-iPhone", false},
		{model.Device{Name: "Кухня 044E7B", MAC: mac}, "", false},
	} {
		if got := autoNamed(c.d, c.oldHost); got != c.want {
			t.Errorf("autoNamed(%q, %q) = %v", c.d.Name, c.oldHost, got)
		}
	}
}

func TestForgotten(t *testing.T) {
	now := time.Now()
	old := now.Add(-20 * 24 * time.Hour).Unix()
	recent := now.Add(-2 * 24 * time.Hour).Unix()
	mac := "02:11:22:04:4e:7b"
	cfg := &model.Config{Rules: []model.Rule{{Who: model.Who{DeviceIDs: []string{"inrule"}}}}}
	for _, c := range []struct {
		d    model.Device
		want bool
	}{
		{model.Device{ID: "x", Name: "Устройство 044E7B", MAC: mac, SeenAt: old}, true},                    // старый случайный MAC
		{model.Device{ID: "x", Name: "Устройство 044E7B", MAC: mac, SeenAt: recent}, false},                // был недавно
		{model.Device{ID: "x", Name: "Устройство 044E7B", MAC: mac, SeenAt: old, LastUsed: recent}, false}, // пользовался NetPilot
		{model.Device{ID: "x", Name: "Телефон Маши", MAC: mac, SeenAt: old}, false},                        // переименован человеком
		{model.Device{ID: "x", Name: "Устройство 044E7B", MAC: mac, SeenAt: old, Pinned: true}, false},     // закреплён IP
		{model.Device{ID: "x", Name: "Устройство 044E7B", MAC: mac, SeenAt: old, Route: "proxy"}, false},   // свой маршрут
		{model.Device{ID: "inrule", Name: "Устройство 044E7B", MAC: mac, SeenAt: old}, false},              // в правиле
	} {
		if got := forgotten(cfg, c.d, now); got != c.want {
			t.Errorf("forgotten(%+v) = %v", c.d, got)
		}
	}
}
