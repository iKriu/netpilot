package scan

import (
	"net/netip"
	"testing"
)

func TestParseArpA(t *testing.T) {
	windows := `
Интерфейс: 192.168.0.219 --- 0x7
  адрес в Интернете      Физический адрес      Тип
  192.168.0.1           d0-27-88-df-d7-20     динамический
  192.168.0.255         ff-ff-ff-ff-ff-ff     статический
  224.0.0.22            01-00-5e-00-00-16     статический
`
	mac := `? (192.168.1.7) at 0:1b:2:aa:b:cc on en0 ifscope [ethernet]
? (192.168.1.8) at (incomplete) on en0 ifscope [ethernet]`

	got := parseArpA(windows)
	if len(got) != 3 || got[0].IP != "192.168.0.1" || got[0].MAC != "d0:27:88:df:d7:20" {
		t.Errorf("windows: %+v", got)
	}
	got = parseArpA(mac)
	if len(got) != 1 || got[0].MAC != "00:1b:02:aa:0b:cc" {
		t.Errorf("macos: %+v", got)
	}
}

func TestUsableMAC(t *testing.T) {
	for mac, want := range map[string]bool{
		"d0:27:88:df:d7:20": true,
		"ff:ff:ff:ff:ff:ff": false, // широковещательный
		"01:00:5e:00:00:16": false, // multicast
		"00:00:00:00:00:00": false,
	} {
		if usableMAC(mac) != want {
			t.Errorf("%s: want %v", mac, want)
		}
	}
	if !RandomMAC("da:a1:19:00:00:01") || RandomMAC("d0:27:88:df:d7:20") {
		t.Error("RandomMAC")
	}
}

func TestIsBroadcast(t *testing.T) {
	p := netip.MustParsePrefix("192.168.1.0/24")
	if !isBroadcast(p, netip.MustParseAddr("192.168.1.255")) || isBroadcast(p, netip.MustParseAddr("192.168.1.254")) {
		t.Error("isBroadcast")
	}
}

func TestParseNBSTAT(t *testing.T) {
	resp := make([]byte, 12+34+10)
	resp = append(resp, 2) // два имени
	entry := func(name string, suffix byte, flags uint16) []byte {
		b := []byte(name)
		for len(b) < 15 {
			b = append(b, ' ')
		}
		return append(b, suffix, byte(flags>>8), byte(flags))
	}
	resp = append(resp, entry("WORKGROUP", 0x00, 0x8400)...) // групповое — пропускается
	resp = append(resp, entry("DESKTOP-7K2", 0x00, 0x0400)...)
	if got := parseNBSTAT(resp); got != "DESKTOP-7K2" {
		t.Errorf("got %q", got)
	}
}

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"iPhone-Masha.lan.":         "iPhone-Masha",
		"nas.local":                 "nas",
		"192-168-1-5.provider.net.": "",
	} {
		if got := cleanName(in); got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
}

func TestParseRoutePrint(t *testing.T) {
	out := `
IPv4 таблица маршрута
===========================================================================
Активные маршруты:
Сетевой адрес           Маска сети      Адрес шлюза       Интерфейс  Метрика
          0.0.0.0          0.0.0.0      192.168.0.1    192.168.0.219     25
===========================================================================
Постоянные маршруты:
  Отсутствует
`
	got := parseRoutePrint(out)
	if len(got) != 1 || got[0] != "192.168.0.1" {
		t.Errorf("got %v", got)
	}
}

func TestParseNeigh6(t *testing.T) {
	out := "fe80::1 dev br-lan lladdr aa:bb:cc:dd:ee:01 router REACHABLE\n" +
		"2a02:6b8::5 dev br-lan lladdr AA:BB:CC:DD:EE:02 REACHABLE\n" +
		"2a02:6b8::6 dev br-lan lladdr aa:bb:cc:dd:ee:02 STALE\n" +
		"2a02:6b8::7 dev br-lan FAILED\n"
	got := parseNeigh6(out)
	if len(got) != 1 || len(got["aa:bb:cc:dd:ee:02"]) != 2 {
		t.Errorf("got %v", got)
	}
}
