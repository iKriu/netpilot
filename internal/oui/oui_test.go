package oui

import "testing"

func TestVendor(t *testing.T) {
	if Count() < 30000 {
		t.Fatalf("база слишком маленькая: %d", Count())
	}
	for mac, want := range map[string]string{
		"00:00:0c:12:34:56": "Cisco",
		"e4-5f-01-25-bc-72": "Raspberry Pi Trading",
		"da:a1:19:00:00:01": "", // случайный MAC
		"not-a-mac":         "",
	} {
		if got := Vendor(mac); got != want {
			t.Errorf("%s: got %q, want %q", mac, got, want)
		}
	}
}

func TestTypeHint(t *testing.T) {
	for vendor, want := range map[string]string{
		"Espressif":            "iot",
		"TP-LINK":              "network",
		"Raspberry Pi Trading": "computer",
		"Apple":                "",
	} {
		if got := TypeHint(vendor); got != want {
			t.Errorf("%s: got %q, want %q", vendor, got, want)
		}
	}
}
