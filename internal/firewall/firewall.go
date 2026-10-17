// Package firewall настраивает перехват трафика устройств для прозрачного
// режима: nftables + TPROXY + маршрутизация по метке. Решения «мимо ядра»
// (прямые устройства, локальные адреса) принимаются прямо в ядре Linux,
// поэтому такой трафик не получает никакой задержки.
package firewall

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

const (
	Table  = "netpilot"
	Table6 = "netpilot6"
	// Метка и таблица маршрутизации: помеченные пакеты доставляются локально,
	// в сокет TPROXY ядра прокси.
	Mark       = 0x2017
	RouteTable = 2017
)

// Режимы IPv6.
const (
	IPv6Block     = "block"     // IPv6 в интернет отклоняется — устройства сразу переходят на IPv4
	IPv6Intercept = "intercept" // IPv6 перехватывается так же, как IPv4
	IPv6Off       = "off"       // IPv6 не трогаем
)

type Spec struct {
	Port      int    // порт TPROXY ядра
	Scope     string // "all" | "selected"
	HijackDNS bool   // перехватывать DNS устройств
	IPv6      string // IPv6Block | IPv6Intercept | IPv6Off

	// Адреса устройств по маршрутам (IPv4 и IPv6 вперемешку).
	Direct []string // «Напрямую» — мимо ядра
	Block  []string // «Без интернета» — отбрасываются
	Proxy  []string // «Через прокси» (для Scope = "selected")

	// LAN6 — IPv6-префиксы локальной сети (с интерфейсов шлюза): в IPv6 у
	// устройств публичные адреса, и отличить «своих» можно только по префиксу.
	LAN6 []string
	// LanIfaces — если задано (режим роутера), перехватывается только трафик,
	// пришедший с этих интерфейсов.
	LanIfaces []string
	// Masquerade — шлюз «на одной ноге» (роутер раздаёт этот компьютер как шлюз):
	// трафик, который идёт мимо прокси (ping, устройства «Напрямую»), выходит с
	// адресом шлюза, чтобы ответы вернулись тем же путём.
	Masquerade bool
}

// Локальные и служебные сети: сюда трафик всегда идёт напрямую.
var privateNets = []string{
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"172.16.0.0/12", "192.0.0.0/24", "192.168.0.0/16", "198.18.0.0/15", "224.0.0.0/4", "240.0.0.0/4",
}

var privateNets6 = []string{"::/127", "::ffff:0:0/96", "64:ff9b:1::/48", "fc00::/7", "fe80::/10", "ff00::/8"}

// Источники, которые считаются устройствами локальной сети.
var lanNets = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}

// Интерфейсы, трафик с которых не трогаем: контейнеры, виртуальные машины, VPN.
var skipIfaces = []string{"lo", "docker*", "br-*", "veth*", "virbr*", "tun*", "tap*", "wg*", "tailscale*", "zt*", "ppp*"}

// Ruleset возвращает скрипт для «nft -f -». Таблицы пересоздаются целиком
// одной транзакцией: либо применяется всё, либо ничего.
func Ruleset(s Spec) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	direct, block, proxy := split(s.Direct), split(s.Block), split(s.Proxy) // [0] — IPv4, [1] — IPv6

	// "add table" + "delete table" — удалить, даже если таблицы ещё нет.
	w("add table ip %s", Table)
	w("delete table ip %s", Table)
	w("add table ip6 %s", Table6)
	w("delete table ip6 %s", Table6)

	// ---- IPv4 ----
	w("table ip %s {", Table)
	w("\tset private { type ipv4_addr; flags interval; elements = { %s } }", strings.Join(privateNets, ", "))
	w("\tset lan { type ipv4_addr; flags interval; elements = { %s } }", strings.Join(lanNets, ", "))
	writeSet(w, "direct", "ipv4_addr", direct[0])
	writeSet(w, "block", "ipv4_addr", block[0])
	writeSet(w, "proxy", "ipv4_addr", proxy[0])
	w("\tchain prerouting {")
	w("\t\ttype filter hook prerouting priority mangle; policy accept;")
	writeIfaceFilter(w, s.LanIfaces)
	w("\t\tip saddr != @lan return")
	w("\t\tmeta l4proto != { tcp, udp } return")
	// «Без интернета»: локальная сеть остаётся доступной.
	w("\t\tip saddr @block ip daddr != @private drop")
	w("\t\tip saddr @direct return")
	if s.Scope == "selected" {
		w("\t\tip saddr != @proxy return")
	}
	if s.HijackDNS {
		// До проверки локальных адресов: DNS-сервер обычно — сам роутер.
		w("\t\tmeta l4proto { tcp, udp } th dport 53 meta mark set 0x%x tproxy to :%d accept", Mark, s.Port)
	}
	w("\t\tfib daddr type local return")
	w("\t\tip daddr @private return")
	// Протокол в правиле с tproxy обязателен, даже если отфильтрован выше.
	w("\t\tmeta l4proto { tcp, udp } meta mark set 0x%x tproxy to :%d accept", Mark, s.Port)
	w("\t}")
	if s.Masquerade {
		w("\tchain postrouting {")
		w("\t\ttype nat hook postrouting priority srcnat; policy accept;")
		// Только пересылаемый трафик устройств; собственный трафик шлюза не трогаем.
		w("\t\tip saddr @lan ip daddr != @private fib saddr type != local masquerade")
		w("\t}")
	}
	w("}")

	// ---- IPv6 ----
	if s.IPv6 == IPv6Off {
		return b.String()
	}
	lan6 := validPrefixes6(s.LAN6)
	w("table ip6 %s {", Table6)
	w("\tset private { type ipv6_addr; flags interval; elements = { %s } }", strings.Join(privateNets6, ", "))
	if len(lan6) > 0 {
		w("\tset lan { type ipv6_addr; flags interval; elements = { %s } }", strings.Join(lan6, ", "))
	} else {
		w("\tset lan { type ipv6_addr; flags interval; }")
	}
	writeSet(w, "direct", "ipv6_addr", direct[1])
	writeSet(w, "block", "ipv6_addr", block[1])
	writeSet(w, "proxy", "ipv6_addr", proxy[1])

	w("\tchain prerouting {")
	w("\t\ttype filter hook prerouting priority mangle; policy accept;")
	writeIfaceFilter(w, s.LanIfaces)
	w("\t\tip6 saddr != @lan return")
	w("\t\tmeta l4proto != { tcp, udp } return") // ICMPv6 (соседи, PMTU) не трогаем
	w("\t\tip6 saddr @block ip6 daddr != @private drop")
	w("\t\tip6 saddr @direct return")
	if s.IPv6 == IPv6Intercept {
		if s.Scope == "selected" {
			w("\t\tip6 saddr != @proxy return")
		}
		if s.HijackDNS {
			w("\t\tmeta l4proto { tcp, udp } th dport 53 meta mark set 0x%x tproxy to :%d accept", Mark, s.Port)
		}
		w("\t\tfib daddr type local return")
		w("\t\tip6 daddr @private return")
		w("\t\tmeta l4proto { tcp, udp } meta mark set 0x%x tproxy to :%d accept", Mark, s.Port)
	}
	w("\t}")

	if s.IPv6 == IPv6Block {
		// reject работает только в forward: устройство сразу получает отказ
		// и переходит на IPv4 без ожидания таймаута.
		w("\tchain forward {")
		w("\t\ttype filter hook forward priority filter; policy accept;")
		writeIfaceFilter(w, s.LanIfaces)
		w("\t\tip6 saddr != @lan return")
		w("\t\tip6 daddr @private return")
		w("\t\tip6 saddr @direct return")
		if s.Scope == "selected" {
			w("\t\tip6 saddr != @proxy return")
		}
		w("\t\tmeta l4proto tcp reject with tcp reset")
		w("\t\tmeta l4proto udp reject with icmpv6 type admin-prohibited")
		w("\t}")
	}
	w("}")
	return b.String()
}

func writeIfaceFilter(w func(string, ...any), lanIfaces []string) {
	for _, ifc := range skipIfaces {
		w("\t\tiifname %q return", ifc)
	}
	if len(lanIfaces) > 0 {
		quoted := make([]string, len(lanIfaces))
		for i, s := range lanIfaces {
			quoted[i] = fmt.Sprintf("%q", s)
		}
		w("\t\tiifname != { %s } return", strings.Join(quoted, ", "))
	}
}

// split делит адреса на IPv4 и IPv6.
func split(ips []string) [2][]string {
	var out [2][]string
	for _, ip := range ips {
		a, err := netip.ParseAddr(strings.TrimSpace(ip))
		if err != nil {
			continue
		}
		if a.Is4() || a.Is4In6() {
			out[0] = append(out[0], a.Unmap().String())
		} else if !a.IsLinkLocalUnicast() { // fe80:: не выходит за пределы сети
			out[1] = append(out[1], a.String())
		}
	}
	return out
}

func writeSet(w func(string, ...any), name, typ string, ips []string) {
	valid := slices.Clone(ips)
	slices.Sort(valid)
	valid = slices.Compact(valid)
	if len(valid) == 0 {
		w("\tset %s { type %s; }", name, typ)
		return
	}
	w("\tset %s { type %s; elements = { %s } }", name, typ, strings.Join(valid, ", "))
}

func validPrefixes6(in []string) []string {
	// ULA и link-local — всегда локальные.
	all := []netip.Prefix{netip.MustParsePrefix("fc00::/7"), netip.MustParsePrefix("fe80::/10")}
	for _, s := range in {
		if p, err := netip.ParsePrefix(strings.TrimSpace(s)); err == nil && p.Addr().Is6() && !p.Addr().Is4In6() {
			all = append(all, p.Masked())
		}
	}
	// nftables не принимает пересекающиеся интервалы: убираем вложенные
	// (например, fd00:30::/64 уже входит в fc00::/7).
	slices.SortFunc(all, func(a, b netip.Prefix) int { return a.Bits() - b.Bits() })
	var kept []netip.Prefix
	for _, p := range all {
		if !slices.ContainsFunc(kept, func(k netip.Prefix) bool { return k.Contains(p.Addr()) }) {
			kept = append(kept, p)
		}
	}
	out := make([]string, len(kept))
	for i, p := range kept {
		out[i] = p.String()
	}
	slices.Sort(out)
	return out
}

// Status — применимость прозрачного режима на этой системе.
type Status struct {
	Supported bool     `json:"supported"`
	Active    bool     `json:"active"`
	Problems  []string `json:"problems,omitempty"`
}
