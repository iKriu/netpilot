package router

import (
	"fmt"
	"strings"
)

const (
	natTable    = "netpilot_nat"
	filterTable = "netpilot_router"
)

// natRuleset — NAT и защита для режима роутера (Linux).
//
//   - маскарадинг: устройства локальной сети выходят в интернет с адресом роутера;
//   - из интернета в локальную сеть — только ответы на наши соединения;
//   - сам роутер из интернета не виден (панель, SSH, службы сервера);
//   - MSS clamping: без него через PPPoE (MTU 1492) «зависают» некоторые сайты.
//
// wanDevs — интерфейсы со стороны провайдера (для PPPoE это и ppp0, и сам порт).
func natRuleset(wanDevs []string, lan string) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	wans := quoteSet(wanDevs)

	w("add table ip %s", natTable)
	w("delete table ip %s", natTable)
	w("add table inet %s", filterTable)
	w("delete table inet %s", filterTable)

	w("table ip %s {", natTable)
	w("\tchain postrouting {")
	w("\t\ttype nat hook postrouting priority srcnat; policy accept;")
	w("\t\toifname %s masquerade", wans)
	w("\t}")
	w("}")

	w("table inet %s {", filterTable)
	w("\tchain forward {")
	w("\t\ttype filter hook forward priority filter; policy accept;")
	w("\t\tiifname %s ct state established,related accept", wans)
	w("\t\tiifname %s oifname %q drop", wans, lan)
	w("\t}")
	w("\tchain mss {")
	w("\t\ttype filter hook forward priority mangle; policy accept;")
	w("\t\toifname %s tcp flags syn tcp option maxseg size set rt mtu", wans)
	w("\t\tiifname %s tcp flags syn tcp option maxseg size set rt mtu", wans)
	w("\t}")
	w("\tchain input {")
	w("\t\ttype filter hook input priority filter; policy accept;")
	w("\t\tiifname %s ct state established,related accept", wans)
	w("\t\tiifname %s meta l4proto { icmp, ipv6-icmp } accept", wans)
	w("\t\tiifname %s udp dport 68 accept", wans) // ответы DHCP провайдера
	w("\t\tiifname %s drop", wans)
	w("\t}")
	w("}")
	return b.String()
}

func quoteSet(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = fmt.Sprintf("%q", s)
	}
	return "{ " + strings.Join(q, ", ") + " }"
}
