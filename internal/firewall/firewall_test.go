package firewall

import (
	"strings"
	"testing"
)

func TestRuleset(t *testing.T) {
	rs := Ruleset(Spec{
		Port: 7893, Scope: "all", HijackDNS: true,
		Direct: []string{"192.168.1.20", "bad", "192.168.1.20"},
		Block:  []string{"192.168.1.30"},
	})
	for _, want := range []string{
		"delete table ip netpilot",
		"set direct { type ipv4_addr; elements = { 192.168.1.20 } }", // дубли и мусор отброшены
		"set block { type ipv4_addr; elements = { 192.168.1.30 } }",
		"set proxy { type ipv4_addr; }",
		`iifname "docker*" return`,
		"ip saddr @block ip daddr != @private drop",
		"meta l4proto { tcp, udp } th dport 53 meta mark set 0x2017 tproxy to :7893 accept",
		"\t\tmeta l4proto { tcp, udp } meta mark set 0x2017 tproxy to :7893 accept",
	} {
		if !strings.Contains(rs, want) {
			t.Errorf("нет строки %q в:\n%s", want, rs)
		}
	}
	if strings.Contains(rs, "ip saddr != @proxy return") {
		t.Error("для scope=all не должно быть ограничения по @proxy")
	}
	// Порядок важен: блокировка и прямые устройства — до перехвата DNS,
	// DNS — до пропуска локальных адресов.
	order := []string{"@block", "saddr @direct return", "dport 53", "fib daddr type local", "daddr @private return", "\t\tmeta l4proto { tcp, udp } meta mark set"}
	last := -1
	for _, s := range order {
		i := strings.Index(rs, s)
		if i < last {
			t.Errorf("%q стоит раньше, чем должно", s)
		}
		last = i
	}

	if strings.Contains(rs, "masquerade") {
		t.Error("без Masquerade не должно быть NAT")
	}
	gw := Ruleset(Spec{Port: 7893, Scope: "all", Masquerade: true})
	if !strings.Contains(gw, "ip saddr @lan ip daddr != @private fib saddr type != local masquerade") {
		t.Errorf("шлюз на одной ноге: нужен NAT для пересылаемого трафика:\n%s", gw)
	}

	sel := Ruleset(Spec{Port: 7893, Scope: "selected"})
	if !strings.Contains(sel, "ip saddr != @proxy return") || strings.Contains(sel, "dport 53") {
		t.Errorf("scope=selected без DNS:\n%s", sel)
	}
}

func TestIPv6(t *testing.T) {
	rs := Ruleset(Spec{
		Port: 7893, Scope: "all", IPv6: IPv6Block,
		LAN6:   []string{"fd00:30::/64", "2a02:6b8:1::/64", "bad"},
		Direct: []string{"192.168.1.20", "2a02:6b8:1::20", "fe80::1"},
	})
	// fd00:30::/64 входит в fc00::/7 и не должен дублироваться (nft отверг бы пересечение).
	if !strings.Contains(rs, "set lan { type ipv6_addr; flags interval; elements = { 2a02:6b8:1::/64, fc00::/7, fe80::/10 } }") {
		t.Errorf("lan6:\n%s", rs)
	}
	if !strings.Contains(rs, "set direct { type ipv6_addr; elements = { 2a02:6b8:1::20 } }") ||
		!strings.Contains(rs, "set direct { type ipv4_addr; elements = { 192.168.1.20 } }") {
		t.Errorf("адреса должны разойтись по таблицам IPv4/IPv6, fe80:: отброшен:\n%s", rs)
	}
	if !strings.Contains(rs, "meta l4proto tcp reject with tcp reset") || strings.Contains(rs, "table ip6 netpilot6 {\n\tset private { type ipv6_addr; flags interval; elements = { ::/127") && strings.Contains(rs, "ip6 daddr @private return\n\t\tmeta l4proto { tcp, udp } meta mark") {
		t.Errorf("режим block: нужен reject в forward и не нужен tproxy:\n%s", rs)
	}
	if off := Ruleset(Spec{Port: 7893, IPv6: IPv6Off}); strings.Contains(off, "table ip6 netpilot6 {") {
		t.Error("режим off не должен создавать таблицу IPv6")
	}
}

func TestDNSFallbackRuleset(t *testing.T) {
	s, err := DNSFallbackRuleset("192.168.0.1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"add table ip " + DNSFallbackTable, "delete table ip " + DNSFallbackTable,
		"fib daddr type local meta l4proto { tcp, udp } th dport 53 dnat to 192.168.0.1:53",
		"ip daddr 192.168.0.1 meta l4proto { tcp, udp } th dport 53 masquerade",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("нет %q в:\n%s", want, s)
		}
	}
	for _, bad := range []string{"", "127.0.0.53\n", "::1", "1.2.3.4; drop"} {
		if _, err := DNSFallbackRuleset(bad); err == nil {
			t.Errorf("адрес %q должен отклоняться", bad)
		}
	}
}

func TestFirstNameserver(t *testing.T) {
	cases := map[string]string{
		"nameserver 127.0.0.53\noptions edns0\n":                   "",
		"# от systemd\nnameserver 192.168.0.1\nnameserver 8.8.8.8": "192.168.0.1",
		"nameserver fe80::1\nnameserver 77.88.8.8\n":               "77.88.8.8",
		"search lan\n": "",
	}
	for in, want := range cases {
		if got := firstNameserver(in); got != want {
			t.Errorf("%q: %q, ожидался %q", in, got, want)
		}
	}
}
