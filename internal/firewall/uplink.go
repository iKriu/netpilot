package firewall

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
)

// Проверки «есть ли у самого сервера выход в интернет». Типичная ловушка
// шлюза «на одной ноге»: на роутере в DHCP указывают шлюзом и DNS адрес
// NetPilot для всех устройств — и сервер после перезагрузки получает те же
// настройки. Его маршрут по умолчанию ведёт в него самого, DNS — тоже: интернета
// нет ни у сервера, ни у сети, а перезапуски ядра ничего не исправят.

// Route — маршрут по умолчанию из /proc/net/route.
type Route struct {
	Iface   string
	Gateway netip.Addr // невалидный — маршрут без шлюза (ppp, tun: «прямо в интерфейс»)
	Metric  int
}

// Флаги маршрута из linux/route.h.
const (
	rtfUp      = 0x1
	rtfGateway = 0x2
)

// ParseDefaultRoutes разбирает содержимое /proc/net/route и возвращает
// включённые маршруты по умолчанию (назначение и маска 0.0.0.0), начиная
// с основного — с наименьшей метрикой: им ядро Linux и пользуется.
// Адреса там записаны в hex в порядке байт процессора (на x86 и ARM —
// little-endian): 0100A8C0 = 192.168.0.1.
func ParseDefaultRoutes(s string) []Route {
	var out []Route
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		// Iface Destination Gateway Flags RefCnt Use Metric Mask ...
		if len(f) < 8 || f[1] != "00000000" || f[7] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(f[3], 16, 32)
		if err != nil || flags&rtfUp == 0 {
			continue
		}
		metric, _ := strconv.Atoi(f[6])
		r := Route{Iface: f[0], Metric: metric}
		if flags&rtfGateway != 0 {
			gw, ok := hexAddr(f[2])
			if !ok {
				continue
			}
			r.Gateway = gw
		}
		out = append(out, r)
	}
	slices.SortStableFunc(out, func(a, b Route) int { return a.Metric - b.Metric })
	return out
}

func hexAddr(s string) (netip.Addr, bool) {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 4 {
		return netip.Addr{}, false
	}
	var ip [4]byte
	binary.BigEndian.PutUint32(ip[:], binary.LittleEndian.Uint32(b))
	return netip.AddrFrom4(ip), true
}

// GatewayIsSelf — шлюз по умолчанию указывает на этот же компьютер.
func GatewayIsSelf(defaultGW string, local []netip.Addr) bool {
	gw, err := netip.ParseAddr(strings.TrimSpace(defaultGW))
	if err != nil {
		return false
	}
	gw = gw.Unmap()
	return gw.IsLoopback() || slices.Contains(local, gw)
}

// localPrefixes — адреса интерфейсов этого компьютера с длиной префикса
// (подменяется в тестах).
var localPrefixes = func() []netip.Prefix {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []netip.Prefix
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip, ok := netip.AddrFromSlice(n.IP)
		if !ok {
			continue
		}
		bits, _ := n.Mask.Size()
		out = append(out, netip.PrefixFrom(ip.Unmap(), bits))
	}
	return out
}

// LocalAddrs — адреса интерфейсов этого компьютера.
func LocalAddrs() []netip.Addr {
	var out []netip.Addr
	for _, p := range localPrefixes() {
		out = append(out, p.Addr())
	}
	return out
}

// procNetRoute — путь к таблице маршрутов (подменяется в тестах).
var procNetRoute = "/proc/net/route"

// DefaultRoutes — маршруты по умолчанию системы. ok=false — таблицу прочитать
// нельзя (не Linux): тогда об исправности маршрутов судить не по чему.
func DefaultRoutes() (routes []Route, ok bool) {
	b, err := os.ReadFile(procNetRoute)
	if err != nil {
		return nil, false
	}
	return ParseDefaultRoutes(string(b)), true
}

// UplinkIssue — причина, по которой у сервера заведомо нет выхода в интернет,
// или "" (всё в порядке или проверить нельзя). Чистая функция для тестов.
func UplinkIssue(routes []Route, ok bool, local []netip.Addr) string {
	if !ok {
		return ""
	}
	if len(routes) == 0 {
		return "у сервера нет маршрута по умолчанию"
	}
	if gw := routes[0].Gateway; gw.IsValid() && GatewayIsSelf(gw.String(), local) {
		return fmt.Sprintf("шлюз по умолчанию сервера — он сам (%s)", gw)
	}
	return ""
}

// Uplink — UplinkIssue для текущей системы.
func Uplink() string {
	routes, ok := DefaultRoutes()
	return UplinkIssue(routes, ok, LocalAddrs())
}

// uplinkProblems — сообщения для Status.Problems о выходе сервера в интернет:
// критичные (без них не работает вообще ничего) и предупреждения.
func uplinkProblems(routes []Route, ok bool, local []netip.Prefix, resolv string) (critical, warnings []string) {
	addrs := make([]netip.Addr, len(local))
	for i, p := range local {
		addrs[i] = p.Addr()
	}
	if ok && len(routes) == 0 {
		critical = append(critical, "У сервера нет маршрута по умолчанию: он сам не выходит в интернет и не может раздавать его устройствам. Проверьте сетевые настройки сервера (шлюз роутера).")
	} else if ok && routes[0].Gateway.IsValid() && GatewayIsSelf(routes[0].Gateway.String(), addrs) {
		gw := routes[0].Gateway
		self := gw.String() + "/24"
		for _, p := range local {
			if p.Addr() == gw {
				self = p.String()
			}
		}
		critical = append(critical, fmt.Sprintf(
			"Шлюз по умолчанию этого сервера — он сам (%s): сервер не выходит в интернет и не может раздавать его. "+
				"Так бывает, когда роутер раздаёт по DHCP адрес NetPilot как шлюз всем устройствам, включая сам сервер. "+
				"Укажите серверу шлюз и DNS роутера вручную, например: nmcli connection modify <подключение> ipv4.method manual ipv4.addresses %s ipv4.gateway <адрес роутера> ipv4.dns <адрес роутера> (имя подключения — nmcli connection show; интерфейс %s), "+
				"или не раздавайте адрес шлюза-NetPilot самому серверу (закрепите за ним адрес с обычным шлюзом на роутере).",
			gw, self, routes[0].Iface))
	}
	if ns := nameservers(resolv); len(ns) > 0 && GatewayIsSelf(ns[0].String(), addrs) {
		warnings = append(warnings, fmt.Sprintf(
			"Первый DNS-сервер этого сервера — он сам (%s): пока ядро не работает, сервер не сможет разрешать имена. Укажите серверу DNS роутера или публичный DNS вручную (ipv4.dns в nmcli).",
			ns[0]))
	}
	return critical, warnings
}
