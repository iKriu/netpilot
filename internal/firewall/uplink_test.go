package firewall

import (
	"net/netip"
	"strings"
	"testing"
)

// Таблица маршрутов сервера после инцидента: DHCP роутера выдал шлюзом сам
// сервер (192.168.0.73 = 4900A8C0), плюс запасной маршрут с большей метрикой.
const sampleRoute = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
	"enp1s0\t00000000\t4900A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
	"wlp2s0\t00000000\t0100A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n" +
	"enp1s0\t0000A8C0\t00000000\t0001\t0\t0\t100\t00FFFFFF\t0\t0\t0\n" +
	"docker0\t000011AC\t00000000\t0001\t0\t0\t0\t0000FFFF\t0\t0\t0\n"

func TestParseDefaultRoutes(t *testing.T) {
	r := ParseDefaultRoutes(sampleRoute)
	if len(r) != 2 {
		t.Fatalf("ожидалось 2 маршрута по умолчанию, получено %v", r)
	}
	if r[0].Iface != "enp1s0" || r[0].Gateway.String() != "192.168.0.73" || r[0].Metric != 100 {
		t.Errorf("основной маршрут: %+v", r[0])
	}
	if r[1].Gateway.String() != "192.168.0.1" {
		t.Errorf("второй маршрут: %+v", r[1])
	}

	// Порядок по метрике, а не по строкам; выключенные (без RTF_UP) — мимо;
	// маршрут без шлюза (ppp) — без адреса шлюза.
	r = ParseDefaultRoutes("Iface\tDestination\n" +
		"eth1\t00000000\t0101A8C0\t0003\t0\t0\t200\t00000000\n" +
		"eth2\t00000000\t0102A8C0\t0002\t0\t0\t10\t00000000\n" +
		"ppp0\t00000000\t00000000\t0001\t0\t0\t50\t00000000\n")
	if len(r) != 2 || r[0].Iface != "ppp0" || r[0].Gateway.IsValid() || r[1].Gateway.String() != "192.168.1.1" {
		t.Errorf("получено %+v", r)
	}
	if r := ParseDefaultRoutes("Iface\tDestination\tGateway\n"); len(r) != 0 {
		t.Errorf("пустая таблица: %v", r)
	}
}

func TestGatewayIsSelf(t *testing.T) {
	local := []netip.Addr{netip.MustParseAddr("192.168.0.73"), netip.MustParseAddr("fd00::73")}
	for gw, want := range map[string]bool{
		"192.168.0.73": true,
		"192.168.0.1":  false,
		"127.0.0.1":    true,
		"fd00::73":     true,
		"":             false,
		"мусор":        false,
	} {
		if got := GatewayIsSelf(gw, local); got != want {
			t.Errorf("%q: %v, ожидалось %v", gw, got, want)
		}
	}
}

func TestUplinkIssue(t *testing.T) {
	local := []netip.Addr{netip.MustParseAddr("192.168.0.73")}
	routes := ParseDefaultRoutes(sampleRoute)
	if why := UplinkIssue(routes, true, local); !strings.Contains(why, "192.168.0.73") {
		t.Errorf("шлюз — сам сервер, причина: %q", why)
	}
	if why := UplinkIssue(routes[1:], true, local); why != "" {
		t.Errorf("шлюз — роутер, а причина %q", why)
	}
	if why := UplinkIssue(nil, true, local); why == "" {
		t.Error("без маршрута по умолчанию должна быть причина")
	}
	if why := UplinkIssue(nil, false, local); why != "" {
		t.Errorf("таблица не прочитана (не Linux) — судить не по чему, а причина %q", why)
	}
	ppp := []Route{{Iface: "ppp0"}}
	if why := UplinkIssue(ppp, true, local); why != "" {
		t.Errorf("маршрут без шлюза (ppp) исправен, а причина %q", why)
	}
}

func TestUplinkProblems(t *testing.T) {
	local := []netip.Prefix{netip.MustParsePrefix("192.168.0.73/24")}
	routes := ParseDefaultRoutes(sampleRoute)
	resolv := "nameserver 192.168.0.73\nnameserver 192.168.0.1\n"
	crit, warn := uplinkProblems(routes, true, local, resolv)
	if len(crit) != 1 || !strings.Contains(crit[0], "он сам (192.168.0.73)") ||
		!strings.Contains(crit[0], "ipv4.addresses 192.168.0.73/24") || !strings.Contains(crit[0], "enp1s0") {
		t.Errorf("критичные: %q", crit)
	}
	if len(warn) != 1 || !strings.Contains(warn[0], "DNS") {
		t.Errorf("предупреждения: %q", warn)
	}

	crit, warn = uplinkProblems(routes[1:], true, local, "nameserver 192.168.0.1\n")
	if len(crit)+len(warn) != 0 {
		t.Errorf("всё настроено верно, а проблемы: %q %q", crit, warn)
	}
	if crit, _ := uplinkProblems(nil, true, local, ""); len(crit) != 1 {
		t.Errorf("нет маршрута по умолчанию: %q", crit)
	}
	if crit, _ := uplinkProblems(nil, false, local, ""); len(crit) != 0 {
		t.Errorf("таблица не прочитана: %q", crit)
	}
}
